package feedback

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lucabodd/obrabi/internal/platform/httpx"
	"github.com/lucabodd/obrabi/internal/platform/ratelimit"
	"github.com/lucabodd/obrabi/internal/platform/telemetry"
)

// dedupWindow: an error already reported within this window only bumps its
// occurrence counter.
const dedupWindow = 6 * time.Hour

// Handlers exposes the feedback HTTP API.
type Handlers struct {
	store       *Store
	worker      *Worker
	log         *slog.Logger
	perUser     *ratelimit.Limiter // browser telemetry, per user
	perService  *ratelimit.Limiter // internal telemetry, per source
	perFeedback *ratelimit.Limiter // suggestions/bugs, per user
}

func NewHandlers(store *Store, worker *Worker, log *slog.Logger) *Handlers {
	return &Handlers{
		store:       store,
		worker:      worker,
		log:         log,
		perUser:     ratelimit.New(30, time.Hour),
		perService:  ratelimit.New(300, time.Hour),
		perFeedback: ratelimit.New(30, time.Hour),
	}
}

// Register mounts the routes on r.
func (h *Handlers) Register(r gin.IRouter) {
	r.POST("/internal/telemetry", h.internalTelemetry)

	g := r.Group("", httpx.RequireUser())
	g.GET("/feedback", h.list)
	g.POST("/feedback", h.create)
	g.POST("/telemetry", h.browserTelemetry)
}

type createRequest struct {
	Kind       string `json:"kind"`
	Message    string `json:"message"`
	Page       string `json:"page"`
	Viewport   string `json:"viewport"`
	AppVersion string `json:"app_version"`
}

func (h *Handlers) create(c *gin.Context) {
	var req createRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	if req.Kind != KindSuggestion && req.Kind != KindBug {
		httpx.BadRequest(c, "Tria si és un suggeriment o un error.")
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		httpx.BadRequest(c, "Escriu el missatge.")
		return
	}
	if len([]rune(msg)) > 5000 {
		httpx.BadRequest(c, "El missatge és massa llarg (màxim 5.000 caràcters).")
		return
	}
	uid := httpx.UserID(c)
	if !h.perFeedback.Allow(strconv.FormatInt(uid, 10)) {
		httpx.Fail(c, http.StatusTooManyRequests, "rate_limited", "Has enviat molts missatges seguits. Torna-ho a provar d'ací a una estona.")
		return
	}
	report, err := h.store.Create(c, Report{
		Kind:     req.Kind,
		UserID:   &uid,
		Username: httpx.Username(c),
		Source:   "web",
		Message:  msg,
		Details: limitDetails(map[string]any{
			"pagina":   truncate(req.Page, 300),
			"browser":  truncate(c.GetHeader("User-Agent"), 300),
			"schermo":  truncate(req.Viewport, 40),
			"versione": truncate(req.AppVersion, 40),
		}),
	})
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	h.worker.Wake()
	c.JSON(http.StatusCreated, gin.H{"report": report})
}

func (h *Handlers) list(c *gin.Context) {
	reports, err := h.store.ListByUser(c, httpx.UserID(c), 50)
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"reports": reports})
}

// browserEvent is an error caught by the web app.
type browserEvent struct {
	Type     string `json:"type"` // error | unhandledrejection | api
	Message  string `json:"message"`
	Stack    string `json:"stack"`
	Script   string `json:"script"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Page     string `json:"page"`
	Status   int    `json:"status"`
	Endpoint string `json:"endpoint"`
	Viewport string `json:"viewport"`
	Version  string `json:"app_version"`
}

func (h *Handlers) browserTelemetry(c *gin.Context) {
	var ev browserEvent
	if !httpx.BindJSON(c, &ev) {
		return
	}
	if strings.TrimSpace(ev.Message) == "" {
		httpx.BadRequest(c, "Missatge buit.")
		return
	}
	uid := httpx.UserID(c)
	if !h.perUser.Allow(strconv.FormatInt(uid, 10)) {
		c.Status(http.StatusAccepted) // silently dropped: the user must not notice
		return
	}
	details := map[string]any{
		"tipo":     ev.Type,
		"pagina":   ev.Page,
		"browser":  c.GetHeader("User-Agent"),
		"schermo":  ev.Viewport,
		"versione": ev.Version,
	}
	if ev.Script != "" {
		details["script"] = stripQuery(ev.Script) + ":" + strconv.Itoa(ev.Line) + ":" + strconv.Itoa(ev.Column)
	}
	if ev.Endpoint != "" {
		details["endpoint"] = ev.Endpoint
		details["status"] = ev.Status
	}
	if ev.Stack != "" {
		details["stack"] = ev.Stack
	}
	where := firstLine(ev.Stack)
	if where == "" {
		where = stripQuery(ev.Script) + stripQuery(ev.Endpoint)
	}
	err := h.record(c, telemetry.Event{
		Source:   "web",
		Message:  ev.Message,
		Details:  details,
		UserID:   uid,
		Username: httpx.Username(c),
	}, where)
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	c.Status(http.StatusAccepted)
}

func (h *Handlers) internalTelemetry(c *gin.Context) {
	var ev telemetry.Event
	if !httpx.BindJSON(c, &ev) {
		return
	}
	if strings.TrimSpace(ev.Message) == "" {
		httpx.BadRequest(c, "empty message")
		return
	}
	if !h.perService.Allow(ev.Source) {
		h.log.Warn("telemetry rate limit reached, event dropped", "source", ev.Source, "message", ev.Message)
		c.Status(http.StatusAccepted)
		return
	}
	where := ""
	if p, ok := ev.Details["path"].(string); ok {
		where = p
	}
	if err := h.record(c, ev, where); err != nil {
		// Do not use httpx.Internal: reporting a telemetry failure to
		// ourselves could loop.
		h.log.Error("record telemetry", "err", err)
		httpx.Fail(c, http.StatusInternalServerError, "internal", "cannot record event")
		return
	}
	c.Status(http.StatusAccepted)
}

// record stores an automatic error report and wakes the mail worker when the
// error is new.
func (h *Handlers) record(ctx context.Context, ev telemetry.Event, where string) error {
	var uid *int64
	if ev.UserID > 0 {
		uid = &ev.UserID
	}
	source := truncate(strings.TrimSpace(ev.Source), 40)
	msg := truncate(strings.TrimSpace(ev.Message), 2000)
	_, isNew, err := h.store.RecordError(ctx, Report{
		UserID:   uid,
		Username: truncate(ev.Username, 60),
		Source:   source,
		Message:  msg,
		Details:  limitDetails(ev.Details),
	}, Fingerprint(source, msg, where), dedupWindow)
	if err != nil {
		return err
	}
	if isNew {
		h.worker.Wake()
	}
	return nil
}

// Reporter lets the feedback service report its own errors without an HTTP
// round trip to itself.
func (h *Handlers) Reporter() telemetry.Reporter { return localReporter{h} }

type localReporter struct{ h *Handlers }

func (l localReporter) Report(ev telemetry.Event) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		where, _ := ev.Details["path"].(string)
		if err := l.h.record(ctx, ev, where); err != nil {
			l.h.log.Error("record own error", "err", err, "message", ev.Message)
		}
	}()
}

func stripQuery(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}

// firstLine returns the first stack frame line that points into our code.
func firstLine(stack string) string {
	for _, line := range strings.Split(stack, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "/js/") || strings.Contains(line, ".go:") {
			return line
		}
	}
	return ""
}
