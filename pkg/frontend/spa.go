package frontend

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

func NewSPAHandler(files fs.FS) http.Handler {
	return &spaHandler{files: files}
}

type spaHandler struct {
	files fs.FS
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	filePath := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if filePath == "." || filePath == "/" {
		h.serveFile(w, r, "index.html")
		return
	}

	if h.isFile(filePath) {
		h.serveFile(w, r, filePath)
		return
	}

	if path.Ext(filePath) != "" {
		http.NotFound(w, r)
		return
	}

	h.serveFile(w, r, "index.html")
}

func (h *spaHandler) isFile(name string) bool {
	file, err := h.files.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return false
	}

	return !info.IsDir()
}

func (h *spaHandler) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	content, err := fs.ReadFile(h.files, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}

	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(content)
}
