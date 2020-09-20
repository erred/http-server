package main

import (
	"context"
	"flag"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/api/global"
	"go.opentelemetry.io/otel/api/metric"
	"go.seankhliao.com/stream"
	"go.seankhliao.com/usvc"
	"google.golang.org/grpc"
)

func main() {
	var s Server

	srvc := usvc.DefaultConf(&s)
	s.log = srvc.Logger()

	s.page = metric.Must(global.Meter(os.Args[0])).NewInt64Counter(
		"page_hit",
		metric.WithDescription("hits per page"),
	)

	notfound, _ := ioutil.ReadFile(path.Join(s.dir, "404.html"))
	s.notfound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write(notfound)
	})

	cc, err := grpc.Dial(s.streamAddr, grpc.WithInsecure())
	if err != nil {
		s.log.Error().Err(err).Msg("connect to stream")
	}
	defer cc.Close()
	s.client = stream.NewStreamClient(cc)

	m := http.NewServeMux()
	m.Handle("/", s)

	err = srvc.RunHTTP(context.Background(), m)
	if err != nil {
		s.log.Fatal().Err(err).Msg("run server")
	}
}

type Server struct {
	dir        string
	notfound   http.Handler
	streamAddr string
	client     stream.StreamClient

	page metric.Int64Counter

	log zerolog.Logger
}

func (s *Server) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&s.dir, "dir", "public", "directory to serve")
	fs.StringVar(&s.streamAddr, "stream.addr", "stream:80", "url to connect to stream")
}

func (s Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	remote := r.Header.Get("x-forwarded-for")
	if remote == "" {
		remote = r.RemoteAddr
	}

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

	httpRequest := &stream.HTTPRequest{
		Timestamp: time.Now().Format(time.RFC3339),
		Method:    r.Method,
		Domain:    r.Host,
		Path:      r.URL.Path,
		Remote:    remote,
		UserAgent: r.UserAgent(),
		Referrer:  r.Referer(),
	}

	_, err := s.client.LogHTTP(ctx, httpRequest)
	if err != nil {
		s.log.Error().Err(err).Msg("write to stream")
	}
}
