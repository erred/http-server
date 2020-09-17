package main

import (
	"context"
	"crypto/tls"
	"flag"
	"io/ioutil"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/api/global"
	"go.opentelemetry.io/otel/api/metric"
	"go.opentelemetry.io/otel/api/unit"
	"go.opentelemetry.io/otel/exporters/metric/prometheus"
	"go.opentelemetry.io/otel/label"

	"github.com/rs/zerolog"
)

func main() {
	var s Server
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.StringVar(&s.addr, "addr", ":8080", "listen addr")
	fs.StringVar(&s.tlsCert, "tls-cert", "", "tls cert file")
	fs.StringVar(&s.tlsKey, "tls-key", "", "tls key file")
	fs.StringVar(&s.dir, "dir", "public", "directory to serve")
	fs.Parse(os.Args[1:])

	promExporter, _ := prometheus.InstallNewPipeline(prometheus.Config{
		DefaultHistogramBoundaries: []float64{1, 5, 10, 50, 100},
	})
	s.meter = global.Meter(os.Args[0])
	s.page = metric.Must(s.meter).NewInt64Counter(
		"page_hit",
		metric.WithDescription("hits per page"),
	)
	s.code = metric.Must(s.meter).NewInt64Counter(
		"response_code",
		metric.WithDescription("http response codes"),
	)
	s.latency = metric.Must(s.meter).NewInt64ValueRecorder(
		"serve_latency",
		metric.WithDescription("http response latency"),
		metric.WithUnit(unit.Milliseconds),
	)

	s.log = zerolog.New(os.Stdout).With().Timestamp().Logger()

	notfound, err := ioutil.ReadFile(path.Join(s.dir, "404.html"))
	if err == nil {
		s.notfound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write(notfound)
		})
	}

	m := http.NewServeMux()
	m.Handle("/", s)
	m.Handle("/metrics", promExporter)
	m.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	m.HandleFunc("/debug/pprof/", pprof.Index)
	m.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	m.HandleFunc("/debug/pprof/profile", pprof.Profile)
	m.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	m.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{
		Addr:              s.addr,
		Handler:           cors(m),
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

	if s.tlsKey != "" && s.tlsCert != "" {
		cert, err := tls.LoadX509KeyPair(s.tlsCert, s.tlsKey)
		if err != nil {
			s.log.Error().Err(err).Msg("laod tls keys")
			return
		}
		srv.TLSConfig.Certificates = []tls.Certificate{cert}
	}

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		<-c
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		go func() {
			<-c
			cancel()
		}()
		err := srv.Shutdown(ctx)
		if err != nil {
			s.log.Error().Err(err).Msg("unclean shutdown")
		}
	}()

	if len(srv.TLSConfig.Certificates) > 0 {
		err = srv.ListenAndServeTLS("", "")
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil {
		s.log.Error().Err(err).Msg("exit")
		os.Exit(1)
	}
}

func cors(h http.Handler) http.Handler {
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

type Server struct {
	dir      string
	notfound http.Handler

	meter   metric.Meter
	page    metric.Int64Counter
	code    metric.Int64Counter
	latency metric.Int64ValueRecorder

	log zerolog.Logger

	addr    string
	tlsCert string
	tlsKey  string
}

func (s Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t, code := time.Now(), http.StatusOK
	s.page.Add(r.Context(), 1, label.String("page", r.URL.Path))

	// get data
	remote := r.Header.Get("x-forwarded-for")
	if remote == "" {
		remote = r.RemoteAddr
	}
	ua := r.Header.Get("user-agent")

	defer func() {
		s.latency.Record(r.Context(), time.Since(t).Milliseconds())
		s.code.Add(r.Context(), 1, label.Int("code", code))

		s.log.Debug().Str("path", r.URL.Path).Str("src", remote).Int("code", code).Str("user-agent", ua).Msg("served")
	}()

	u, f := r.URL.Path, ""
	switch {
	case strings.HasSuffix(u, "/") && exists(path.Join(s.dir, u[:len(u)-1]+".html")):
		f = path.Join(s.dir, u[:len(u)-1]+".html")
	case strings.HasSuffix(u, "/") && exists(path.Join(s.dir, u, "index.html")):
		f = path.Join(s.dir, u, "index.html")
	case strings.HasSuffix(u, "/"):
		if s.notfound != nil {
			s.notfound.ServeHTTP(w, r)
		} else {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		}
		code = http.StatusNotFound
		return
	case !strings.HasSuffix(u, ".html") && exists(path.Join(s.dir, u)):
		f = path.Join(s.dir, u)
	default:
		http.Redirect(w, r, canonical(u), http.StatusMovedPermanently)
		code = http.StatusMovedPermanently
		return
	}
	http.ServeFile(w, r, f)
}

func exists(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return true
}

func canonical(p string) string {
	p = strings.TrimSuffix(strings.TrimSuffix(p, ".html"), "index")
	if p[len(p)-1] != '/' {
		p = p + "/"
	}
	return p
}
