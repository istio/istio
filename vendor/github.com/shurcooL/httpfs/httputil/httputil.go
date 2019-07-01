// Package httputil implements HTTP utility functions for http.FileSystem.
package httputil

import (
	"log"
	"net/http"

	"github.com/shurcooL/httpgzip"
)

// FileHandler is an http.Handler that serves the root of File,
// which is expected to be a normal file (not a directory).
type FileHandler struct {
	File http.FileSystem
}

func (h FileHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	f, err := h.File.Open("/")
	if err != nil {
		log.Printf("FileHandler.File.Open('/'): %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		log.Printf("FileHandler.File.Stat('/'): %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpgzip.ServeContent(w, req, fi.Name(), fi.ModTime(), f)
}
