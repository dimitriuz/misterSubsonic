package subsonic

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"testing"
)

// fakeServer implements enough of the Subsonic auth rules to test the
// client's fallbacks, and serves testdata/<endpoint>.json for everything else.
type fakeServer struct {
	*httptest.Server
	user, pass       string
	apiKey           string // "" means the server doesn't support API keys (error 42)
	tokenUnsupported bool   // answer token auth with error 41 (LDAP-style users)
	override         map[string]string
	postStatus       int // when set, answer POSTs with this HTTP status and no body

	lastMethod map[string]string // endpoint -> the method of its last request
	lastType   map[string]string // endpoint -> the Content-Type of its last request
	lastURLQ   map[string]string // endpoint -> the raw URL query of its last request

	mu      sync.Mutex
	queries map[string]url.Values
}

func newFakeServer(t *testing.T, tls bool) *fakeServer {
	s := &fakeServer{user: "alice", pass: "sesame", override: map[string]string{}, queries: map[string]url.Values{}, lastMethod: map[string]string{}, lastType: map[string]string{}, lastURLQ: map[string]string{}}
	h := http.HandlerFunc(s.serve)
	if tls {
		s.Server = httptest.NewTLSServer(h)
	} else {
		s.Server = httptest.NewServer(h)
	}
	t.Cleanup(s.Close)
	return s
}

func (s *fakeServer) query(endpoint string) url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queries[endpoint]
}

func (s *fakeServer) request(endpoint string) (method, contentType, urlQuery string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastMethod[endpoint], s.lastType[endpoint], s.lastURLQ[endpoint]
}

func fail(w http.ResponseWriter, code int, msg string) {
	fmt.Fprintf(w, `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":%d,"message":%q}}}`, code, msg)
}

func (s *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	endpoint := strings.TrimSuffix(path.Base(r.URL.Path), ".view")
	r.ParseForm() // the URL query, plus the body of a form POST
	q := r.Form
	s.mu.Lock()
	s.queries[endpoint] = q
	s.lastMethod[endpoint] = r.Method
	s.lastType[endpoint] = r.Header.Get("Content-Type")
	s.lastURLQ[endpoint] = r.URL.RawQuery
	s.mu.Unlock()
	if r.Method == http.MethodPost && s.postStatus != 0 {
		w.WriteHeader(s.postStatus)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	switch {
	case q.Get("apiKey") != "":
		if s.apiKey == "" {
			fail(w, CodeAuthMechUnsupported, "api keys not supported")
			return
		}
		if q.Get("apiKey") != s.apiKey {
			fail(w, CodeInvalidAPIKey, "invalid api key")
			return
		}
	case q.Get("t") != "":
		if s.tokenUnsupported {
			fail(w, CodeTokenAuthUnsupported, "token auth not supported for LDAP users")
			return
		}
		sum := md5.Sum([]byte(s.pass + q.Get("s")))
		if q.Get("u") != s.user || hex.EncodeToString(sum[:]) != q.Get("t") {
			fail(w, CodeWrongCredentials, "wrong username or password")
			return
		}
	case q.Get("p") != "":
		raw, _ := hex.DecodeString(strings.TrimPrefix(q.Get("p"), "enc:"))
		if q.Get("u") != s.user || string(raw) != s.pass {
			fail(w, CodeWrongCredentials, "wrong username or password")
			return
		}
	default:
		fail(w, CodeMissingParameter, "missing auth")
		return
	}

	if body, ok := s.override[endpoint]; ok {
		w.Write([]byte(body))
		return
	}
	switch endpoint {
	case "ping", "star", "unstar", "scrobble", "savePlayQueue":
		w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1","type":"navidrome","serverVersion":"0.58.0","openSubsonic":true}}`))
	case "getOpenSubsonicExtensions":
		w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1","openSubsonicExtensions":[{"name":"apiKeyAuthentication","versions":[1]},{"name":"transcodeOffset","versions":[1]}]}}`))
	default:
		b, err := os.ReadFile("testdata/" + endpoint + ".json")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}
}

func newTestClient(t *testing.T, s *fakeServer, c Credentials) *Client {
	t.Helper()
	cl, err := New(Options{BaseURL: s.URL, Credentials: c, HTTPClient: s.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return cl
}
