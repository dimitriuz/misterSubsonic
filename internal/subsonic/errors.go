package subsonic

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
)

// Subsonic error codes.
const (
	CodeGeneric              = 0
	CodeMissingParameter     = 10
	CodeClientTooOld         = 20
	CodeServerTooOld         = 30
	CodeWrongCredentials     = 40
	CodeTokenAuthUnsupported = 41
	CodeAuthMechUnsupported  = 42
	CodeConflictingAuth      = 43
	CodeInvalidAPIKey        = 44
	CodeNotAuthorized        = 50
	CodeTrialExpired         = 60
	CodeNotFound             = 70
)

// APIError is a "failed" subsonic-response.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string { return fmt.Sprintf("subsonic: error %d: %s", e.Code, e.Message) }

// ErrPlaintextRefused: the server only accepts the plain password, and the
// connection isn't HTTPS, and allow_plaintext_password is off.
var ErrPlaintextRefused = errors.New("subsonic: server needs the plain password; use https or set allow_plaintext_password")

// ErrNoCredentials: none of password, token+salt or API key was configured.
var ErrNoCredentials = errors.New("subsonic: no credentials configured")

type ErrorKind int

const (
	KindOther ErrorKind = iota
	KindUnreachable
	KindTLS
	KindAuth
	KindPlaintextRefused
	KindNotFound
	KindTimeout
)

func (k ErrorKind) String() string {
	return [...]string{"error", "server unreachable", "TLS/certificate error", "wrong credentials", "plain password refused", "not found", "timed out"}[k]
}

// Classify maps an error from this package to something a UI can explain.
func Classify(err error) ErrorKind {
	if err == nil {
		return KindOther
	}
	var ae *APIError
	if errors.As(err, &ae) {
		switch ae.Code {
		case CodeWrongCredentials, CodeInvalidAPIKey, CodeNotAuthorized, CodeTokenAuthUnsupported, CodeAuthMechUnsupported:
			return KindAuth
		case CodeNotFound:
			return KindNotFound
		}
		return KindOther
	}
	if errors.Is(err, ErrPlaintextRefused) {
		return KindPlaintextRefused
	}
	var (
		unknownCA *x509.UnknownAuthorityError
		hostErr   x509.HostnameError
		invalid   x509.CertificateInvalidError
		verifyErr *tls.CertificateVerificationError
		recordErr tls.RecordHeaderError
	)
	if errors.As(err, &unknownCA) || errors.As(err, &hostErr) || errors.As(err, &invalid) ||
		errors.As(err, &verifyErr) || errors.As(err, &recordErr) {
		return KindTLS
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return KindTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return KindTimeout
	}
	var opErr *net.OpError
	var dnsErr *net.DNSError
	if errors.As(err, &opErr) || errors.As(err, &dnsErr) {
		return KindUnreachable
	}
	return KindOther
}
