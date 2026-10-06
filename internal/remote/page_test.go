package remote

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestPageServed(t *testing.T) {
	s, _ := browse(&fakeLib{})
	for _, c := range []struct{ path, typ, sniff string }{
		{"/", "text/html; charset=utf-8", "<!doctype html>"},
		{"/app.js", "text/javascript; charset=utf-8", "use strict"},
		{"/app.css", "text/css; charset=utf-8", "--accent"},
	} {
		r := do(s.Handler(), "GET", c.path, "", nil)
		if r.Code != 200 {
			t.Fatalf("%s: %d", c.path, r.Code)
		}
		if got := r.Header().Get("Content-Type"); got != c.typ {
			t.Errorf("%s content type %q, want %q", c.path, got, c.typ)
		}
		if got := r.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s cache control %q", c.path, got)
		}
		if got := r.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s nosniff %q", c.path, got)
		}
		if got := r.Header().Get("Content-Security-Policy"); got != "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'" {
			t.Errorf("%s CSP %q", c.path, got)
		}
		if got := r.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s X-Frame-Options %q", c.path, got)
		}
		if !strings.Contains(r.Body.String(), c.sniff) {
			t.Errorf("%s does not contain %q", c.path, c.sniff)
		}
	}
	// Only those paths, only GET; anything else is still a 404 or 405.
	if r := do(s.Handler(), "GET", "/nope", "", nil); r.Code != 404 {
		t.Errorf("/nope: %d", r.Code)
	}
	if r := do(s.Handler(), "GET", "/app_test.js", "", nil); r.Code != 404 {
		t.Errorf("/app_test.js: %d", r.Code)
	}
}

func TestPageIsSelfContained(t *testing.T) {
	ext := regexp.MustCompile(`(?i)(https?:)?//[a-z0-9.-]+\.[a-z]|https?://`)
	total := 0
	for _, f := range []string{"index.html", "app.js", "app.css"} {
		b, err := webFS.ReadFile("web/" + f)
		if err != nil {
			t.Fatal(err)
		}
		total += len(b)
		text := strings.ReplaceAll(string(b), "http://www.w3.org/2000/svg", "")
		if m := ext.FindString(text); m != "" {
			t.Errorf("%s references an external URL: %q", f, m)
		}
	}
	if total >= 60<<10 {
		t.Errorf("embedded page is %d bytes, want under 60 KB", total)
	}
	t.Logf("embedded page: %d bytes", total)
}

// TestPageJS runs the page's own unit tests with node, when node is installed.
func TestPageJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	if _, err := os.Stat("web/app_test.js"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--test", "web/app_test.js")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node --test: %v\n%s", err, out)
	}
}

// TestPageNeedsNoInlineCode: the CSP forbids inline scripts, styles and event
// handlers, so the page must not use any.
func TestPageNeedsNoInlineCode(t *testing.T) {
	html, _ := webFS.ReadFile("web/index.html")
	js, _ := webFS.ReadFile("web/app.js")
	for _, tag := range regexp.MustCompile(`(?i)<script[^>]*>`).FindAllString(string(html), -1) {
		if !strings.Contains(tag, " src=") {
			t.Errorf("inline script: %s", tag)
		}
	}
	for _, re := range []string{`(?i)<style`, `(?i)\son[a-z]+\s*=`, `(?i)\sstyle\s*=`, `(?i)javascript:`} {
		if m := regexp.MustCompile(re).FindString(string(html)); m != "" {
			t.Errorf("index.html matches %s: %q", re, m)
		}
	}
	for _, re := range []string{`setAttribute\(\s*['"]style`, `\bstyle\s*:`, `\.cssText`, `insertAdjacentHTML|innerHTML|outerHTML|eval\(|new Function`} {
		if m := regexp.MustCompile(re).FindString(string(js)); m != "" {
			t.Errorf("app.js matches %s: %q", re, m)
		}
	}
}

// The Screenshot button takes its own line above the cover. Pinned over the
// corner of the page it sat on the cover of a phone's Now Playing.
func TestScreenshotButtonDoesNotOverlayTheCover(t *testing.T) {
	b, err := webFS.ReadFile("web/app.css")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^\.shot\s*\{([^}]*)\}`).FindStringSubmatch(string(b))
	if m == nil {
		t.Fatal("no .shot rule")
	}
	if strings.Contains(m[1], "position: absolute") || strings.Contains(m[1], "position:absolute") {
		t.Fatalf(".shot is positioned over the page: %s", m[1])
	}
	if !strings.Contains(m[1], "margin-left: auto") {
		t.Fatalf(".shot does not sit at the right of its own line: %s", m[1])
	}
}
