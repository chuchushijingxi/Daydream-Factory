package httpapi

import (
	"net/http"
	"path"
	"strings"
)

// Serve only shipped shells and code/static resources. Local QA builds and
// prototypes in the shared demo directory are not production entry points.
func staticShell(directory string) http.Handler {
	files := http.StripPrefix("/demo/", http.FileServer(http.Dir(directory)))
	roots := map[string]bool{".": true, "index.html": true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean(strings.TrimPrefix(r.URL.Path, "/demo/"))
		allowed := roots[name] || name == "app" || strings.HasPrefix(name, "app/") || strings.HasPrefix(name, "assets/")
		if !allowed || (strings.HasSuffix(r.URL.Path, "/") && name != "." && name != "app") {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}
