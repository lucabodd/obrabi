// Package config reads service configuration from environment variables.
//
// Every service collects its settings through an Env value so that all the
// missing or malformed variables are reported together at startup instead of
// failing one at a time.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	// Embed the timezone database so the binaries work on scratch/distroless
	// images without /usr/share/zoneinfo.
	_ "time/tzdata"
)

// Env accumulates configuration errors while reading variables.
type Env struct {
	problems []string
}

func lookup(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	return v, v != ""
}

// String returns the variable or def when unset/empty.
func (e *Env) String(key, def string) string {
	if v, ok := lookup(key); ok {
		return v
	}
	return def
}

// Required returns the variable and records a problem when it is missing.
func (e *Env) Required(key string) string {
	v, ok := lookup(key)
	if !ok {
		e.problems = append(e.problems, key+" is required")
	}
	return v
}

// Int returns the variable parsed as an int.
func (e *Env) Int(key string, def int) int {
	v, ok := lookup(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		e.problems = append(e.problems, fmt.Sprintf("%s: %q is not an integer", key, v))
		return def
	}
	return n
}

// Bool returns the variable parsed as a boolean (1/0, true/false, yes/no).
func (e *Env) Bool(key string, def bool) bool {
	v, ok := lookup(key)
	if !ok {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	}
	e.problems = append(e.problems, fmt.Sprintf("%s: %q is not a boolean", key, v))
	return def
}

// Duration returns the variable parsed with time.ParseDuration.
func (e *Env) Duration(key string, def time.Duration) time.Duration {
	v, ok := lookup(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		e.problems = append(e.problems, fmt.Sprintf("%s: %q is not a duration", key, v))
		return def
	}
	return d
}

// Location returns the time zone named by the variable.
func (e *Env) Location(key, def string) *time.Location {
	name := e.String(key, def)
	loc, err := time.LoadLocation(name)
	if err != nil {
		e.problems = append(e.problems, fmt.Sprintf("%s: unknown time zone %q", key, name))
		return time.UTC
	}
	return loc
}

// Secret is like Required but also enforces a minimum length, so that
// placeholder values such as "changeme" are rejected.
func (e *Env) Secret(key string, minLen int) string {
	v := e.Required(key)
	if v != "" && len(v) < minLen {
		e.problems = append(e.problems, fmt.Sprintf("%s must be at least %d characters long", key, minLen))
	}
	return v
}

// Err returns every problem found so far, or nil.
func (e *Env) Err() error {
	if len(e.problems) == 0 {
		return nil
	}
	return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(e.problems, "\n  - "))
}
