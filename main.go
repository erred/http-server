package main

import (
	"flag"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.seankhliao.com/usvc"
)

func main() {
	s := NewServer(os.Args)
	s.svc.Log.Error().Err(usvc.Run(usvc.SignalContext(), s.svc)).Msg("exited")
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
	svc *usvc.ServerSimple
}

func NewServer(args []string) *Server {
	fs := flag.NewFlagSet(args[0], flag.ExitOnError)
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
		svc: usvc.NewServerSimple(usvc.NewConfig(fs)),
	}

	s.svc.Mux.Handle("/metrics", promhttp.Handler())
	s.svc.Mux.Handle("/", s)

	fs.StringVar(&s.dir, "dir", "public", "template to use, takes a singe {{.Repo}}")
	fs.Parse(args[1:])

	notfound, err := ioutil.ReadFile(path.Join(s.dir, "404.html"))
	if err == nil {
		s.notfound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write(notfound)
		})
	}

	s.svc.Log.Info().Str("dir", s.dir).Msg("configured")
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
