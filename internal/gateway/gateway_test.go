package gateway

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lucabodd/obrabi/internal/platform/telemetry"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestSessionRoundTrip(t *testing.T) {
	s := NewSessions(testSecret, time.Hour, SecureAlways)
	token, _, err := s.Issue(7, "nando")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := s.Parse(token)
	if err != nil || sess.UserID != 7 || sess.Username != "nando" {
		t.Fatalf("Parse = %+v, %v", sess, err)
	}

	if _, err := s.Parse(token[:len(token)-2] + "xx"); err == nil {
		t.Error("tampered token accepted")
	}
	if _, err := NewSessions(strings.Repeat("z", 32), time.Hour, SecureAlways).Parse(token); err == nil {
		t.Error("token signed with another secret accepted")
	}
	s.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := s.Parse(token); err == nil {
		t.Error("expired token accepted")
	}
	// alg=none must never be accepted.
	none := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJ1c3IiOiJuYW5kbyIsImlzcyI6Im9icmFiaSIsInN1YiI6IjEiLCJleHAiOjQxMDI0NDQ4MDAsImlhdCI6MTcwMDAwMDAwMH0."
	if _, err := s.Parse(none); err == nil {
		t.Error("alg=none token accepted")
	}
}

// fakeServices answers like the internal services: /internal/login checks
// nando/secret, everything else echoes the request.
func fakeServices(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/internal/login" {
			var req loginRequest
			json.NewDecoder(r.Body).Decode(&req)
			if r.Header.Get("X-Internal-Token") != "internal-token-123" || req.Username != "nando" || req.Password != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				io.WriteString(w, `{"error":"invalid_credentials"}`)
				return
			}
			io.WriteString(w, `{"user":{"id":1,"username":"nando","display_name":"Nando"}}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"path":     r.URL.Path,
			"user":     r.Header.Get("X-User-ID"),
			"username": r.Header.Get("X-Username"),
			"internal": r.Header.Get("X-Internal-Token"),
			"cookie":   r.Header.Get("Cookie"),
		})
	}))
}

func newTestGateway(t *testing.T, upstreamURL string) http.Handler {
	t.Helper()
	static, err := NewStatic(fstest.MapFS{
		"index.html": {Data: []byte(`<!doctype html><meta name="obrabi-version" content="{{VERSION}}">`)},
		"js/main.js": {Data: []byte(strings.Repeat("console.log('obrabi');\n", 100))},
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err := New(Config{
		AuthURL: upstreamURL, ProjectsURL: upstreamURL, StatsURL: upstreamURL, FeedbackURL: upstreamURL,
		InternalToken: "internal-token-123",
		Sessions:      NewSessions(testSecret, time.Hour, SecureAuto),
	}, static, slog.New(slog.NewTextHandler(testLogWriter, nil)), telemetry.Discard{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := g.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func do(h http.Handler, method, target, body string, headers map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = "192.0.2.10:1234"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := recorder{httptest.NewRecorder()}
	h.ServeHTTP(rec, req)
	return rec.ResponseRecorder
}

var csrf = map[string]string{"X-Obrabi": "1"}

func login(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	rec := do(h, http.MethodPost, "/api/auth/login", `{"username":"nando","password":"secret"}`, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status %d: %s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == CookieName && c.Value != "" {
			if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
				t.Errorf("cookie flags: %+v", c)
			}
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestLoginAndIdentityForwarding(t *testing.T) {
	up := fakeServices(t)
	defer up.Close()
	h := newTestGateway(t, up.URL)

	if rec := do(h, http.MethodGet, "/api/projects", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous API call: status %d", rec.Code)
	}

	cookie := login(t, h)
	rec := do(h, http.MethodGet, "/api/projects/5", "", map[string]string{
		"X-User-ID":        "999", // spoofing attempt
		"X-Internal-Token": "guess",
	}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var echo map[string]string
	json.Unmarshal(rec.Body.Bytes(), &echo)
	if echo["user"] != "1" || echo["username"] != "nando" {
		t.Errorf("identity not taken from the session: %v", echo)
	}
	if echo["internal"] != "internal-token-123" {
		t.Errorf("internal token not set: %v", echo)
	}
	if echo["path"] != "/projects/5" {
		t.Errorf("upstream path = %q", echo["path"])
	}
	if echo["cookie"] != "" {
		t.Errorf("session cookie leaked upstream: %q", echo["cookie"])
	}

	rec = do(h, http.MethodGet, "/api/auth/me", "", nil, cookie)
	json.Unmarshal(rec.Body.Bytes(), &echo)
	if echo["path"] != "/users/me" {
		t.Errorf("/api/auth/me mapped to %q", echo["path"])
	}

	// Path tricks must not reach the internal routes of a service.
	rec = do(h, http.MethodGet, "/api/projects/../internal/login", "", nil, cookie)
	if rec.Code != http.StatusNotFound {
		t.Errorf("internal route reachable: status %d body %s", rec.Code, rec.Body)
	}
}

func TestCookieSecureAuto(t *testing.T) {
	up := fakeServices(t)
	defer up.Close()
	h := newTestGateway(t, up.URL)
	body := `{"username":"nando","password":"secret"}`

	plain := do(h, http.MethodPost, "/api/auth/login", body, csrf)
	proxied := do(h, http.MethodPost, "/api/auth/login", body, map[string]string{"X-Obrabi": "1", "X-Forwarded-Proto": "https"})
	secure := func(rec *httptest.ResponseRecorder) bool {
		for _, c := range rec.Result().Cookies() {
			if c.Name == CookieName {
				return c.Secure
			}
		}
		t.Fatal("no session cookie")
		return false
	}
	if secure(plain) {
		t.Error("plain HTTP login got a Secure cookie (the browser would drop it)")
	}
	if !secure(proxied) {
		t.Error("login behind an HTTPS proxy got a non-Secure cookie")
	}
}

func TestCSRFHeaderRequired(t *testing.T) {
	up := fakeServices(t)
	defer up.Close()
	h := newTestGateway(t, up.URL)
	cookie := login(t, h)

	if rec := do(h, http.MethodPost, "/api/projects", `{"name":"x"}`, nil, cookie); rec.Code != http.StatusForbidden {
		t.Errorf("POST without X-Obrabi: status %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/api/projects", `{"name":"x"}`, csrf, cookie); rec.Code != http.StatusOK {
		t.Errorf("POST with X-Obrabi: status %d", rec.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	up := fakeServices(t)
	defer up.Close()
	h := newTestGateway(t, up.URL)

	for i := 0; i < 10; i++ {
		rec := do(h, http.MethodPost, "/api/auth/login", `{"username":"nando","password":"wrong"}`, csrf)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d", i+1, rec.Code)
		}
	}
	rec := do(h, http.MethodPost, "/api/auth/login", `{"username":"nando","password":"secret"}`, csrf)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after 10 failures: status %d", rec.Code)
	}
}

func TestStaticApp(t *testing.T) {
	up := fakeServices(t)
	defer up.Close()
	h := newTestGateway(t, up.URL)

	rec := do(h, http.MethodGet, "/obres/12", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "obrabi-version") {
		t.Fatalf("client route: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "{{VERSION}}") {
		t.Error("version placeholder not replaced")
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("missing CSP on the app shell")
	}

	rec = do(h, http.MethodGet, "/js/main.js", "", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("asset: %d encoding %q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	etag := rec.Header().Get("ETag")
	if rec := do(h, http.MethodGet, "/js/main.js", "", map[string]string{"If-None-Match": etag}); rec.Code != http.StatusNotModified {
		t.Errorf("conditional request: %d", rec.Code)
	}

	if rec := do(h, http.MethodGet, "/js/missing.js", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("missing asset: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/api/nope", "", nil); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("unknown API route: %d %s", rec.Code, rec.Body)
	}
}

// Set to os.Stderr to see the gateway logs while debugging a test.
var testLogWriter io.Writer = io.Discard

// recorder adds CloseNotify, which gin's writer forwards and ReverseProxy
// uses; the real net/http ResponseWriter implements it.
type recorder struct {
	*httptest.ResponseRecorder
}

func (recorder) CloseNotify() <-chan bool { return make(chan bool) }
