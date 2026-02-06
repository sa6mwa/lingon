package webui

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed static/*
var staticFS embed.FS

// Options configures web UI asset serving behavior.
type Options struct {
	NoBanner bool
}

type indexTemplateData struct {
	Title      string
	ShowBanner bool
}

// Handler returns a file server for the embedded UI assets.
func Handler() http.Handler {
	return HandlerWithOptions(Options{})
}

// HandlerWithOptions returns a file server for the embedded UI assets with options.
func HandlerWithOptions(opts Options) http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return http.NotFoundHandler()
	}
	indexBytes, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return http.NotFoundHandler()
	}
	indexTmpl, err := template.New("index.html").Parse(string(indexBytes))
	if err != nil {
		return http.NotFoundHandler()
	}
	return &handler{
		files:     http.FileServer(http.FS(sub)),
		indexTmpl: indexTmpl,
		opts:      opts,
	}
}

type handler struct {
	files     http.Handler
	indexTmpl *template.Template
	opts      Options
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.files.ServeHTTP(w, r)
		return
	}
	assetPath, ok := resolveAssetPath(r.URL.Path)
	if !ok {
		h.files.ServeHTTP(w, r)
		return
	}
	if assetPath != "index.html" {
		h.files.ServeHTTP(w, r)
		return
	}
	title := "Lingon"
	if h.opts.NoBanner {
		title = "Service"
	}
	data := indexTemplateData{
		Title:      title,
		ShowBanner: !h.opts.NoBanner,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.indexTmpl.Execute(w, data); err != nil {
		http.Error(w, "template render failed", http.StatusInternalServerError)
	}
}

func resolveAssetPath(requestPath string) (string, bool) {
	cleaned := path.Clean("/" + requestPath)
	if cleaned == "/" || cleaned == "." {
		return "index.html", true
	}
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == "." {
		return "index.html", true
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", false
	}
	if strings.HasSuffix(requestPath, "/") {
		return path.Join(cleaned, "index.html"), true
	}
	return cleaned, true
}
