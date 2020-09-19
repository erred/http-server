package main

import (
	"context"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/api/global"
	"go.opentelemetry.io/otel/api/metric"
	"go.seankhliao.com/usvc"
)

func main() {
	var srvconf usvc.Conf
	var s Server

	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	srvconf.RegisterFlags(fs)
	s.RegisterFlags(fs)
	fs.Parse(os.Args[1:])

	s.log = zerolog.New(os.Stdout).With().Timestamp().Logger()
	log.SetOutput(s.log)

	s.page = metric.Must(global.Meter(os.Args[0])).NewInt64Counter(
		"page_hit",
		metric.WithDescription("hits per page"),
	)

	notfound, _ := ioutil.ReadFile(path.Join(s.dir, "404.html"))
	s.notfound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write(notfound)
	})

	m := http.NewServeMux()
	m.Handle("/", s)

	_, run, err := srvconf.Server(m, s.log)
	if err != nil {
		s.log.Error().Err(err).Msg("prepare server")
		os.Exit(1)
	}

	err = run(context.Background())
	if err != nil {
		s.log.Error().Err(err).Msg("exit")
		os.Exit(1)
	}
}

type Server struct {
	dir      string
	notfound http.Handler

	page metric.Int64Counter

	log zerolog.Logger
}

func (s *Server) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&s.dir, "dir", "public", "directory to serve")
}

func (s Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		return
	case !strings.HasSuffix(u, ".html") && exists(path.Join(s.dir, u)):
		f = path.Join(s.dir, u)
	default:
		http.Redirect(w, r, canonical(u), http.StatusMovedPermanently)
		return
	}

	setHeaders(w)
	switch path.Ext(f) {
	case "otf", "ttf", "woff", "woff2", "css", "png", "jpg", "jpeg", "webp", "json", "js":
		w.Header().Set("cache-control", `max-age=2592000`)
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

func setHeaders(w http.ResponseWriter) {
	w.Header().Set("strict-transport-security", `max-age=63072000; preload`)
	w.Header().Set("referrer-policy", "strict-origin-when-cross-origin")
	w.Header().Set("report-to", `{"group": "csp-endpoint", "max_age": 10886400, "endpoints": [{"url":"https://statslogger.seankhliao.com/json"}]}`)
	w.Header().Set("content-security-policy", `default-src 'self'; upgrade-insecure-requests; connect-src https://statslogger.seankhliao.com https://www.google-analytics.com; font-src https://seankhliao.com; img-src *; object-src 'none'; script-src-elem 'nonce-deadbeef2' 'nonce-deadbeef3' 'nonce-deadbeef4' https://unpkg.com https://www.google-analytics.com https://ssl.google-analytics.com https://www.googletagmanager.com; sandbox allow-scripts; style-src-elem 'nonce-deadbeef1' https://seankhliao.com; report-to csp-endpoint; report-uri https://statslogger.seankhliao.com/json`)
	w.Header().Set("feature-policy", `accelerometer 'none'; autoplay 'none'; camera 'none'; document-domain 'none'; encrypted-media 'none'; fullscreen 'none'; geolocation 'none'; gyroscope 'none'; magnetometer 'none'; microphone 'none'; midi 'none'; payment 'none'; picture-in-picture 'none'; sync-xhr 'none'; usb 'none'; xr-spatial-tracking 'none'`)
}
