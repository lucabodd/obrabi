package stats

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lucabodd/obrabi/internal/platform/httpx"
)

// Longest range a chart may ask for.
const maxMonths = 120

// Handlers exposes the stats HTTP API.
type Handlers struct {
	Store *Store
	Log   *slog.Logger
	Loc   *time.Location
	// Now is overridable in tests.
	Now func() time.Time
}

// Register mounts the routes on r.
func (h *Handlers) Register(r gin.IRouter) {
	g := r.Group("/stats", httpx.RequireUser())
	g.GET("/overview", h.overview)
	g.GET("/monthly", h.monthly)
	g.GET("/categories", h.categories)
	g.GET("/yoy", h.yoy)
}

func (h *Handlers) today() time.Time {
	now := time.Now
	if h.Now != nil {
		now = h.Now
	}
	return Day(now().In(h.Loc))
}

// overview answers the headline numbers of the home page: this month so far,
// the same days of last month, the whole of last month, the year to date
// against the same period of last year, and what clients still owe.
func (h *Handlers) overview(c *gin.Context) {
	uid := httpx.UserID(c)
	today := h.today()
	if v := c.Query("today"); v != "" {
		t, err := time.Parse(dateLayout, v)
		if err != nil {
			httpx.BadRequest(c, "La data no és vàlida.")
			return
		}
		today = t
	}
	month := MonthOf(today)
	prev := month.Add(-1)
	yearStart := time.Date(today.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	lastYearToday := SameDayLastYear(today)

	periods := []struct {
		key      string
		from, to time.Time
	}{
		{"month", month.First(), today},
		{"previous_month_to_date", prev.First(), SameDayIn(today, prev)},
		{"previous_month", prev.First(), prev.Last()},
		{"year_to_date", yearStart, today},
		{"previous_year_to_date", yearStart.AddDate(-1, 0, 0), lastYearToday},
	}
	out := gin.H{
		"today":      today.Format(dateLayout),
		"this_month": month.String(),
		"last_month": prev.String(),
	}
	for _, p := range periods {
		f, err := h.Store.Period(c, uid, p.from, p.to)
		if err != nil {
			httpx.Internal(c, err)
			return
		}
		out[p.key] = f
	}
	o, err := h.Store.Outstanding(c, uid)
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	out["outstanding"] = o
	c.JSON(http.StatusOK, out)
}

// monthRange reads ?from=YYYY-MM&to=YYYY-MM, defaulting to the last 12 months.
func (h *Handlers) monthRange(c *gin.Context) (Month, Month, bool) {
	to := MonthOf(h.today())
	from := to.Add(-11)
	var err error
	if v := c.Query("to"); v != "" {
		if to, err = ParseMonth(v); err != nil {
			httpx.BadRequest(c, "El mes final no és vàlid.")
			return Month{}, Month{}, false
		}
		if c.Query("from") == "" {
			from = to.Add(-11)
		}
	}
	if v := c.Query("from"); v != "" {
		if from, err = ParseMonth(v); err != nil {
			httpx.BadRequest(c, "El mes inicial no és vàlid.")
			return Month{}, Month{}, false
		}
	}
	n := MonthsBetween(from, to)
	if n == 0 {
		httpx.BadRequest(c, "El mes inicial ha de ser anterior al final.")
		return Month{}, Month{}, false
	}
	if n > maxMonths {
		httpx.BadRequest(c, "El període és massa llarg (màxim 10 anys).")
		return Month{}, Month{}, false
	}
	return from, to, true
}

func (h *Handlers) monthly(c *gin.Context) {
	from, to, ok := h.monthRange(c)
	if !ok {
		return
	}
	months, err := h.Store.Monthly(c, httpx.UserID(c), from, to)
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"from": from.String(), "to": to.String(), "months": months})
}

func (h *Handlers) categories(c *gin.Context) {
	from, to, ok := h.monthRange(c)
	if !ok {
		return
	}
	cats, err := h.Store.Categories(c, httpx.UserID(c), from, to)
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"from": from.String(), "to": to.String(), "categories": cats})
}

// yoyMonth pairs a calendar month of the requested year with the same month
// of the previous year.
type yoyMonth struct {
	Month    int     `json:"month"`
	Current  Figures `json:"current"`
	Previous Figures `json:"previous"`
}

func (h *Handlers) yoy(c *gin.Context) {
	year := h.today().Year()
	if v := c.Query("year"); v != "" {
		y, err := strconv.Atoi(v)
		if err != nil || y < 2001 || y > 2100 {
			httpx.BadRequest(c, "L'any no és vàlid.")
			return
		}
		year = y
	}
	jan := Month{time.Date(year-1, 1, 1, 0, 0, 0, 0, time.UTC)}
	months, err := h.Store.Monthly(c, httpx.UserID(c), jan, jan.Add(23))
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	out := make([]yoyMonth, 12)
	for i := range out {
		out[i] = yoyMonth{Month: i + 1, Previous: months[i].Figures, Current: months[12+i].Figures}
	}
	c.JSON(http.StatusOK, gin.H{"year": year, "previous_year": year - 1, "months": out})
}
