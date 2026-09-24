// Package httpx holds the Gin plumbing shared by every Obrabi service:
// request ids, access logs, panic recovery, error reporting, service-to-service
// authentication and graceful shutdown.
package httpx

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lucabodd/obrabi/internal/platform/telemetry"
)

// Header names used between the gateway and the internal services.
const (
	HeaderRequestID     = "X-Request-ID"
	HeaderInternalToken = "X-Internal-Token"
	HeaderUserID        = "X-User-ID"
	HeaderUsername      = "X-Username"
)

const (
	ctxRequestID = "obrabi.request_id"
	ctxUserID    = "obrabi.user_id"
	ctxUsername  = "obrabi.username"
)

// Error is the JSON body of every failed response. Message is meant to be
// shown to the user as-is (in Valencian).
type Error struct {
	Code    string `json:"error"`
	Message string `json:"message"`
}

// NewEngine returns a Gin engine with the standard middleware stack.
func NewEngine(service string, log *slog.Logger, reporter telemetry.Reporter) *gin.Engine {
	if os.Getenv("OBRABI_ENV") != "development" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.ContextWithFallback = true
	// The internal services are only reached through the gateway; the gateway
	// itself configures trusted proxies explicitly.
	_ = r.SetTrustedProxies(nil)
	r.Use(RequestID(), AccessLog(log), Recover(service, log, reporter), ReportErrors(service, log, reporter))
	r.NoRoute(func(c *gin.Context) {
		Fail(c, http.StatusNotFound, "not_found", "No s'ha trobat el recurs.")
	})
	r.NoMethod(func(c *gin.Context) {
		Fail(c, http.StatusMethodNotAllowed, "method_not_allowed", "Operació no permesa.")
	})
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "service": service}) })
	return r
}

// NewID returns a random hex identifier.
func NewID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// RequestID propagates X-Request-ID, generating one when absent.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if id == "" || len(id) > 64 {
			id = NewID()
		}
		c.Set(ctxRequestID, id)
		c.Header(HeaderRequestID, id)
		c.Next()
	}
}

// RequestIDFrom returns the current request id.
func RequestIDFrom(c *gin.Context) string { return c.GetString(ctxRequestID) }

// AccessLog writes one structured line per request (health checks excluded).
func AccessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if c.Request.URL.Path == "/healthz" {
			return
		}
		status := c.Writer.Status()
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}
		attrs := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", RequestIDFrom(c),
		}
		if uid := UserID(c); uid != 0 {
			attrs = append(attrs, "user_id", uid)
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "errors", c.Errors.String())
		}
		log.Log(c.Request.Context(), level, "request", attrs...)
	}
}

// Recover turns panics into 500 responses and reports them with the stack.
func Recover(service string, log *slog.Logger, reporter telemetry.Reporter) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rec) // client went away; let net/http handle it
			}
			stack := string(debug.Stack())
			log.Error("panic", "panic", fmt.Sprint(rec), "stack", stack, "request_id", RequestIDFrom(c))
			reporter.Report(telemetry.Event{
				Source:   service,
				Message:  fmt.Sprintf("panic: %v", rec),
				UserID:   UserID(c),
				Username: Username(c),
				Details: map[string]any{
					"method":     c.Request.Method,
					"path":       c.Request.URL.Path,
					"request_id": RequestIDFrom(c),
					"stack":      stack,
				},
			})
			if !c.Writer.Written() {
				c.AbortWithStatusJSON(http.StatusInternalServerError, Error{Code: "internal", Message: msgInternal})
			} else {
				c.Abort()
			}
		}()
		c.Next()
	}
}

// ReportErrors reports 5xx responses whose handler attached an error with
// Internal(). Panics are reported by Recover instead.
func ReportErrors(service string, log *slog.Logger, reporter telemetry.Reporter) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Writer.Status() < 500 || len(c.Errors) == 0 {
			return
		}
		reporter.Report(telemetry.Event{
			Source:   service,
			Message:  c.Errors.Last().Error(),
			UserID:   UserID(c),
			Username: Username(c),
			Details: map[string]any{
				"method":     c.Request.Method,
				"path":       c.FullPath(),
				"url":        c.Request.URL.String(),
				"status":     c.Writer.Status(),
				"request_id": RequestIDFrom(c),
				"errors":     c.Errors.Errors(),
			},
		})
	}
}

// InternalOnly rejects requests that do not carry the shared internal token,
// so that the services cannot be called bypassing the gateway even if one of
// their ports gets exposed by mistake.
func InternalOnly(token string) gin.HandlerFunc {
	expected := []byte(token)
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/healthz" {
			c.Next()
			return
		}
		got := []byte(c.GetHeader(HeaderInternalToken))
		if len(expected) == 0 || subtle.ConstantTimeCompare(got, expected) != 1 {
			Fail(c, http.StatusUnauthorized, "unauthorized", "Accés no autoritzat.")
			return
		}
		c.Next()
	}
}

// RequireUser reads the user identity forwarded by the gateway.
func RequireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.GetHeader(HeaderUserID), 10, 64)
		if err != nil || id <= 0 {
			Fail(c, http.StatusUnauthorized, "unauthorized", "Cal iniciar la sessió.")
			return
		}
		c.Set(ctxUserID, id)
		c.Set(ctxUsername, c.GetHeader(HeaderUsername))
		c.Next()
	}
}

// UserID returns the authenticated user id, or 0.
func UserID(c *gin.Context) int64 { return c.GetInt64(ctxUserID) }

// Username returns the authenticated user name, or "".
func Username(c *gin.Context) string { return c.GetString(ctxUsername) }

const msgInternal = "Alguna cosa ha anat malament. S'ha enviat un avís automàtic per a arreglar-ho."

// Fail aborts with a JSON error body.
func Fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, Error{Code: code, Message: message})
}

// BadRequest aborts with 400 and a user-facing message.
func BadRequest(c *gin.Context, message string) {
	Fail(c, http.StatusBadRequest, "invalid", message)
}

// NotFound aborts with 404.
func NotFound(c *gin.Context, message string) {
	if message == "" {
		message = "No s'ha trobat."
	}
	Fail(c, http.StatusNotFound, "not_found", message)
}

// Conflict aborts with 409.
func Conflict(c *gin.Context, code, message string) {
	Fail(c, http.StatusConflict, code, message)
}

// Internal records err (it is logged and reported by the middleware) and
// aborts with a generic 500.
func Internal(c *gin.Context, err error) {
	_ = c.Error(err)
	Fail(c, http.StatusInternalServerError, "internal", msgInternal)
}

// ParamID parses a positive int64 path parameter; on failure it aborts with 404.
func ParamID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		NotFound(c, "")
		return 0, false
	}
	return id, true
}

// BindJSON decodes the body into dst; on failure it aborts with 400.
func BindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		BadRequest(c, "Les dades enviades no són vàlides.")
		return false
	}
	return true
}

// Run serves handler on addr until SIGINT/SIGTERM, then shuts down gracefully.
func Run(addr string, handler http.Handler, log *slog.Logger) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// Healthcheck performs GET http://127.0.0.1<addr>/healthz and returns an exit
// code. It backs the "healthcheck" sub-command used by Docker, since the
// distroless images have neither curl nor wget.
func Healthcheck(addr string) int {
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + host + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.Status)
		return 1
	}
	return 0
}
