package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

//go:embed static
var staticFS embed.FS

// logRequests logs method, path, status and duration for every request.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &loggingWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lw, r)
		slog.Info("http", "method", r.Method, "path", r.URL.Path,
			"status", lw.status, "ms", time.Since(start).Milliseconds())
	})
}

type loggingWriter struct {
	http.ResponseWriter
	status int
}

func (w *loggingWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// recoverPanics converts handler panics into 500s instead of dropped connections.
func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic", "path", r.URL.Path, "err", rec, "stack", string(debug.Stack()))
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// staticHandler serves embedded static assets with long cache headers.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		files.ServeHTTP(w, r)
	})
}
