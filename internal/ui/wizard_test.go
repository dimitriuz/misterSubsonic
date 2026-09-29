package ui

import (
	"crypto/md5"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// fakeServer answers every Subsonic call. mode "auth" refuses the login;
// "token41" refuses token auth (error 41) but takes the plain password.
func fakeServer(mode string, tls bool) *httptest.Server {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch {
		case mode == "auth":
			io.WriteString(w, `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":40,"message":"Wrong username or password"}}}`)
		case mode == "token41" && q.Get("t") != "":
			io.WriteString(w, `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":41,"message":"Token authentication not supported"}}}`)
		default:
			io.WriteString(w, `{"subsonic-response":{"status":"ok","version":"1.16.1","type":"navidrome","serverVersion":"0.59.0"}}`)
		}
	})
	if tls {
		srv := httptest.NewUnstartedServer(h)
		srv.Config.ErrorLog = log.New(io.Discard, "", 0) // the refused handshake is expected
		srv.StartTLS()
		return srv
	}
	return httptest.NewServer(h)
}

func wizardApp(t *testing.T) (*testApp, *connectRecorder, *WizardScreen) {
	t.Helper()
	ta, rec := sessionApp(t, nil)
	ta.o.ConfigErr = config.ErrNotFound
	ta.start()
	w, ok := ta.Top().(*WizardScreen)
	if !ok {
		t.Fatalf("first screen %T, want the wizard", ta.Top())
	}
	return ta, rec, w
}

// fill types the four fields (physical keyboard; Enter is Next) and waits for the test.
func fill(t *testing.T, ta *testApp, url, user, pass, key string) {
	t.Helper()
	typeKeys(ta, url+"\n"+user+"\n"+pass+"\n"+key+"\n")
	ta.settle(t)
}

func readConfig(t *testing.T, ta *testApp) string {
	t.Helper()
	waitFile(t, ta.o.ConfigPath, "[[server]]")
	b, _ := os.ReadFile(ta.o.ConfigPath)
	return string(b)
}

func TestWizardFirstRunStoresATokenAndConnects(t *testing.T) {
	srv := fakeServer("ok", false)
	defer srv.Close()
	ta, rec, w := wizardApp(t)
	fill(t, ta, strings.TrimPrefix(srv.URL, "http://"), "alice", "s3cret", "")
	if w.step != stepTest || w.err != nil || w.auth != subsonic.AuthToken {
		t.Fatalf("step %d err %v auth %v", w.step, w.err, w.auth)
	}
	if w.actions[0].label != "Save and continue" {
		t.Fatalf("actions %v", w.actions)
	}
	ta.press(input.BtnA)
	body := readConfig(t, ta)
	if strings.Contains(body, "s3cret") || !strings.Contains(body, "token = ") || !strings.Contains(body, "salt = ") {
		t.Fatalf("saved config:\n%s", body)
	}
	if !strings.Contains(body, `url = "`+srv.URL+`"`) || !strings.Contains(body, `default_server = "127.0.0.1"`) {
		t.Fatalf("saved config:\n%s", body)
	}
	if len(rec.got) != 1 || rec.got[0].Servers[0].Token == "" {
		t.Fatalf("connects %v", rec.got)
	}
	if messageTitle(ta) != "Connecting…" {
		t.Fatalf("after saving: %q", messageTitle(ta))
	}
}

func TestWizardPlainPasswordOverHTTPNeedsConsent(t *testing.T) {
	srv := fakeServer("token41", false)
	defer srv.Close()
	ta, _, w := wizardApp(t)
	fill(t, ta, srv.URL, "ldapuser", "pw", "")
	if subsonic.Classify(w.err) != subsonic.KindPlaintextRefused || !strings.HasPrefix(w.actions[0].label, "Allow sending the password") {
		t.Fatalf("err %v actions %v", w.err, w.actions)
	}
	ta.press(input.BtnA) // allow
	ta.settle(t)
	if w.err != nil || w.auth != subsonic.AuthPlain {
		t.Fatalf("after allowing: err %v auth %v", w.err, w.auth)
	}
	ta.press(input.BtnA) // save
	body := readConfig(t, ta)
	if !strings.Contains(body, `password = "pw"`) || !strings.Contains(body, "allow_plaintext_password = true") {
		t.Fatalf("saved config:\n%s", body)
	}
}

func TestWizardSelfSignedCertificateOffersInsecure(t *testing.T) {
	srv := fakeServer("ok", true)
	defer srv.Close()
	ta, _, w := wizardApp(t)
	fill(t, ta, srv.URL, "alice", "pw", "")
	if subsonic.Classify(w.err) != subsonic.KindTLS || !strings.Contains(w.actions[0].label, "insecure") {
		t.Fatalf("err %v actions %v", w.err, w.actions)
	}
	ta.press(input.BtnA)
	ta.settle(t)
	if w.err != nil {
		t.Fatalf("insecure retry: %v", w.err)
	}
	ta.press(input.BtnA)
	if body := readConfig(t, ta); !strings.Contains(body, "insecure_skip_verify = true") {
		t.Fatalf("saved config:\n%s", body)
	}
}

func TestWizardWrongPasswordGoesBackToTheLogin(t *testing.T) {
	srv := fakeServer("auth", false)
	defer srv.Close()
	ta, _, w := wizardApp(t)
	fill(t, ta, srv.URL, "alice", "wrong", "")
	if subsonic.Classify(w.err) != subsonic.KindAuth || w.actions[0].label != "Change the username or password" {
		t.Fatalf("err %v actions %v", w.err, w.actions)
	}
	ta.press(input.BtnA)
	if w.step != stepUser || string(w.fields[stepUser]) != "alice" {
		t.Fatalf("step %d, user %q", w.step, string(w.fields[stepUser]))
	}
	if _, err := os.Stat(ta.o.ConfigPath); err == nil {
		t.Fatal("a failed test saved the config")
	}
}

func TestWizardChecksTheFields(t *testing.T) {
	ta, _, w := wizardApp(t)
	typeKeys(ta, "ftp://x\n")
	if w.step != stepURL || w.problem == "" {
		t.Fatalf("ftp accepted: step %d problem %q", w.step, w.problem)
	}
	for range len("ftp://x") {
		typeKeys(ta, "\b")
	}
	typeKeys(ta, "music.example.com/rest/\n\n\n\n") // no user, password or key
	if w.step != stepAPIKey || !strings.Contains(w.problem, "password") {
		t.Fatalf("step %d problem %q", w.step, w.problem)
	}
	if got := string(w.fields[stepURL]); got != "http://music.example.com" {
		t.Fatalf("url normalized to %q", got)
	}
}

func TestWizardOnScreenKeysAndBack(t *testing.T) {
	ta, _, w := wizardApp(t)
	ta.press(input.BtnRight) // presets row: https://
	ta.press(input.BtnA)
	if got := string(w.fields[stepURL]); got != "https://" {
		t.Fatalf("preset typed %q", got)
	}
	typeKeys(ta, "music.example.com\nalice")
	ta.press(input.BtnB) // back to the address, fields kept
	if w.step != stepURL || string(w.fields[stepUser]) != "alice" {
		t.Fatalf("B: step %d user %q", w.step, string(w.fields[stepUser]))
	}
	ta.press(input.BtnB) // the first screen: B has nowhere to go
	if ta.Top() != w {
		t.Fatalf("B on the first step left the wizard: %T", ta.Top())
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	typeKeys(ta, "\n")
	for range 5 {
		ta.press(input.BtnDown) // the bottom row
	}
	ta.press(input.BtnRight)
	ta.press(input.BtnRight) // ABC
	ta.press(input.BtnA)
	ta.press(input.BtnUp)
	ta.press(input.BtnUp)
	ta.press(input.BtnA)
	got := []rune(string(w.fields[stepUser]))
	if len(got) != 6 || !unicode.IsUpper(got[5]) {
		t.Fatalf("after switching to upper case the key typed %q", string(got))
	}
}

func TestWizardReplacesAnInvalidConfig(t *testing.T) {
	srv := fakeServer("ok", false)
	defer srv.Close()
	ta, rec := sessionApp(t, nil)
	os.WriteFile(ta.o.ConfigPath, []byte("this is = not toml ["), 0o600)
	ta.o.ConfigErr = errors.New("config: invalid TOML syntax at line 1")
	ta.start()
	ta.press(input.BtnA) // set up again
	fill(t, ta, srv.URL, "alice", "pw", "")
	ta.press(input.BtnA) // save
	ta.settle(t)
	olds, _ := filepath.Glob(ta.o.ConfigPath + ".invalid-*")
	if len(olds) != 1 {
		t.Fatalf("backups %v", olds)
	}
	if b, _ := os.ReadFile(olds[0]); string(b) != "this is = not toml [" {
		t.Fatalf("backup holds %q", b)
	}
	if _, _, err := config.Load(ta.o.ConfigPath); err != nil {
		t.Fatalf("new config: %v", err)
	}
	if len(rec.got) != 1 {
		t.Fatalf("connects %d", len(rec.got))
	}
}

func TestGoldenWizard(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		w := NewWizardScreen(true, false)
		ta.Push(w)
		typeKeys(ta, "http://192.168.1.10:4533")
		golden(t, "wizard-url-"+p.Name, ta.settle(t))
		w.fields[stepURL] = []rune("ftp://x")
		w.next(ta.App)
		golden(t, "wizard-url-problem-"+p.Name, ta.settle(t))
		w.problem = ""
		w.fields[stepURL] = []rune("https://music.example.com")
		w.step, w.err = stepTest, x509.UnknownAuthorityError{}
		w.actions = w.resultActions()
		golden(t, "wizard-error-"+p.Name, ta.settle(t))
	}
}

// People type addresses every which way.
func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.1.10:4533":                   "http://192.168.1.10:4533",
		"  https://music.example.com/  ":      "https://music.example.com",
		"https://music.example.com/rest":      "https://music.example.com",
		"https://example.com/navidrome/rest/": "https://example.com/navidrome",
		"HTTP://Music.Example.com:4533":       "http://Music.Example.com:4533",
		"http://[::1]:4533":                   "http://[::1]:4533",
		"https://alice:pw@music.example.com":  "https://music.example.com",
	} {
		got, err := normalizeURL(in)
		if err != nil || got != want {
			t.Errorf("normalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://x", "http://", "://x", ":4533", "http://:4533"} {
		if _, err := normalizeURL(bad); err == nil {
			t.Errorf("normalizeURL(%q) accepted", bad)
		}
	}
}

// A password is kept exactly as typed (spaces, symbols, any script): the
// token the server checks is md5(password + salt) of those bytes.
func TestWizardPasswordIsKeptExactly(t *testing.T) {
	const pw = " pä$$ wörd:密码 "
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		sum := md5.Sum([]byte(pw + q.Get("s")))
		if q.Get("t") != hex.EncodeToString(sum[:]) {
			io.WriteString(w, `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":40,"message":"Wrong username or password"}}}`)
			return
		}
		io.WriteString(w, `{"subsonic-response":{"status":"ok","version":"1.16.1"}}`)
	}))
	defer srv.Close()
	ta, _, w := wizardApp(t)
	fill(t, ta, srv.URL, "alice", pw, "")
	if w.err != nil {
		t.Fatalf("the server rejected the typed password: %v", w.err)
	}
	ta.press(input.BtnA)
	body := readConfig(t, ta)
	cfg, _, err := config.Load(ta.o.ConfigPath)
	if err != nil {
		t.Fatalf("saved config doesn't load: %v\n%s", err, body)
	}
	s := cfg.Servers[0]
	sum := md5.Sum([]byte(pw + s.Salt))
	if s.Token != hex.EncodeToString(sum[:]) || s.Password != "" {
		t.Fatalf("saved token doesn't match the typed password: %+v", s)
	}
}

// Consent to skip the certificate check or to send the password in plain
// text is for one address; another host must ask again.
func TestWizardInsecureConsentDoesNotCarryToAnotherHost(t *testing.T) {
	a, b := fakeServer("ok", true), fakeServer("ok", true)
	defer a.Close()
	defer b.Close()
	ta, _, w := wizardApp(t)
	fill(t, ta, a.URL, "alice", "pw", "")
	ta.press(input.BtnA) // allow insecure
	ta.settle(t)
	if w.err != nil {
		t.Fatalf("insecure retry: %v", w.err)
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Back -> API key step
	if w.step != stepAPIKey {
		t.Fatalf("step %d", w.step)
	}
	ta.press(input.BtnB)
	ta.press(input.BtnB)
	ta.press(input.BtnB)
	if w.step != stepURL {
		t.Fatalf("step %d", w.step)
	}
	for range len(w.fields[stepURL]) {
		typeKeys(ta, "\b")
	}
	typeKeys(ta, b.URL+"\n\n\n\n")
	ta.settle(t)
	if subsonic.Classify(w.err) != subsonic.KindTLS || !strings.Contains(w.actions[0].label, "insecure") {
		t.Fatalf("another host: err %v actions %v", w.err, w.actions)
	}
	if w.server().InsecureSkipVerify {
		t.Fatal("insecure consent carried to another host")
	}
	if _, err := os.Stat(ta.o.ConfigPath); err == nil {
		t.Fatal("saved")
	}
}

func TestWizardPlaintextConsentDoesNotCarryToAnotherHost(t *testing.T) {
	a, b := fakeServer("token41", false), fakeServer("token41", false)
	defer a.Close()
	defer b.Close()
	ta, _, w := wizardApp(t)
	fill(t, ta, a.URL, "u", "pw", "")
	ta.press(input.BtnA) // allow plaintext
	ta.settle(t)
	if w.err != nil || !w.server().AllowPlaintextPassword {
		t.Fatalf("after allowing: %v", w.err)
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Back
	for range 4 {
		ta.press(input.BtnB)
	}
	for range len(w.fields[stepURL]) {
		typeKeys(ta, "\b")
	}
	typeKeys(ta, b.URL+"\n\n\n\n")
	ta.settle(t)
	if subsonic.Classify(w.err) != subsonic.KindPlaintextRefused || w.server().AllowPlaintextPassword {
		t.Fatalf("another host: err %v plaintext %v", w.err, w.server().AllowPlaintextPassword)
	}
}

// The keyboard stays inside the title-safe area, with or without a problem line.
func TestWizardKeyboardFitsTheScreen(t *testing.T) {
	crt288 := ProfileCRT240
	crt288.H = 288
	for _, p := range []Profile{ProfileHDMI, ProfileCRT240, crt288} {
		for name, setup := range map[string]func(w *WizardScreen){
			"url":         func(w *WizardScreen) {},
			"url problem": func(w *WizardScreen) { w.fields[stepURL] = []rune("ftp://x"); w.next(nil) },
			"password":    func(w *WizardScreen) { w.setStep(stepPassword) },
			"password problem": func(w *WizardScreen) {
				w.setStep(stepPassword)
				w.problem = "Enter a password (step 3) or an API key"
			},
		} {
			ta := newTestApp(t, p)
			w := NewWizardScreen(true, false)
			ta.Push(w)
			setup(w)
			if strings.Contains(name, "problem") && w.problem == "" {
				t.Fatalf("%s %d: no problem shown", p.Name, p.H)
			}
			ta.settle(t)
			body := gfx.R(0, p.SafeY, p.W, p.H-2*p.SafeY)
			kb := w.kbArea
			if kb.H == 0 || kb.Bottom() > body.Bottom() || kb.Y < body.Y || kb.Right() > body.Right() {
				t.Errorf("%s %d %s: keyboard %+v outside %+v", p.Name, p.H, name, kb, body)
			}
		}
	}
}
