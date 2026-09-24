// Package telemetry ships error events to the feedback service, which stores
// them and e-mails a bug report to the maintainer.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Event describes something that went wrong.
type Event struct {
	Source   string         `json:"source"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details,omitempty"`
	UserID   int64          `json:"user_id,omitempty"`
	Username string         `json:"username,omitempty"`
}

// Reporter accepts events without blocking the caller.
type Reporter interface {
	Report(Event)
}

// Discard drops every event.
type Discard struct{}

func (Discard) Report(Event) {}

// Client posts events to the feedback service's internal endpoint.
type Client struct {
	endpoint string
	token    string
	source   string
	http     *http.Client
	log      *slog.Logger
	slots    chan struct{}
}

// New returns a Reporter for the feedback service at baseURL. With an empty
// baseURL events are only logged.
func New(baseURL, internalToken, source string, log *slog.Logger) Reporter {
	if strings.TrimSpace(baseURL) == "" {
		return logOnly{log: log}
	}
	return &Client{
		endpoint: strings.TrimRight(baseURL, "/") + "/internal/telemetry",
		token:    internalToken,
		source:   source,
		http:     &http.Client{Timeout: 5 * time.Second},
		log:      log,
		slots:    make(chan struct{}, 8),
	}
}

// Report sends the event in the background. If too many reports are already
// in flight the event is logged and dropped rather than piling up goroutines.
func (c *Client) Report(ev Event) {
	if ev.Source == "" {
		ev.Source = c.source
	}
	select {
	case c.slots <- struct{}{}:
	default:
		c.log.Warn("telemetry saturated, event dropped", "message", ev.Message)
		return
	}
	go func() {
		defer func() { <-c.slots }()
		if err := c.send(ev); err != nil {
			c.log.Warn("telemetry delivery failed", "err", err, "message", ev.Message)
		}
	}()
}

func (c *Client) send(ev Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("feedback service answered %s", resp.Status)
	}
	return nil
}

type logOnly struct{ log *slog.Logger }

func (l logOnly) Report(ev Event) {
	l.log.Error("telemetry event (no feedback service configured)", "source", ev.Source, "message", ev.Message, "details", ev.Details)
}
