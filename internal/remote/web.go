package remote

import (
	"embed"
	"net/http"
)

// The page: three embedded files, no build step. app_test.js sits beside them
// but is not embedded.
//
//go:embed web/index.html web/app.js web/app.css
var webFS embed.FS

// static serves one embedded file. no-cache makes the browser ask every time
// (a 304 costs nothing), so an app update shows at once.
func static(name, contentType string) http.HandlerFunc {
	b, err := webFS.ReadFile("web/" + name)
	if err != nil {
		panic(err) // the file is embedded: a build-time mistake
	}
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", contentType)
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Content-Type-Options", "nosniff")
		w.Write(b)
	}
}
