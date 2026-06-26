// Package spa serves the embedded Vue 3 web UI build output.
package spa

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed dist/*
var distDir embed.FS

// Handler serves the SPA with proper MIME types and SPA-routing fallback.
func Handler() http.Handler {
	sub, err := fs.Sub(distDir, "dist")
	if err != nil {
		panic("spa: embed dist: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		f, err := sub.Open(path)
		if err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		index, ierr := sub.Open("index.html")
		if ierr != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer index.Close()
		data, _ := io.ReadAll(index)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", timeZero(), bytes.NewReader(data))
	})
}

func timeZero() time.Time { return time.Time{} }
