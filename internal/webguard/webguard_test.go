package webguard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHostAllowedIPLiteralsAndLocalhost(t *testing.T) {
	for host, want := range map[string]bool{
		"192.168.1.5:8090": true, "[fe80::1]:8090": true, "10.0.0.2": true, "localhost:8090": true, "[::1]:80": true, "::1": true,
		"evil.example:8090": false, "localhost.": false, "127.0.0.1.nip.io:8090": false, "": false,
	} {
		if got := HostAllowed(host); got != want {
			t.Errorf("HostAllowed(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestHostAllowedExtraNames(t *testing.T) {
	extra := []string{"mister", "mister.local"}
	for host, want := range map[string]bool{
		"mister:8080": true, "mister.local:8080": true, "mister.local": true, "MISTER.local:8080": true,
		"mister.example.com:8080": false, "other.local:8080": false, "mister.local.evil.example": false, "xmister": false,
	} {
		if got := HostAllowed(host, extra...); got != want {
			t.Errorf("HostAllowed(%q, %v) = %v, want %v", host, extra, got, want)
		}
	}
	if HostAllowed("mister:8080") {
		t.Error("a hostname is allowed only when the caller names it")
	}
}

func TestHostnamesAreTheMachinesWithAndWithoutLocal(t *testing.T) {
	name, err := os.Hostname()
	if err != nil || name == "" {
		t.Skip("no hostname on this machine")
	}
	got := Hostnames()
	if len(got) != 2 || got[0] != name || got[1] != name+".local" {
		t.Fatalf("Hostnames() = %v, want [%s %s.local]", got, name, name)
	}
	if !HostAllowed(name+":8080", got...) || !HostAllowed(name+".local", got...) {
		t.Fatal("the machine's own names must pass")
	}
}

func TestCheckHostRefusesForeignNames(t *testing.T) {
	reached := 0
	h := CheckHost(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached++ }), "mister.local")
	for host, want := range map[string]int{
		"127.0.0.1:8080": 200, "localhost:8080": 200, "mister.local:8080": 200, "192.168.1.9": 200,
		"evil.example:8080": 403, "mister.local.evil.example": 403,
	} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Host %q: status %d, want %d", host, rec.Code, want)
		}
	}
	if reached != 4 {
		t.Fatalf("handler reached %d times, want 4", reached)
	}
}

func TestSameOrigin(t *testing.T) {
	for name, tc := range map[string]struct {
		hdr  map[string]string
		want bool
	}{
		"no headers (curl)":    {nil, true},
		"same origin":          {map[string]string{"Origin": "http://192.168.1.5:8080", "Sec-Fetch-Site": "same-origin"}, true},
		"typed in the address": {map[string]string{"Sec-Fetch-Site": "none"}, true},
		"origin of the host":   {map[string]string{"Origin": "http://192.168.1.5:8080"}, true},
		"foreign origin":       {map[string]string{"Origin": "http://evil.example"}, false},
		"other port":           {map[string]string{"Origin": "http://192.168.1.5:9999"}, false},
		"cross-site fetch":     {map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		"same-site fetch":      {map[string]string{"Sec-Fetch-Site": "same-site"}, false},
		"garbage origin":       {map[string]string{"Origin": "http://%zz"}, false},
		"null origin":          {map[string]string{"Origin": "null"}, false},
	} {
		req := httptest.NewRequest("POST", "http://192.168.1.5:8080/x", strings.NewReader(""))
		for k, v := range tc.hdr {
			req.Header.Set(k, v)
		}
		if got := SameOrigin(req); got != tc.want {
			t.Errorf("%s: SameOrigin = %v, want %v", name, got, tc.want)
		}
	}
}
