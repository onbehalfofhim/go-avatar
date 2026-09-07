package http

import (
	"io/fs"
	"net/http"
)

func NewWebHandler(staticFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(staticFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.ServeFileFS(w, r, staticFS, "index.html")
			return
		}

		fileServer.ServeHTTP(w, r)
	})
}
