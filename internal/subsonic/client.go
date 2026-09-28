// Package subsonic is a client for the Subsonic REST API (1.16.1) with
// OpenSubsonic extensions, as served by Navidrome, gonic, Airsonic and others.
package subsonic

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	APIVersion        = "1.16.1"
	DefaultClientName = "MiSTerSubsonic"
)

//go:embed certs/cacert.pem
var embeddedRoots []byte

type AuthMethod int

const (
	AuthNone AuthMethod = iota
	AuthAPIKey
	AuthToken
	AuthPlain
)

func (m AuthMethod) String() string {
	return [...]string{"none", "api key", "token", "plain password"}[m]
}

// Credentials: set any of APIKey, Token+Salt, or Password. Connect tries
// them in that order.
type Credentials struct {
	Username       string
	Password       string
	Token, Salt    string // precomputed md5(password+salt) pair
	APIKey         string
	AllowPlaintext bool // allow p=enc: over plain http
}

type Options struct {
	BaseURL string // e.g. "https://music.example.com" or "http://192.168.1.10:4533"
	Credentials
	CAFile             string // extra PEM roots (self-signed servers)
	InsecureSkipVerify bool
	ClientName         string        // default DefaultClientName
	Timeout            time.Duration // per metadata request; default 15 s
	HTTPClient         *http.Client  // overrides the built-in client (tests)
}

type Client struct {
	base    string
	creds   Credentials
	name    string
	timeout time.Duration
	hc      *http.Client

	mu     sync.Mutex
	method AuthMethod
	info   *ServerInfo
}

// NewTokenPair returns a random salt and md5(password+salt), for storing
// instead of the password.
func NewTokenPair(password string) (token, salt string) {
	b := make([]byte, 8)
	rand.Read(b)
	salt = hex.EncodeToString(b)
	sum := md5.Sum([]byte(password + salt))
	return hex.EncodeToString(sum[:]), salt
}

func New(o Options) (*Client, error) {
	u, err := url.Parse(o.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("subsonic: invalid server URL %q", o.BaseURL)
	}
	c := &Client{
		base:    strings.TrimRight(o.BaseURL, "/"),
		creds:   o.Credentials,
		name:    o.ClientName,
		timeout: o.Timeout,
		hc:      o.HTTPClient,
	}
	if c.name == "" {
		c.name = DefaultClientName
	}
	if c.timeout <= 0 {
		c.timeout = 15 * time.Second
	}
	if c.creds.Token == "" && c.creds.Password != "" {
		c.creds.Token, c.creds.Salt = NewTokenPair(c.creds.Password)
	}
	if c.hc == nil {
		tc, err := tlsConfig(o.CAFile, o.InsecureSkipVerify)
		if err != nil {
			return nil, err
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = tc
		tr.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
		tr.ResponseHeaderTimeout = 15 * time.Second
		c.hc = &http.Client{Transport: tr}
	}
	c.method = c.candidates()[0]
	return c, nil
}

func tlsConfig(caFile string, insecure bool) (*tls.Config, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(embeddedRoots)
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("subsonic: ca_file: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("subsonic: ca_file %s contains no PEM certificates", caFile)
		}
	}
	return &tls.Config{RootCAs: pool, InsecureSkipVerify: insecure}, nil
}

// HTTPClient is the client used for API calls, for sharing TLS settings
// with the stream reader. It has no overall timeout.
func (c *Client) HTTPClient() *http.Client { return c.hc }

// AuthMethod is the method in use (settled by Connect).
func (c *Client) AuthMethod() AuthMethod { c.mu.Lock(); defer c.mu.Unlock(); return c.method }

// Info is what the last successful Connect learned, or nil.
func (c *Client) Info() *ServerInfo { c.mu.Lock(); defer c.mu.Unlock(); return c.info }

func (c *Client) plaintextAllowed() bool {
	return strings.HasPrefix(c.base, "https://") || c.creds.AllowPlaintext
}

func (c *Client) candidates() []AuthMethod {
	var m []AuthMethod
	if c.creds.APIKey != "" {
		m = append(m, AuthAPIKey)
	}
	if c.creds.Token != "" && c.creds.Salt != "" {
		m = append(m, AuthToken)
	}
	if c.creds.Password != "" && c.plaintextAllowed() {
		m = append(m, AuthPlain)
	}
	if len(m) == 0 {
		m = append(m, AuthNone)
	}
	return m
}

// Connect pings the server, settling on the first auth method that works,
// and records the server's OpenSubsonic extensions.
func (c *Client) Connect(ctx context.Context) (*ServerInfo, error) {
	var lastErr error = ErrNoCredentials
	for _, m := range c.candidates() {
		if m == AuthNone {
			break
		}
		c.mu.Lock()
		c.method = m
		c.mu.Unlock()
		r, err := c.call(ctx, "ping", nil)
		if err == nil {
			info := &ServerInfo{APIVersion: r.Version, Type: r.Type, ServerVersion: r.ServerVersion, OpenSubsonic: r.OpenSubsonic, Extensions: map[string][]int{}}
			if r.OpenSubsonic {
				if er, err := c.call(ctx, "getOpenSubsonicExtensions", nil); err == nil {
					for _, e := range er.Extensions {
						info.Extensions[e.Name] = e.Versions
					}
				}
			}
			c.mu.Lock()
			c.info = info
			c.mu.Unlock()
			return info, nil
		}
		lastErr = err
		var ae *APIError
		if !errors.As(err, &ae) || (ae.Code != CodeTokenAuthUnsupported && ae.Code != CodeAuthMechUnsupported) {
			return nil, err
		}
	}
	var ae *APIError
	if errors.As(lastErr, &ae) && ae.Code == CodeTokenAuthUnsupported && c.creds.Password != "" && !c.plaintextAllowed() {
		return nil, fmt.Errorf("%w (%v)", ErrPlaintextRefused, lastErr)
	}
	return nil, lastErr
}

// params returns the auth and protocol query parameters.
func (c *Client) params() url.Values {
	c.mu.Lock()
	m := c.method
	c.mu.Unlock()
	v := url.Values{}
	v.Set("v", APIVersion)
	v.Set("c", c.name)
	v.Set("f", "json")
	switch m {
	case AuthAPIKey:
		v.Set("apiKey", c.creds.APIKey)
		return v
	case AuthToken:
		v.Set("t", c.creds.Token)
		v.Set("s", c.creds.Salt)
	case AuthPlain:
		v.Set("p", "enc:"+hex.EncodeToString([]byte(c.creds.Password)))
	}
	v.Set("u", c.creds.Username)
	return v
}

func (c *Client) endpointURL(endpoint string, extra url.Values) string {
	v := c.params()
	for k, vs := range extra {
		v[k] = vs
	}
	return c.base + "/rest/" + endpoint + ".view?" + v.Encode()
}

func (c *Client) call(ctx context.Context, endpoint string, extra url.Values) (*response, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpointURL(endpoint, extra), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("subsonic: %s: %w", endpoint, stripURL(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("subsonic: %s: %w", endpoint, err)
	}
	r, derr := decodeResponse(body)
	if derr != nil {
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("subsonic: %s: HTTP %s", endpoint, resp.Status)
		}
		return nil, fmt.Errorf("subsonic: %s: %w", endpoint, derr)
	}
	if r.Status != "ok" {
		if r.Error == nil {
			return nil, fmt.Errorf("subsonic: %s: status %q", endpoint, r.Status)
		}
		return nil, r.Error
	}
	return r, nil
}

func decodeResponse(body []byte) (*response, error) {
	var env struct {
		R *response `json:"subsonic-response"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("bad response: %w", err)
	}
	if env.R == nil {
		return nil, errors.New("bad response: no subsonic-response")
	}
	return env.R, nil
}

// CheckStreamResponse rejects stream responses that are really error
// documents (servers answer failed stream requests with 200 + JSON/XML).
// It fits stream.Options.CheckResponse.
func CheckStreamResponse(resp *http.Response) error {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(ct, "json") && !strings.Contains(ct, "xml") {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if r, err := decodeResponse(body); err == nil && r.Error != nil {
		return r.Error
	}
	return fmt.Errorf("subsonic: stream returned %s instead of audio", ct)
}

// stripURL drops the request URL (which carries credentials) from
// net/http's *url.Error, keeping the underlying cause for errors.As/Is.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

// RedactURL hides credentials in a URL built by this package, for logs.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable url>"
	}
	q := u.Query()
	for _, k := range []string{"t", "s", "p", "apiKey"} {
		if q.Has(k) {
			q.Set(k, "REDACTED")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
