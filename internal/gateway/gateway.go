// Package gateway implements the only public entry point of Obrabi: it serves
// the web app, handles login sessions and forwards API calls to the internal
// services with the verified user identity.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lucabodd/obrabi/internal/platform/httpx"
	"github.com/lucabodd/obrabi/internal/platform/ratelimit"
	"github.com/lucabodd/obrabi/internal/platform/telemetry"
)

// Config of the gateway.
type Config struct {
	AuthURL, ProjectsURL, StatsURL, FeedbackURL string
	InternalToken                               string
	Sessions                                    *Sessions
	// TrustedProxies are the addresses (IPs or CIDRs) of the reverse proxies
	// in front of the gateway whose X-Forwarded-For header is trusted, e.g.
	// the Nginx Proxy Manager / Caddy / Traefik instance on Proxmox.
	TrustedProxies []string
}

// Gateway wires everything together.
type Gateway struct {
	cfg      Config
	log      *slog.Logger
	reporter telemetry.Reporter
	static   *Static
	auth     *upstream
	projects *upstream
	stats    *upstream
	feedback *upstream

	loginByIP   *ratelimit.Limiter
	loginByUser *ratelimit.Limiter
	httpClient  *http.Client
}

// Headers sent by the web app with every mutating request. A cross-site page
// cannot add a custom header without a CORS preflight, which the gateway never
// grants: together with the SameSite=Lax cookie this blocks CSRF.
const headerCSRF = "X-Obrabi"

// New builds the gateway.
func New(cfg Config, static *Static, log *slog.Logger, reporter telemetry.Reporter) (*Gateway, error) {
	g := &Gateway{
		cfg:         cfg,
		log:         log,
		reporter:    reporter,
		static:      static,
		loginByIP:   ratelimit.New(10, 15*time.Minute),
		loginByUser: ratelimit.New(20, time.Hour),
		httpClient:  &http.Client{Timeout: 15 * time.Second},
	}
	var err error
	for _, u := range []struct {
		dst  **upstream
		name string
		raw  string
	}{
		{&g.auth, "auth", cfg.AuthURL},
		{&g.projects, "projects", cfg.ProjectsURL},
		{&g.stats, "stats", cfg.StatsURL},
		{&g.feedback, "feedback", cfg.FeedbackURL},
	} {
		if *u.dst, err = g.newUpstream(u.name, u.raw); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// Handler returns the HTTP handler.
func (g *Gateway) Handler() (http.Handler, error) {
	r := httpx.NewEngine("gateway", g.log, g.reporter)
	if err := r.SetTrustedProxies(g.cfg.TrustedProxies); err != nil {
		return nil, fmt.Errorf("trusted proxies: %w", err)
	}
	if len(g.cfg.TrustedProxies) == 0 {
		// Without trusted proxies ClientIP() is the TCP peer address.
		r.ForwardedByClientIP = false
	}
	r.Use(securityHeaders())

	api := r.Group("/api", limitBody(1<<20), requireCSRFHeader())
	api.POST("/auth/login", g.login)
	api.POST("/auth/logout", g.logout)

	authed := api.Group("", g.requireSession)
	authed.GET("/auth/me", g.forward(g.auth, fixed("/users/me")))
	authed.PUT("/auth/me", g.forward(g.auth, fixed("/users/me")))
	authed.PUT("/auth/password", g.forward(g.auth, fixed("/users/me/password")))
	for _, prefix := range []string{"/towns", "/categories", "/projects"} {
		authed.Any(prefix, g.forward(g.projects, stripAPI))
		authed.Any(prefix+"/*rest", g.forward(g.projects, stripAPI))
	}
	authed.Any("/stats/*rest", g.forward(g.stats, stripAPI))
	authed.Any("/feedback", g.forward(g.feedback, stripAPI))
	authed.POST("/telemetry", g.forward(g.feedback, stripAPI))

	r.NoRoute(g.serveApp)
	return r, nil
}

// ---------------------------------------------------------------- middleware

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		c.Next()
	}
}

func limitBody(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, n)
		}
		c.Next()
	}
}

func requireCSRFHeader() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if c.GetHeader(headerCSRF) == "" {
				httpx.Fail(c, http.StatusForbidden, "csrf", "Petició no permesa.")
				return
			}
		}
		c.Next()
	}
}

const sessionKey = "obrabi.session"

func (g *Gateway) requireSession(c *gin.Context) {
	cookie, err := c.Cookie(CookieName)
	if err != nil || cookie == "" {
		httpx.Fail(c, http.StatusUnauthorized, "unauthorized", "Cal iniciar la sessió.")
		return
	}
	sess, err := g.cfg.Sessions.Parse(cookie)
	if err != nil {
		g.cfg.Sessions.ClearCookie(c.Writer, c.Request)
		httpx.Fail(c, http.StatusUnauthorized, "unauthorized", "La sessió ha caducat. Torna a entrar.")
		return
	}
	if g.cfg.Sessions.NeedsRefresh(sess) {
		if token, exp, err := g.cfg.Sessions.Issue(sess.UserID, sess.Username); err == nil {
			g.cfg.Sessions.SetCookie(c.Writer, c.Request, token, exp)
		}
	}
	c.Set(sessionKey, sess)
	c.Next()
}

// ---------------------------------------------------------------- login

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginUser struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

func (g *Gateway) login(c *gin.Context) {
	ip := c.ClientIP()
	var req loginRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	userKey := strings.ToLower(strings.TrimSpace(req.Username))
	if g.loginByIP.Blocked(ip) || g.loginByUser.Blocked(userKey) {
		g.log.Warn("login rate limited", "ip", ip, "username", userKey)
		httpx.Fail(c, http.StatusTooManyRequests, "rate_limited",
			"Massa intents fallits. Espera uns minuts i torna-ho a provar.")
		return
	}

	user, status, err := g.checkCredentials(c, req, httpx.RequestIDFrom(c))
	if err != nil {
		g.reportUpstream(c, g.auth, err)
		httpx.Fail(c, http.StatusBadGateway, "unavailable", "Ara mateix no es pot iniciar la sessió. Torna-ho a provar d'ací a un moment.")
		return
	}
	switch status {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusBadRequest:
		g.loginByIP.Hit(ip)
		g.loginByUser.Hit(userKey)
		httpx.Fail(c, http.StatusUnauthorized, "invalid_credentials", "L'usuari o la contrasenya no són correctes.")
		return
	default:
		g.reportUpstream(c, g.auth, fmt.Errorf("login answered %d", status))
		httpx.Fail(c, http.StatusBadGateway, "unavailable", "Ara mateix no es pot iniciar la sessió. Torna-ho a provar d'ací a un moment.")
		return
	}

	g.loginByIP.Reset(ip)
	g.loginByUser.Reset(userKey)
	token, exp, err := g.cfg.Sessions.Issue(user.ID, user.Username)
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	g.cfg.Sessions.SetCookie(c.Writer, c.Request, token, exp)
	g.log.Info("login", "user_id", user.ID, "ip", ip)
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// checkCredentials asks the auth service to verify username and password.
func (g *Gateway) checkCredentials(ctx context.Context, req loginRequest, requestID string) (loginUser, int, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.auth.target.String()+"/internal/login", bytes.NewReader(body))
	if err != nil {
		return loginUser{}, 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(httpx.HeaderInternalToken, g.cfg.InternalToken)
	httpReq.Header.Set(httpx.HeaderRequestID, requestID)
	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return loginUser{}, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return loginUser{}, resp.StatusCode, nil
	}
	var out struct {
		User loginUser `json:"user"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return loginUser{}, 0, fmt.Errorf("decode login response: %w", err)
	}
	if out.User.ID <= 0 {
		return loginUser{}, 0, errors.New("login response without user")
	}
	return out.User, http.StatusOK, nil
}

func (g *Gateway) logout(c *gin.Context) {
	g.cfg.Sessions.ClearCookie(c.Writer, c.Request)
	c.Status(http.StatusNoContent)
}

// ---------------------------------------------------------------- proxy

type upstream struct {
	name   string
	target *url.URL
	proxy  *httputil.ReverseProxy
}

var upstreamTransport = &http.Transport{
	Proxy:                 nil, // internal traffic never goes through an HTTP proxy
	DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	MaxIdleConns:          64,
	MaxIdleConnsPerHost:   16,
	IdleConnTimeout:       90 * time.Second,
	ResponseHeaderTimeout: 45 * time.Second,
}

type ctxKey int

const (
	ctxUpstreamPath ctxKey = iota
	ctxGin
)

func (g *Gateway) newUpstream(name, raw string) (*upstream, error) {
	target, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("invalid %s service URL %q", name, raw)
	}
	u := &upstream{name: name, target: target}
	u.proxy = &httputil.ReverseProxy{
		Transport: upstreamTransport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			p, _ := pr.In.Context().Value(ctxUpstreamPath).(string)
			pr.Out.URL.Scheme = target.Scheme
			pr.Out.URL.Host = target.Host
			pr.Out.URL.Path = target.Path + p
			pr.Out.URL.RawPath = ""
			pr.Out.Host = target.Host
		},
		ModifyResponse: func(resp *http.Response) error {
			// The gateway already set its own request id header.
			resp.Header.Del(httpx.HeaderRequestID)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if c, ok := r.Context().Value(ctxGin).(*gin.Context); ok {
				if errors.Is(err, context.Canceled) {
					return // the browser went away
				}
				g.reportUpstream(c, u, err)
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(httpx.Error{Code: "unavailable",
				Message: "Ara mateix no es pot connectar amb el servidor. Torna-ho a provar d'ací a un moment."})
		},
	}
	return u, nil
}

// pathMapper computes the upstream path from the public one.
type pathMapper func(c *gin.Context) string

func stripAPI(c *gin.Context) string { return strings.TrimPrefix(c.Request.URL.Path, "/api") }

func fixed(p string) pathMapper { return func(*gin.Context) string { return p } }

// forward proxies the request to u with the session identity attached.
func (g *Gateway) forward(u *upstream, mapPath pathMapper) gin.HandlerFunc {
	return func(c *gin.Context) {
		sess := c.MustGet(sessionKey).(Session)
		h := c.Request.Header
		// Never let the client choose its identity or reach internal routes.
		for _, k := range []string{httpx.HeaderUserID, httpx.HeaderUsername, httpx.HeaderInternalToken, httpx.HeaderRequestID, "Cookie", "Authorization", headerCSRF} {
			h.Del(k)
		}
		h.Set(httpx.HeaderUserID, strconv.FormatInt(sess.UserID, 10))
		h.Set(httpx.HeaderUsername, sess.Username)
		h.Set(httpx.HeaderInternalToken, g.cfg.InternalToken)
		h.Set(httpx.HeaderRequestID, httpx.RequestIDFrom(c))

		p := path.Clean(mapPath(c))
		if strings.HasPrefix(p, "/internal") {
			httpx.NotFound(c, "")
			return
		}
		ctx := context.WithValue(c.Request.Context(), ctxUpstreamPath, p)
		ctx = context.WithValue(ctx, ctxGin, c)
		u.proxy.ServeHTTP(c.Writer, c.Request.WithContext(ctx))
	}
}

func (g *Gateway) reportUpstream(c *gin.Context, u *upstream, err error) {
	g.log.Error("upstream failure", "service", u.name, "err", err, "request_id", httpx.RequestIDFrom(c))
	ev := telemetry.Event{
		Source:  "gateway",
		Message: fmt.Sprintf("servizio %s non raggiungibile: %v", u.name, err),
		Details: map[string]any{
			"method":     c.Request.Method,
			"path":       c.FullPath(),
			"url":        c.Request.URL.Path,
			"service":    u.name,
			"request_id": httpx.RequestIDFrom(c),
		},
	}
	if v, ok := c.Get(sessionKey); ok {
		sess := v.(Session)
		ev.UserID, ev.Username = sess.UserID, sess.Username
	}
	g.reporter.Report(ev)
}

// ---------------------------------------------------------------- web app

func (g *Gateway) serveApp(c *gin.Context) {
	p := c.Request.URL.Path
	if p == "/api" || strings.HasPrefix(p, "/api/") {
		httpx.NotFound(c, "")
		return
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		httpx.Fail(c, http.StatusMethodNotAllowed, "method_not_allowed", "Operació no permesa.")
		return
	}
	if a, ok := g.static.Lookup(p); ok {
		g.static.Serve(c.Writer, c.Request, a)
		return
	}
	if path.Ext(p) != "" {
		c.String(http.StatusNotFound, "404 not found")
		return
	}
	// Client-side route such as /obres/12: serve the app shell.
	g.static.ServeIndex(c.Writer, c.Request)
}
