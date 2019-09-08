package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var (
	Dir string
)

func main() {
	if len(os.Args) < 2 {
		Dir = "dst"
	} else {
		Dir = os.Args[1]
	}

	p := os.Getenv("PORT")
	if p == "" {
		p = ":8080"
	}
	if p[0] != ':' {
		p = ":" + p
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		opath := r.URL.Path
		r.URL.Path = strings.TrimSuffix(r.URL.Path, ".html")
		r.URL.Path = strings.TrimSuffix(r.URL.Path, "index")
		if opath != r.URL.Path {
			log.Printf("redirect %v to %v\n", opath, r.URL.Path)
			http.Redirect(w, r, r.URL.Path, http.StatusFound)
			return
		}

		if fi, err := os.Stat(filepath.Join(Dir, r.URL.Path)); err == nil && !fi.IsDir() {
			log.Printf("served %v\n", filepath.Join(Dir, r.URL.Path))
			http.ServeFile(w, r, filepath.Join(Dir, r.URL.Path))
		} else if fi, err := os.Stat(filepath.Join(Dir, r.URL.Path+".html")); err == nil && !fi.IsDir() {
			log.Printf("served %v\n", filepath.Join(Dir, r.URL.Path+".html"))
			http.ServeFile(w, r, filepath.Join(Dir, r.URL.Path+".html"))
		} else if fi, err := os.Stat(filepath.Join(Dir, r.URL.Path, "index.html")); err == nil && !fi.IsDir() {
			log.Printf("served %v\n", filepath.Join(Dir, r.URL.Path, "index.html"))
			http.ServeFile(w, r, filepath.Join(Dir, r.URL.Path, "index.html"))
		} else {
			log.Printf("404: %v\n", r.URL.Path)
			http.Error(w, "Not Found", http.StatusNotFound)
		}
	})
	if os.Getenv("HTTPS") != "" {
		go http.ListenAndServeTLS(":443", "/etc/letsencrypt/live/"+os.Getenv("HTTPS")+"/fullchain.pem", "/etc/letsencrypt/live/"+os.Getenv("HTTPS")+"/privkey.pem", nil)
	}

	log.Fatal(http.ListenAndServe(p, nil))
}
