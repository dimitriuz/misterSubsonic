package ui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// The setup wizard (spec §8.2): server URL → username → password → API key
// (optional) → test → save. It runs on first start, when the config file
// is broken (the old file is kept as config.toml.invalid-<time>), and from
// Settings → Servers → Add server.

type wizardStep int

const (
	stepURL wizardStep = iota
	stepUser
	stepPassword
	stepAPIKey
	stepTest
	wizardFields = stepTest
)

var stepText = [wizardFields]struct{ prompt, help string }{
	{"Server address", "Like http://192.168.1.10:4533 or https://music.example.com"},
	{"Username", "Leave it empty if you log in with an API key"},
	{"Password", "It is not stored: the app keeps a token instead"},
	{"API key (optional)", "Only for servers that give out API keys; leave it empty otherwise"},
}

// WizardScreen collects a server's address and login, tests them, and
// saves the server to the config as the default.
type WizardScreen struct {
	step     wizardStep
	fields   [wizardFields][]rune
	kb       Keyboard
	show     bool   // the password is shown
	problem  string // why Next didn't advance
	firstRun bool   // the app's first screen: B on the first step has nowhere to go
	backup   bool   // the config file is invalid: move it aside when saving

	// The test step.
	insecure, plaintext bool // the user allowed these after an error
	testing             bool
	err                 error
	info                *subsonic.ServerInfo
	auth                subsonic.AuthMethod
	actions             []menuEntry
	list                List
	cancel              func()
	saving              bool
}

// NewWizardScreen starts the wizard. firstRun: it is the app's first
// screen. backup: the existing config file is invalid and is kept aside.
func NewWizardScreen(firstRun, backup bool) *WizardScreen {
	s := &WizardScreen{firstRun: firstRun, backup: backup}
	s.setStep(stepURL)
	return s
}

func (s *WizardScreen) Title() string {
	if s.firstRun {
		return "Set up MiSTer Subsonic"
	}
	return "Add a server"
}

func (s *WizardScreen) Enter(a *App) {}

func (s *WizardScreen) setStep(st wizardStep) {
	s.step, s.problem = st, ""
	switch st {
	case stepURL:
		s.kb = NewTextKeyboard(urlPresets...)
	case stepPassword:
		s.kb = NewTextKeyboard(kbKey{label: "Show / hide", action: keyShow, units: 4})
	default:
		s.kb = NewTextKeyboard()
	}
}

// Text takes physical keyboard input: Enter is Next, Backspace edits (on
// an empty field it goes back a step, as B).
func (s *WizardScreen) Text(a *App, r rune) bool {
	if s.step == stepTest {
		return false
	}
	f := &s.fields[s.step]
	switch {
	case r == '\n':
		s.next(a)
	case r == '\b':
		if len(*f) == 0 {
			return false
		}
		*f = (*f)[:len(*f)-1]
	case unicode.IsPrint(r):
		*f = append(*f, r)
	default:
		return false
	}
	return true
}

func (s *WizardScreen) Handle(a *App, e input.Event) bool {
	if s.step == stepTest {
		return s.handleTest(a, e)
	}
	if e.Kind == input.Release {
		return false
	}
	if s.kb.Move(e) {
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	f := &s.fields[s.step]
	switch e.Button {
	case input.BtnA:
		k := s.kb.Focused()
		switch k.action {
		case keyChar:
			*f = append(*f, k.r)
		case keySpace:
			*f = append(*f, ' ')
		case keyDel:
			if len(*f) > 0 {
				*f = (*f)[:len(*f)-1]
			}
		case keyLayout:
			s.kb.Switch(k.to)
		case keyInsert:
			*f = append(*f, []rune(k.text)...)
		case keyShow:
			s.show = !s.show
		case keyNext:
			s.next(a)
		}
		return true
	case input.BtnX: // shortcut for Del
		if len(*f) > 0 {
			*f = (*f)[:len(*f)-1]
		}
		return true
	case input.BtnB:
		return s.back(a)
	}
	return false
}

// back goes to the previous step; false (the app pops the wizard) on the
// first step, unless the wizard is the app's first screen.
func (s *WizardScreen) back(a *App) bool {
	if s.step > stepURL {
		s.cancelTest()
		s.setStep(s.step - 1)
		return true
	}
	return s.firstRun
}

// next checks the field and moves on; after the last field it tests.
func (s *WizardScreen) next(a *App) {
	switch s.step {
	case stepURL:
		u, err := normalizeURL(string(s.fields[stepURL]))
		if err != nil {
			s.problem = err.Error()
			return
		}
		s.fields[stepURL] = []rune(u)
	case stepAPIKey:
		pw, key := string(s.fields[stepPassword]), strings.TrimSpace(string(s.fields[stepAPIKey]))
		switch {
		case pw == "" && key == "":
			s.problem = "Enter a password (step 3) or an API key"
			return
		case key == "" && strings.TrimSpace(string(s.fields[stepUser])) == "":
			s.problem = "Enter a username (step 2), or an API key"
			return
		}
	}
	s.setStep(s.step + 1)
	if s.step == stepTest {
		s.test(a)
	}
}

// normalizeURL accepts "host:port" as http and trims a trailing "/rest".
func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("Enter the server's address")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("That doesn't look like http://host:port or https://host")
	}
	u.User = nil
	u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/rest")
	return strings.TrimSuffix(u.String(), "/"), nil
}

// server is the server being set up, from the fields and the options the
// user allowed after an error.
func (s *WizardScreen) server() config.Server {
	u, _ := url.Parse(string(s.fields[stepURL]))
	return config.Server{
		Name:     u.Hostname(),
		URL:      string(s.fields[stepURL]),
		Username: strings.TrimSpace(string(s.fields[stepUser])),
		Password: string(s.fields[stepPassword]),
		APIKey:   strings.TrimSpace(string(s.fields[stepAPIKey])),

		InsecureSkipVerify:     s.insecure,
		AllowPlaintextPassword: s.plaintext,
	}
}

func (s *WizardScreen) cancelTest() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.testing = false
}

// test pings the server with the entered login (off the UI goroutine).
func (s *WizardScreen) test(a *App) {
	s.cancelTest()
	s.testing, s.err, s.info, s.actions = true, nil, nil, nil
	srv := s.server()
	type result struct {
		info *subsonic.ServerInfo
		auth subsonic.AuthMethod
	}
	s.cancel = a.LoadCancel(s, func(ctx context.Context) (any, error) {
		c, info, err := Dial(ctx, srv)
		if err != nil {
			return nil, err
		}
		return result{info, c.AuthMethod()}, nil
	}, func(v any, err error) {
		s.testing, s.cancel = false, nil
		if err != nil {
			s.err = err
		} else {
			r := v.(result)
			s.info, s.auth = r.info, r.auth
		}
		s.list = List{}
		s.actions = s.resultActions()
	})
}

// resultActions are the choices after a test: save, or ways to fix it.
func (s *WizardScreen) resultActions() []menuEntry {
	if s.err == nil {
		return []menuEntry{
			{"Save and continue", func(a *App) { s.save(a) }},
			{"Back", func(a *App) { s.setStep(stepAPIKey) }},
		}
	}
	var out []menuEntry
	switch subsonic.Classify(s.err) {
	case subsonic.KindTLS:
		if !s.insecure {
			out = append(out, menuEntry{"Connect without checking the certificate (insecure)", func(a *App) { s.insecure = true; s.test(a) }})
		}
	case subsonic.KindPlaintextRefused:
		out = append(out, menuEntry{"Allow sending the password in plain text", func(a *App) { s.plaintext = true; s.test(a) }})
	case subsonic.KindAuth:
		out = append(out, menuEntry{"Change the username or password", func(a *App) { s.setStep(stepUser) }})
	}
	return append(out,
		menuEntry{"Try again", func(a *App) { s.test(a) }},
		menuEntry{"Change the address", func(a *App) { s.setStep(stepURL) }},
	)
}

// save writes the tested server as the default and connects to it. The
// password is stored only when the server needs the plain password; with
// token auth a token and salt are stored instead (spec §4).
func (s *WizardScreen) save(a *App) {
	if s.saving {
		return
	}
	s.saving = true
	srv := s.server()
	switch s.auth {
	case subsonic.AuthToken:
		srv.Token, srv.Salt = subsonic.NewTokenPair(srv.Password)
		srv.Password, srv.APIKey = "", ""
	case subsonic.AuthAPIKey:
		srv.Password = ""
	}
	finish := func() {
		a.UpdateConfig(func(c *config.Config) { c.AddServer(srv) }, false)
		a.Connect()
	}
	if !s.backup {
		finish()
		return
	}
	path := a.o.ConfigPath
	now := a.o.Now()
	a.Load(s, func(context.Context) (any, error) { return config.BackupInvalid(path, now) }, func(v any, err error) {
		s.saving = false
		if err != nil {
			a.Toast("Couldn't move the old config aside: %s", saveProblem(err))
			return
		}
		a.Toast("The old config is kept as %s", v)
		a.cfg = nil // start over from the defaults
		finish()
	})
}

func (s *WizardScreen) handleTest(a *App, e input.Event) bool {
	if e.Kind == input.Press && e.Button == input.BtnB {
		return s.back(a)
	}
	if s.list.Handle(e, len(s.actions)) {
		return true
	}
	if e.Kind == input.Press && e.Button == input.BtnA && len(s.actions) > 0 {
		s.actions[s.list.Focus].run(a)
		return true
	}
	return false
}

func (s *WizardScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	fb, fs := a.F.Body, a.F.Small
	x, w := area.X+p.Margin, area.W-2*p.Margin
	y := area.Y + p.Margin/2
	line := func(f *gfx.Font, text string, col gfx.Color) {
		for _, l := range wrap(f, text, w) {
			f.Draw(c, x, y+f.Ascent(), l, col, area)
			y += f.Height()
		}
	}
	if s.step == stepTest {
		s.drawTest(a, c, area, line, &y)
		return
	}
	line(fb, fmt.Sprintf("%d of %d · %s", s.step+1, wizardFields, stepText[s.step].prompt), colText)
	line(fs, stepText[s.step].help, colDim)
	y += p.Margin / 4
	field := gfx.R(x, y, w, fb.Height()+p.Margin/2)
	masked := s.step == stepPassword && !s.show
	a.drawTextField(c, field, s.fields[s.step], "", masked)
	y = field.Bottom() + p.Margin/4
	if s.problem != "" {
		line(fs, s.problem, colError)
	}
	y += p.Margin / 4
	keyH := p.RowH
	kw := min(w, 12*p.RowH)
	s.kb.Draw(a, c, gfx.R(x, y, kw, s.kb.Height(keyH)), keyH, true)
}

func (s *WizardScreen) drawTest(a *App, c *gfx.Canvas, area gfx.Rect, line func(*gfx.Font, string, gfx.Color), y *int) {
	p := a.P
	fb, fs := a.F.Body, a.F.Small
	srv := s.server()
	switch {
	case s.testing:
		line(fb, "Connecting to "+displayURL(srv.URL)+"…", colDim)
		return
	case s.err == nil && s.info != nil:
		who := srv.Username
		if who == "" {
			who = "your API key"
		}
		line(fb, fmt.Sprintf("Connected to %s as %s.", serverName(s.info), who), colText)
		line(fs, "Login: "+s.auth.String()+". "+displayURL(srv.URL), colDim)
	case s.err != nil:
		line(fb, fmt.Sprintf("%s: %s.", displayURL(srv.URL), subsonic.Classify(s.err)), colError)
		if h := wizardHint(s.err); h != "" {
			line(fs, h, colDim)
		}
	}
	*y += p.Margin / 2
	s.list.Draw(c, gfx.R(area.X, *y, area.W, area.Bottom()-*y), len(s.actions), p.RowH, func(i int, r gfx.Rect, focused bool) {
		a.drawRow(c, r, row{main: s.actions[i].label, col: colAccent, focused: focused})
	})
}

// serverName is e.g. "Navidrome 0.59.0", or "the server".
func serverName(info *subsonic.ServerInfo) string {
	if info == nil || info.Type == "" {
		return "the server"
	}
	name := strings.ToUpper(info.Type[:1]) + info.Type[1:]
	if info.ServerVersion != "" {
		name += " " + info.ServerVersion
	}
	return name
}

// wizardHint explains an error in the wizard's terms.
func wizardHint(err error) string {
	switch subsonic.Classify(err) {
	case subsonic.KindTLS:
		return "The server's certificate isn't trusted (self-signed?). The safe fix is ca_file in the config; skipping the check works but anyone on the network could pose as the server."
	case subsonic.KindAuth:
		return "The server didn't accept this username and password (or API key)."
	case subsonic.KindPlaintextRefused:
		return "This server can't use tokens (LDAP or proxy login?) and wants the plain password, which over http anyone on the network can read. Use https, or allow it."
	case subsonic.KindUnreachable, subsonic.KindTimeout:
		return "Check the address and port, and that the server is running."
	case subsonic.KindNotFound:
		return "Something answered, but not a Subsonic server: check the address and port."
	}
	return ""
}
