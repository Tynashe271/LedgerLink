// Package auth implements the Go side of the delegated-identity handshake
// described in BranchLedger_Architecture.pdf: "Laravel routes /api requests
// through an authenticated gateway to Go, with verified identity context and
// request IDs. Go independently validates the delegated identity and scopes.
// Reject client-provided identity headers."
//
// Laravel owns login, sessions, CSRF and account recovery. Once a browser
// request is authenticated at the session layer, Laravel signs a short-lived
// delegation token (HMAC, shared secret, never exposed to the browser) that
// states who the gateway verified the caller to be. Go verifies that
// signature independently on every request; it never trusts an unsigned
// identity header, and the signature's secret is never sent to the browser.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrMissingToken     = errors.New("auth: missing delegation token")
	ErrInvalidSignature = errors.New("auth: delegation token signature invalid")
	ErrExpiredToken      = errors.New("auth: delegation token expired")
	ErrMalformedToken    = errors.New("auth: delegation token malformed")
)

// Claims is the payload Laravel signs and Go verifies. It carries exactly the
// identity context the architecture requires: who, which company, which
// branches, what role, the access version (for permission-change propagation),
// and a request ID for audit correlation.
type Claims struct {
	UserID        uuid.UUID   `json:"user_id"`
	CompanyID     uuid.UUID   `json:"company_id"`
	Role          string      `json:"role"`
	BranchScope   []uuid.UUID `json:"branch_scope"`
	AccessVersion int         `json:"access_version"`
	SessionID     string      `json:"session_id"`
	RequestID     string      `json:"request_id"`
	IssuedAt      time.Time   `json:"issued_at"`
	ExpiresAt     time.Time   `json:"expires_at"`
	// Delegations lists the explicit per-action grants this membership
	// carries on top of its role (tenancy.Action values, as strings so this
	// package does not need to import tenancy). Laravel resolves these from
	// the membership record at sign time, same as role and branch_scope.
	Delegations []string `json:"delegations"`
}

// Verifier checks the HMAC-SHA256 signature on a delegation token and enforces
// its short expiry window. The secret is provisioned out-of-band (deployment
// secret store), shared only between the Laravel gateway process and Go API
// process, and is never logged or sent to the browser.
type Verifier struct {
	secret []byte
	maxAge time.Duration
}

// NewVerifier builds a Verifier. maxAge bounds how long a signed token may be
// trusted after issuance, independent of its embedded ExpiresAt, as a defence
// against clock skew abuse.
func NewVerifier(secret []byte, maxAge time.Duration) *Verifier {
	return &Verifier{secret: secret, maxAge: maxAge}
}

// token wire format: base64url(json claims) + "." + base64url(hmac-sha256 of that json)
func (v *Verifier) Verify(token string) (Claims, error) {
	if token == "" {
		return Claims{}, ErrMissingToken
	}

	payloadB64, sigB64, found := strings.Cut(token, ".")
	if !found || payloadB64 == "" || sigB64 == "" {
		return Claims{}, ErrMalformedToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return Claims{}, ErrMalformedToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return Claims{}, ErrMalformedToken
	}

	mac := hmac.New(sha256.New, v.secret)
	mac.Write(payload)
	expectedSig := mac.Sum(nil)
	if subtle.ConstantTimeCompare(sig, expectedSig) != 1 {
		return Claims{}, ErrInvalidSignature
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrMalformedToken
	}

	now := time.Now()
	if now.After(claims.ExpiresAt) {
		return Claims{}, ErrExpiredToken
	}
	if now.Sub(claims.IssuedAt) > v.maxAge {
		return Claims{}, ErrExpiredToken
	}

	return claims, nil
}

// Sign produces a delegation token for the given claims. This side normally
// runs in a small Go helper invoked by, or mirrored in, the Laravel gateway
// deployment so both processes agree on the exact wire format; it is exported
// here so Go-side tests and the local dev gateway stub can issue tokens
// without duplicating the HMAC logic.
func (v *Verifier) Sign(claims Claims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: marshal claims: %w", err)
	}
	mac := hmac.New(sha256.New, v.secret)
	mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
