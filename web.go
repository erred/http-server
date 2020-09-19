package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/api/global"
	"go.opentelemetry.io/otel/api/metric"
	"go.opentelemetry.io/otel/api/unit"
	"go.opentelemetry.io/otel/exporters/metric/prometheus"
)

type LogMiddleware struct {
	Latency metric.Int64ValueRecorder
	Log     zerolog.Logger
}

func (m LogMiddleware) Handle(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		remote := r.Header.Get("x-forwarded-for")
		if remote == "" {
			remote = r.RemoteAddr
		}
		ua := r.Header.Get("user-agent")

		defer func() {
			d := time.Since(t)
			m.Latency.Record(r.Context(), d.Milliseconds())
			m.Log.Debug().
				Str("src", remote).
				Str("url", r.URL.String()).
				Str("user-agent", ua).
				Dur("dur", d).
				Msg("served")
		}()

		h.ServeHTTP(w, r)
	})
}

var HealthOK = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

type HTTPServerConf struct {
	Addr        string
	TLSCertFile string
	TLSKeyFile  string

	disablePprof  bool
	disableHealth bool
	disableProm   bool

	disableCORS   bool
	disableLogMid bool
}

func (sc *HTTPServerConf) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&sc.Addr, "addr", ":8080", "listen addr")
	fs.StringVar(&sc.TLSCertFile, "tls-cert", "", "tls cert file")
	fs.StringVar(&sc.TLSKeyFile, "tls-key", "", "tls key file")
}

func (sc HTTPServerConf) Server(h http.Handler, log zerolog.Logger) (*http.Server, func(context.Context) error, error) {
	latency := metric.Must(global.Meter(os.Args[0])).NewInt64ValueRecorder(
		"request_latency_ms",
		metric.WithDescription("http request serve latency"),
		metric.WithUnit(unit.Milliseconds),
	)

	if m, ok := h.(*http.ServeMux); ok {
		if !sc.disablePprof {
			m.HandleFunc("/debug/pprof/", pprof.Index)
			m.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			m.HandleFunc("/debug/pprof/profile", pprof.Profile)
			m.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			m.HandleFunc("/debug/pprof/trace", pprof.Trace)
		}
		if !sc.disableHealth {
			m.Handle("/health", HealthOK)
		}
		if !sc.disableProm {
			promExporter, _ := prometheus.InstallNewPipeline(prometheus.Config{
				DefaultHistogramBoundaries: []float64{1, 5, 10, 50, 100},
			})
			m.Handle("/metrics", promExporter)
		}
	}

	if !sc.disableCORS {
		h = corsAllowAll(h)
	}
	if !sc.disableLogMid {
		lm := LogMiddleware{latency, log}
		h = lm.Handle(h)
	}

	srv := &http.Server{
		Addr:              sc.Addr,
		Handler:           h,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
		TLSConfig: &tls.Config{
			MinVersion:               tls.VersionTLS13,
			PreferServerCipherSuites: true,
		},
	}

	if sc.TLSKeyFile != "" && sc.TLSCertFile != "" {
		cert, err := tls.LoadX509KeyPair(sc.TLSCertFile, sc.TLSKeyFile)
		if err != nil {
			return nil, nil, err
		}
		srv.TLSConfig.Certificates = []tls.Certificate{cert}
	}

	run := func(ctx context.Context) error {
		se := make(chan error)

		go func() {
			c := make(chan os.Signal, 1)
			signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
			<-c
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			go func() {
				<-c
				cancel()
			}()
			se <- srv.Shutdown(ctx)
		}()

		var err error
		if len(srv.TLSConfig.Certificates) > 0 {
			err = srv.ListenAndServeTLS("", "")
		} else {
			err = srv.ListenAndServe()
		}
		if errors.Is(err, http.ErrServerClosed) {
			return <-se
		}
		return err
	}

	return srv, run, nil
}

func corsAllowAll(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodOptions:
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		case http.MethodGet, http.MethodPost:
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
			w.Header().Set("Access-Control-Max-Age", "86400")
			h.ServeHTTP(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
	})
}
