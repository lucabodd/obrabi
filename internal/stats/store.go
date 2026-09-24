package stats

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Figures are the money totals of a period.
//
// Revenue, cost and benefit are attributed to the date of each line item;
// collected money to the date of each payment.
type Figures struct {
	From           string  `json:"from,omitempty"`
	To             string  `json:"to,omitempty"`
	RevenueCents   int64   `json:"revenue_cents"`
	CostCents      int64   `json:"cost_cents"`
	BenefitCents   int64   `json:"benefit_cents"`
	CollectedCents int64   `json:"collected_cents"`
	LaborHours     float64 `json:"labor_hours"`
}

// MonthFigures are the figures of one calendar month.
type MonthFigures struct {
	Month string `json:"month"`
	Figures
}

// CategoryFigures break a period down by expense category. Labour has no
// category and is reported as its own group (Kind == "labor", ID == nil).
type CategoryFigures struct {
	ID           *int64 `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	RevenueCents int64  `json:"revenue_cents"`
	CostCents    int64  `json:"cost_cents"`
	BenefitCents int64  `json:"benefit_cents"`
	Items        int64  `json:"items"`
}

// Outstanding summarises what clients still owe.
type Outstanding struct {
	PendingCents        int64 `json:"pending_cents"`
	ProjectsWithPending int64 `json:"projects_with_pending"`
	ActiveProjects      int64 `json:"active_projects"`
}

// Store queries the projects schema (read only).
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

// Period returns the figures between two days, both included.
func (s *Store) Period(ctx context.Context, uid int64, from, to time.Time) (Figures, error) {
	f := Figures{From: from.Format(dateLayout), To: to.Format(dateLayout)}
	err := s.db.QueryRow(ctx, `
		SELECT
		    COALESCE((SELECT SUM(price_cents) FROM projects.line_items
		              WHERE user_id = $1 AND item_date BETWEEN $2::date AND $3::date), 0)::bigint,
		    COALESCE((SELECT SUM(cost_cents) FROM projects.line_items
		              WHERE user_id = $1 AND item_date BETWEEN $2::date AND $3::date), 0)::bigint,
		    COALESCE((SELECT SUM(hours) FROM projects.line_items
		              WHERE user_id = $1 AND kind = 'labor' AND item_date BETWEEN $2::date AND $3::date), 0)::float8,
		    COALESCE((SELECT SUM(amount_cents) FROM projects.payments
		              WHERE user_id = $1 AND paid_on BETWEEN $2::date AND $3::date), 0)::bigint`,
		uid, f.From, f.To).Scan(&f.RevenueCents, &f.CostCents, &f.LaborHours, &f.CollectedCents)
	f.BenefitCents = f.RevenueCents - f.CostCents
	return f, err
}

// Monthly returns one entry per month from..to (inclusive), zero-filled.
func (s *Store) Monthly(ctx context.Context, uid int64, from, to Month) ([]MonthFigures, error) {
	rows, err := s.db.Query(ctx, `
		WITH months AS (
		    SELECT generate_series($2::date::timestamp, $3::date::timestamp, interval '1 month')::date AS m
		),
		items AS (
		    SELECT date_trunc('month', item_date)::date AS m,
		           SUM(price_cents)::bigint AS revenue,
		           SUM(cost_cents)::bigint AS cost,
		           COALESCE(SUM(hours) FILTER (WHERE kind = 'labor'), 0)::float8 AS hours
		    FROM projects.line_items
		    WHERE user_id = $1 AND item_date >= $2::date AND item_date < ($3::date + interval '1 month')
		    GROUP BY 1
		),
		pays AS (
		    SELECT date_trunc('month', paid_on)::date AS m, SUM(amount_cents)::bigint AS collected
		    FROM projects.payments
		    WHERE user_id = $1 AND paid_on >= $2::date AND paid_on < ($3::date + interval '1 month')
		    GROUP BY 1
		)
		SELECT to_char(months.m, 'YYYY-MM'),
		       COALESCE(items.revenue, 0), COALESCE(items.cost, 0), COALESCE(items.hours, 0),
		       COALESCE(pays.collected, 0)
		FROM months
		LEFT JOIN items USING (m)
		LEFT JOIN pays USING (m)
		ORDER BY months.m`,
		uid, from.First().Format(dateLayout), to.First().Format(dateLayout))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (MonthFigures, error) {
		var m MonthFigures
		err := row.Scan(&m.Month, &m.RevenueCents, &m.CostCents, &m.LaborHours, &m.CollectedCents)
		m.BenefitCents = m.RevenueCents - m.CostCents
		return m, err
	})
}

// Categories returns revenue, cost and benefit per category in from..to,
// best benefit first.
func (s *Store) Categories(ctx context.Context, uid int64, from, to Month) ([]CategoryFigures, error) {
	rows, err := s.db.Query(ctx, `
		SELECT li.kind, c.id, COALESCE(c.name, ''),
		       SUM(li.price_cents)::bigint, SUM(li.cost_cents)::bigint, COUNT(*)
		FROM projects.line_items li
		LEFT JOIN projects.categories c ON c.id = li.category_id
		WHERE li.user_id = $1 AND li.item_date >= $2::date AND li.item_date < ($3::date + interval '1 month')
		GROUP BY li.kind, c.id, c.name
		ORDER BY SUM(li.price_cents - li.cost_cents) DESC, c.name`,
		uid, from.First().Format(dateLayout), to.First().Format(dateLayout))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (CategoryFigures, error) {
		var c CategoryFigures
		err := row.Scan(&c.Kind, &c.ID, &c.Name, &c.RevenueCents, &c.CostCents, &c.Items)
		if c.Kind == "labor" {
			c.Name = "Mà d'obra"
		}
		c.BenefitCents = c.RevenueCents - c.CostCents
		return c, err
	})
}

// Outstanding returns the money still owed by clients over every project
// (finished ones included: a job can be archived before it is fully paid).
func (s *Store) Outstanding(ctx context.Context, uid int64) (Outstanding, error) {
	var o Outstanding
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(GREATEST(i.price - pay.amount, 0)), 0)::bigint,
		       COUNT(*) FILTER (WHERE i.price - pay.amount > 0),
		       COUNT(*) FILTER (WHERE p.status = 'active')
		FROM projects.projects p
		CROSS JOIN LATERAL (
		    SELECT COALESCE(SUM(price_cents), 0) AS price FROM projects.line_items WHERE project_id = p.id
		) i
		CROSS JOIN LATERAL (
		    SELECT COALESCE(SUM(amount_cents), 0) AS amount FROM projects.payments WHERE project_id = p.id
		) pay
		WHERE p.user_id = $1`, uid).Scan(&o.PendingCents, &o.ProjectsWithPending, &o.ActiveProjects)
	return o, err
}
