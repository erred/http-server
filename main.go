package main

import (
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh/terminal"
)

var port, httpsdomain, dir string

func initLog() {
	logfmt := os.Getenv("LOGFMT")
	if logfmt != "json" {
		logfmt = "text"
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, NoColor: !terminal.IsTerminal(int(os.Stdout.Fd()))})
	}

	level, _ := zerolog.ParseLevel(os.Getenv("LOGLVL"))
	if level == zerolog.NoLevel {
		level = zerolog.InfoLevel
	}
	log.Info().Str("FMT", logfmt).Str("LVL", level.String()).Msg("log initialized")
	zerolog.SetGlobalLevel(level)
}

func main() {
	initLog()

	p := os.Getenv("PORT")
	if p == "" {
		p = ":8080"
	}
	flag.StringVar(&port, "port", p, "port to serve http on")
	flag.StringVar(&httpsdomain, "https", "", "letsencrypt domain to look for certs")
	flag.StringVar(&dir, "dir", "dst", "directory to serve")
	flag.Parse()
	if p[0] != ':' {
		p = ":" + p
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {

		f := "Not Found"
		if fi, err := os.Stat(filepath.Join(dir, r.URL.Path)); err == nil && !fi.IsDir() {
			f = fi.Name()
		} else if fi, err := os.Stat(filepath.Join(dir, r.URL.Path+".html")); err == nil && !fi.IsDir() {
			f = fi.Name()
		} else if fi, err := os.Stat(filepath.Join(dir, r.URL.Path, "index.html")); err == nil && !fi.IsDir() {
			f = fi.Name()
		}

		l := log.Info().Timestamp()
		if f == "Not Found" {
			l = log.Error().Timestamp()
		}
		l.Str("remote", r.RemoteAddr).Str("proto", r.Proto).Str("method", r.Method).Str("url", r.URL.String()).Str("agent", r.Header.Get("user-agent"))

		if f == "Not Found" {
			if strings.HasSuffix(r.URL.Path, ".html") || strings.HasSuffix(r.URL.Path, "index") {
				p := strings.TrimSuffix(strings.TrimSuffix(r.URL.Path, ".html"), "index")
				l.Msgf("redirect to %v", p)
				http.Redirect(w, r, p, http.StatusFound)
				return
			}
			http.Error(w, "Not Found", http.StatusNotFound)
			l.Msg("Not Found")
			return
		}
		http.ServeFile(w, r, f)
		l.Msg(f)
	})

	l := log.Info().Str("dir", dir)
	if httpsdomain != "" {
		l = l.Str("https", ":443")
		pub := "/etc/letsencrypt/live/" + httpsdomain + "/fullchain.pem"
		priv := "/etc/letsencrypt/live/" + httpsdomain + "/privkey.pem"
		go http.ListenAndServeTLS(":443", pub, priv, nil)
	}

	l.Str("http", port).Msg("serving")
	http.ListenAndServe(port, nil)
}
