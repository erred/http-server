package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var port, httpsdomain, dir string

func main() {
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
		opath := r.URL.Path
		r.URL.Path = strings.TrimSuffix(r.URL.Path, ".html")
		r.URL.Path = strings.TrimSuffix(r.URL.Path, "index")
		if opath != r.URL.Path {
			log.Printf("%s %s %s %s %s: ", r.RemoteAddr, r.Header.Get("user-agent"), r.Proto, r.Method, r.URL)
			log.Printf("redirect %v to %v\n", opath, r.URL.Path)
			http.Redirect(w, r, r.URL.Path, http.StatusFound)
			return
		}

		if fi, err := os.Stat(filepath.Join(dir, r.URL.Path)); err == nil && !fi.IsDir() {
			log.Printf("%s %s %s %s %s: %s\n", r.RemoteAddr, r.Header.Get("user-agent"), r.Proto, r.Method, r.URL, filepath.Join(dir, r.URL.Path))
			http.ServeFile(w, r, filepath.Join(dir, r.URL.Path))
		} else if fi, err := os.Stat(filepath.Join(dir, r.URL.Path+".html")); err == nil && !fi.IsDir() {
			log.Printf("%s %s %s %s %s: %s\n", r.RemoteAddr, r.Header.Get("user-agent"), r.Proto, r.Method, r.URL, filepath.Join(dir, r.URL.Path+".html"))
			http.ServeFile(w, r, filepath.Join(dir, r.URL.Path+".html"))
		} else if fi, err := os.Stat(filepath.Join(dir, r.URL.Path, "index.html")); err == nil && !fi.IsDir() {
			log.Printf("%s %s %s %s %s: %s\n", r.RemoteAddr, r.Header.Get("user-agent"), r.Proto, r.Method, r.URL, filepath.Join(dir, r.URL.Path, "index.html"))
			http.ServeFile(w, r, filepath.Join(dir, r.URL.Path, "index.html"))
		} else {
			log.Printf("%s %s %s %s %s\n", r.RemoteAddr, r.Header.Get("user-agent"), r.Proto, r.Method, r.URL)
			http.Error(w, "Not Found", http.StatusNotFound)
		}
	})

	log.Println("serving", dir)
	if httpsdomain != "" {
		go func() {
			log.Println("starting https server on :443")
			pub := "/etc/letsencrypt/live/" + httpsdomain + "/fullchain.pem"
			priv := "/etc/letsencrypt/live/" + httpsdomain + "/privkey.pem"
			log.Fatal(http.ListenAndServeTLS(":443", pub, priv, nil))
		}()
	}

	log.Println("starting http server on " + port)
	log.Fatal(http.ListenAndServe(port, nil))
}
