package subsonic

import (
	"context"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var ctx = context.Background()

func TestConnectWithPasswordUsesTokenAuth(t *testing.T) {
	s := newFakeServer(t, false)
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame"})
	info, err := c.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.AuthMethod() != AuthToken {
		t.Fatalf("method = %v, want token", c.AuthMethod())
	}
	if info.Type != "navidrome" || !info.OpenSubsonic || !info.HasExtension("apiKeyAuthentication") {
		t.Fatalf("info = %+v", info)
	}
	if q := s.query("ping"); q.Has("p") || q.Get("v") != APIVersion || q.Get("c") != DefaultClientName || q.Get("f") != "json" {
		t.Fatalf("ping query = %v", q)
	}
}

func TestConnectWithStoredTokenPair(t *testing.T) {
	s := newFakeServer(t, false)
	tok, salt := NewTokenPair("sesame")
	c := newTestClient(t, s, Credentials{Username: "alice", Token: tok, Salt: salt})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if got := s.query("ping").Get("s"); got != salt {
		t.Fatalf("salt sent = %q, want the stored %q", got, salt)
	}
}

func TestConnectAPIKey(t *testing.T) {
	s := newFakeServer(t, false)
	s.apiKey = "key-123"
	c := newTestClient(t, s, Credentials{APIKey: "key-123"})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if c.AuthMethod() != AuthAPIKey {
		t.Fatalf("method = %v", c.AuthMethod())
	}
	if s.query("ping").Has("u") {
		t.Fatal("u must not be sent with apiKey (OpenSubsonic error 43)")
	}
}

func TestAPIKeyUnsupportedFallsBackToToken(t *testing.T) {
	s := newFakeServer(t, false)
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame", APIKey: "key-123"})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if c.AuthMethod() != AuthToken {
		t.Fatalf("method = %v, want token", c.AuthMethod())
	}
}

func TestInvalidAPIKeyIsAnError(t *testing.T) {
	s := newFakeServer(t, false)
	s.apiKey = "key-123"
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame", APIKey: "wrong"})
	_, err := c.Connect(ctx)
	if Classify(err) != KindAuth {
		t.Fatalf("err = %v (kind %v), want auth error", err, Classify(err))
	}
}

func TestTokenUnsupportedOverHTTPRefusesPlaintext(t *testing.T) {
	s := newFakeServer(t, false)
	s.tokenUnsupported = true
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame"})
	_, err := c.Connect(ctx)
	if !errors.Is(err, ErrPlaintextRefused) || Classify(err) != KindPlaintextRefused {
		t.Fatalf("err = %v, want ErrPlaintextRefused", err)
	}
}

func TestTokenUnsupportedFallsBackToPlaintextWhenAllowed(t *testing.T) {
	s := newFakeServer(t, false)
	s.tokenUnsupported = true
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame", AllowPlaintext: true})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if c.AuthMethod() != AuthPlain || !strings.HasPrefix(s.query("ping").Get("p"), "enc:") {
		t.Fatalf("method = %v, p = %q", c.AuthMethod(), s.query("ping").Get("p"))
	}
}

func TestTokenUnsupportedFallsBackToPlaintextOverHTTPS(t *testing.T) {
	s := newFakeServer(t, true)
	s.tokenUnsupported = true
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame"})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if c.AuthMethod() != AuthPlain {
		t.Fatalf("method = %v, want plain", c.AuthMethod())
	}
}

func TestWrongPassword(t *testing.T) {
	s := newFakeServer(t, false)
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "nope"})
	_, err := c.Connect(ctx)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != CodeWrongCredentials || Classify(err) != KindAuth {
		t.Fatalf("err = %v", err)
	}
}

func TestNoCredentials(t *testing.T) {
	s := newFakeServer(t, false)
	c := newTestClient(t, s, Credentials{Username: "alice"})
	if _, err := c.Connect(ctx); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("err = %v", err)
	}
}

func TestInvalidURL(t *testing.T) {
	for _, u := range []string{"", "music.local:4533", "ftp://x", "http://"} {
		if _, err := New(Options{BaseURL: u}); err == nil {
			t.Errorf("New(%q) succeeded", u)
		}
	}
}

func TestBaseURLWithPathPrefix(t *testing.T) {
	s := newFakeServer(t, false)
	c, _ := New(Options{BaseURL: s.URL + "/navidrome/", Credentials: Credentials{Username: "alice", Password: "sesame"}, HTTPClient: s.Client()})
	u := c.StreamURL("so-1", StreamOptions{Format: "raw"})
	if !strings.HasPrefix(u, s.URL+"/navidrome/rest/stream.view?") {
		t.Fatalf("stream URL = %s", u)
	}
}

func TestNonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<html>502 Bad Gateway</html>"))
	}))
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL, Credentials: Credentials{Username: "a", Password: "b"}})
	_, err := c.Connect(ctx)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v, want mention of HTTP 502", err)
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL, Credentials: Credentials{Username: "a", Password: "b"}, Timeout: 100 * time.Millisecond})
	_, err := c.Connect(ctx)
	if Classify(err) != KindTimeout {
		t.Fatalf("err = %v (kind %v), want timeout", err, Classify(err))
	}
}

func TestUnreachable(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close() // nothing listens here now
	c, _ := New(Options{BaseURL: "http://" + addr, Credentials: Credentials{Username: "a", Password: "b"}})
	_, err := c.Connect(ctx)
	if Classify(err) != KindUnreachable {
		t.Fatalf("err = %v (kind %v), want unreachable", err, Classify(err))
	}
}

func writeServerCA(t *testing.T, s *fakeServer) string {
	p := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw})
	if err := os.WriteFile(p, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTLSSelfSignedRejectedByDefault(t *testing.T) {
	s := newFakeServer(t, true)
	c, _ := New(Options{BaseURL: s.URL, Credentials: Credentials{Username: "alice", Password: "sesame"}})
	_, err := c.Connect(ctx)
	if Classify(err) != KindTLS {
		t.Fatalf("err = %v (kind %v), want TLS", err, Classify(err))
	}
}

func TestTLSSelfSignedTrustedViaCAFile(t *testing.T) {
	s := newFakeServer(t, true)
	c, err := New(Options{BaseURL: s.URL, Credentials: Credentials{Username: "alice", Password: "sesame"}, CAFile: writeServerCA(t, s)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTLSInsecureSkipVerify(t *testing.T) {
	s := newFakeServer(t, true)
	c, _ := New(Options{BaseURL: s.URL, Credentials: Credentials{Username: "alice", Password: "sesame"}, InsecureSkipVerify: true})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTLSBadCAFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "junk.pem")
	os.WriteFile(p, []byte("not a certificate"), 0o600)
	if _, err := New(Options{BaseURL: "https://x.example", CAFile: p}); err == nil {
		t.Fatal("New accepted a CA file without certificates")
	}
	if _, err := New(Options{BaseURL: "https://x.example", CAFile: "/does/not/exist.pem"}); err == nil {
		t.Fatal("New accepted a missing CA file")
	}
}

func TestEmbeddedRootsParse(t *testing.T) {
	pool, err := tlsConfig("", false)
	if err != nil || pool.RootCAs == nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(embeddedRoots), "BEGIN CERTIFICATE"); n < 100 {
		t.Fatalf("embedded bundle has only %d certificates", n)
	}
}

func TestRedactURL(t *testing.T) {
	s := newFakeServer(t, false)
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame"})
	red := RedactURL(c.StreamURL("so-1", StreamOptions{Format: "raw"}))
	if strings.Contains(red, c.creds.Token) || strings.Contains(red, c.creds.Salt) {
		t.Fatalf("token or salt leaked: %s", red)
	}
	if !strings.Contains(red, "id=so-1") {
		t.Fatalf("redaction removed non-secret params: %s", red)
	}
}

func TestCheckStreamResponse(t *testing.T) {
	mk := func(ct, body string) *http.Response {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", ct)
		rec.WriteString(body)
		return rec.Result()
	}
	if err := CheckStreamResponse(mk("audio/flac", "fLaC")); err != nil {
		t.Fatalf("audio rejected: %v", err)
	}
	err := CheckStreamResponse(mk("application/json", `{"subsonic-response":{"status":"failed","error":{"code":70,"message":"not found"}}}`))
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != CodeNotFound {
		t.Fatalf("err = %v, want APIError 70", err)
	}
	if err := CheckStreamResponse(mk("text/xml", `<subsonic-response status="failed"/>`)); err == nil {
		t.Fatal("xml error document accepted")
	}
}

func TestErrorsDoNotLeakCredentials(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	c, _ := New(Options{BaseURL: "http://" + addr, Credentials: Credentials{Username: "alice", Password: "sesame", APIKey: "key-123"}})
	_, err := c.Connect(ctx)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, secret := range []string{c.creds.Token, c.creds.Salt, "key-123", "sesame"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error text leaks a credential: %v", err)
		}
	}
	if Classify(err) != KindUnreachable {
		t.Fatalf("redaction broke classification: %v", Classify(err))
	}
}
