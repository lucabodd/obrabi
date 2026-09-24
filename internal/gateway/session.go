package gateway

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// CookieName is the session cookie set after login.
const CookieName = "obrabi_session"

// Session is the identity carried by the cookie.
type Session struct {
	UserID    int64
	Username  string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type claims struct {
	Username string `json:"usr"`
	jwt.RegisteredClaims
}

// CookieSecurity decides the Secure flag of the session cookie.
type CookieSecurity int

const (
	// SecureAuto marks the cookie Secure when the request came over HTTPS,
	// directly or through a reverse proxy (X-Forwarded-Proto). A first test
	// over plain HTTP on the LAN then still works.
	SecureAuto CookieSecurity = iota
	SecureAlways
	SecureNever
)

// ParseCookieSecurity reads "auto", "true" or "false".
func ParseCookieSecurity(s string) (CookieSecurity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return SecureAuto, nil
	case "1", "true", "yes", "on":
		return SecureAlways, nil
	case "0", "false", "no", "off":
		return SecureNever, nil
	}
	return SecureAuto, errors.New(`must be "auto", "true" or "false"`)
}

// Sessions mints and verifies session tokens (JWT, HS256). Only the gateway
// holds the secret: the internal services receive the verified identity in
// headers instead.
type Sessions struct {
	secret []byte
	ttl    time.Duration
	secure CookieSecurity
	now    func() time.Time
}

func NewSessions(secret string, ttl time.Duration, secure CookieSecurity) *Sessions {
	return &Sessions{secret: []byte(secret), ttl: ttl, secure: secure, now: time.Now}
}

// secureFor tells whether the cookie set in response to r must be Secure.
// Trusting X-Forwarded-Proto here is safe: a forged header can only make the
// browser refuse the cookie, never weaken it for a real HTTPS request.
func (s *Sessions) secureFor(r *http.Request) bool {
	switch s.secure {
	case SecureAlways:
		return true
	case SecureNever:
		return false
	}
	return r.TLS != nil ||
		strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") ||
		strings.EqualFold(r.Header.Get("X-Forwarded-Ssl"), "on")
}

// Issue returns a signed token for the user.
func (s *Sessions) Issue(uid int64, username string) (string, time.Time, error) {
	now := s.now()
	exp := now.Add(s.ttl)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "obrabi",
			Subject:   strconv.FormatInt(uid, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	})
	signed, err := token.SignedString(s.secret)
	return signed, exp, err
}

// Parse verifies a token and returns its session.
func (s *Sessions) Parse(token string) (Session, error) {
	var c claims
	_, err := jwt.ParseWithClaims(token, &c,
		func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer("obrabi"),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil {
		return Session{}, err
	}
	uid, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil || uid <= 0 || c.IssuedAt == nil {
		return Session{}, errors.New("invalid session subject")
	}
	return Session{UserID: uid, Username: c.Username, IssuedAt: c.IssuedAt.Time, ExpiresAt: c.ExpiresAt.Time}, nil
}

// NeedsRefresh reports whether a still valid session should be re-issued
// (sliding expiration: Nando stays logged in as long as he uses the app).
func (s *Sessions) NeedsRefresh(sess Session) bool {
	return s.now().Sub(sess.IssuedAt) > 24*time.Hour
}

// SetCookie stores the token in an HttpOnly cookie.
func (s *Sessions) SetCookie(w http.ResponseWriter, r *http.Request, token string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  exp,
		MaxAge:   int(time.Until(exp).Seconds()),
		HttpOnly: true,
		Secure:   s.secureFor(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie removes the session cookie.
func (s *Sessions) ClearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureFor(r),
		SameSite: http.SameSiteLaxMode,
	})
}
