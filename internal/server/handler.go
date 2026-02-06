package server

import "net/http"

// WrapBasePath mounts the handler under the provided base path.
func WrapBasePath(base string, handler http.Handler) http.Handler {
	if base == "" {
		return handler
	}

	root := http.NewServeMux()
	root.Handle(base+"/", http.StripPrefix(base, handler))
	root.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, base+"/", http.StatusMovedPermanently)
	})
	return root
}
