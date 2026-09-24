// Package stats implements the analytics service behind the dashboard. It is
// a read-only consumer of the projects schema (CQRS-style read side): it never
// writes and owns no tables.
package stats

import (
	"errors"
	"time"
)

const dateLayout = "2006-01-02"

// Month is the first day of a calendar month (UTC, no time part).
type Month struct{ time.Time }

// ParseMonth parses "YYYY-MM".
func ParseMonth(s string) (Month, error) {
	t, err := time.Parse("2006-01", s)
	if err != nil || t.Year() < 2000 || t.Year() > 2100 {
		return Month{}, errors.New("invalid month")
	}
	return Month{t}, nil
}

// MonthOf returns the month containing t.
func MonthOf(t time.Time) Month {
	return Month{time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)}
}

func (m Month) String() string { return m.Format("2006-01") }

// Add returns the month n months later (n may be negative).
func (m Month) Add(n int) Month { return Month{m.AddDate(0, n, 0)} }

// First is the first day of the month.
func (m Month) First() time.Time { return m.Time }

// Last is the last day of the month.
func (m Month) Last() time.Time { return m.AddDate(0, 1, -1) }

// MonthsBetween counts the months from a to b inclusive (0 when b < a).
func MonthsBetween(a, b Month) int {
	n := (b.Year()-a.Year())*12 + int(b.Month()) - int(a.Month()) + 1
	if n < 0 {
		return 0
	}
	return n
}

// Day is a calendar day (UTC, no time part).
func Day(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// SameDayIn returns the day of month d moved into month m, clamped to the
// month's length (31 March -> 28/29 February).
func SameDayIn(d time.Time, m Month) time.Time {
	last := m.Last()
	if d.Day() > last.Day() {
		return last
	}
	return time.Date(m.Year(), m.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
}

// SameDayLastYear returns d one year earlier (29 February -> 28 February).
func SameDayLastYear(d time.Time) time.Time {
	return SameDayIn(d, MonthOf(d).Add(-12))
}
