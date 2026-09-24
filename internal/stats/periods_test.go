package stats

import (
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestMonths(t *testing.T) {
	m, err := ParseMonth("2026-01")
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Add(-1).String(); got != "2025-12" {
		t.Errorf("Add(-1) = %s", got)
	}
	if got := m.Last().Format(dateLayout); got != "2026-01-31" {
		t.Errorf("Last = %s", got)
	}
	feb, _ := ParseMonth("2024-02")
	if got := feb.Last().Format(dateLayout); got != "2024-02-29" {
		t.Errorf("leap Last = %s", got)
	}
	if n := MonthsBetween(m.Add(-11), m); n != 12 {
		t.Errorf("MonthsBetween = %d", n)
	}
	if n := MonthsBetween(m, m.Add(-1)); n != 0 {
		t.Errorf("MonthsBetween reversed = %d", n)
	}
	for _, bad := range []string{"", "2026-13", "26-01", "1999-12"} {
		if _, err := ParseMonth(bad); err == nil {
			t.Errorf("ParseMonth(%q) accepted", bad)
		}
	}
}

func TestSameDay(t *testing.T) {
	cases := []struct{ in, month, want string }{
		{"2026-03-31", "2026-02", "2026-02-28"},
		{"2026-09-24", "2026-08", "2026-08-24"},
		{"2024-03-30", "2024-02", "2024-02-29"},
	}
	for _, c := range cases {
		m, _ := ParseMonth(c.month)
		if got := SameDayIn(day(c.in), m).Format(dateLayout); got != c.want {
			t.Errorf("SameDayIn(%s, %s) = %s, want %s", c.in, c.month, got, c.want)
		}
	}
	if got := SameDayLastYear(day("2024-02-29")).Format(dateLayout); got != "2023-02-28" {
		t.Errorf("SameDayLastYear(2024-02-29) = %s", got)
	}
}
