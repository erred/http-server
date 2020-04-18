package main

import (
	"context"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

var (
	port = func() string {
		port := os.Getenv("PORT")
		if port == "" {
			port = ":8080"
		} else if port[0] != ':' {
			port = ":" + port
		}
		return port
	}()
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT)
		<-sigs
		cancel()
	}()

	// server
	s := NewServer(os.Args)
	s.Run(ctx)
}

type Server struct {
	// config
	dir      string
	notfound http.Handler

	// metrics
	page *prometheus.CounterVec
	code *prometheus.CounterVec
	lat  prometheus.Histogram

	// server
	log zerolog.Logger
	mux *http.ServeMux
	srv *http.Server
}

func NewServer(args []string) *Server {
	s := &Server{
		notfound: http.NotFoundHandler(),
		page: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "staticserve_page_requests",
			Help: "requests by page",
		},
			[]string{"module"},
		),
		code: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "staticserve_response_codes",
			Help: "reponse by code",
		},
			[]string{"code"},
		),
		lat: promauto.NewHistogram(prometheus.HistogramOpts{
			Name: "staticserve_response_latency_s",
			Help: "response times in s",
		}),
		log: zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, NoColor: true, TimeFormat: time.RFC3339}).With().Timestamp().Logger(),
		mux: http.NewServeMux(),
		srv: &http.Server{
			ReadHeaderTimeout: 5 * time.Second,
			WriteTimeout:      5 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
	}

	s.mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	s.mux.Handle("/metrics", promhttp.Handler())
	s.mux.Handle("/", s)

	s.srv.Handler = s.mux
	s.srv.ErrorLog = log.New(s.log, "", 0)

	fs := flag.NewFlagSet(args[0], flag.ExitOnError)
	fs.StringVar(&s.srv.Addr, "addr", port, "host:port to serve on")
	fs.StringVar(&s.dir, "dir", "public", "template to use, takes a singe {{.Repo}}")
	fs.Parse(args[1:])

	notfound, err := ioutil.ReadFile(path.Join(s.dir, "404.html"))
	if err == nil {
		s.notfound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write(notfound)
		})
	}

	s.log.Info().Str("addr", s.srv.Addr).Str("dir", s.dir).Msg("configured")
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t, code := time.Now(), http.StatusOK
	s.page.WithLabelValues(r.URL.Path).Inc()
	defer func() {
		s.lat.Observe(time.Since(t).Seconds())
		s.code.WithLabelValues(strconv.Itoa(code)).Inc()
	}()

	u, f := r.URL.Path, ""
	switch {
	case strings.HasSuffix(u, "/") && exists(path.Join(s.dir, u[:len(u)-1]+".html")):
		f = path.Join(s.dir, u[:len(u)-1]+".html")
	case strings.HasSuffix(u, "/") && exists(path.Join(s.dir, u, "index.html")):
		f = path.Join(s.dir, u, "index.html")
	case strings.HasSuffix(u, "/"):
		s.notfound.ServeHTTP(w, r)
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

func (s *Server) Run(ctx context.Context) {
	errc := make(chan error)
	go func() {
		errc <- s.srv.ListenAndServe()
	}()

	var err error
	select {
	case err = <-errc:
	case <-ctx.Done():
		err = s.srv.Shutdown(ctx)
	}
	s.log.Error().Err(err).Msg("server exit")
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
