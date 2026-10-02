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

// pageCSP lets the page load only its own files and data: images (covers);
// no inline script or style, no other origin, and no framing.
const pageCSP = "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'"

// static serves one embedded file. no-cache makes the browser ask every time,
// so an app update shows at once. No ETag is sent, so each ask downloads the
// file again (about 40 KB for all three); that is fine on a home network.
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
		h.Set("Content-Security-Policy", pageCSP)
		h.Set("X-Frame-Options", "DENY")
		w.Write(b)
	}
}
