// Package projects implements the core service of Obrabi: towns (pobles),
// projects (obres), their line items (partides), expense categories and the
// payments received from clients (cobraments).
package projects

import "time"

// Project statuses.
const (
	StatusActive   = "active"
	StatusFinished = "finished"
)

// Line item kinds.
const (
	KindExpense = "expense" // material or other cost, optionally marked up
	KindLabor   = "labor"   // Nando's own work: hours and price
)

// Town is a poble with the number of projects it groups.
type Town struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	ActiveProjects   int64  `json:"active_projects"`
	FinishedProjects int64  `json:"finished_projects"`
}

// Ref is a lightweight {id, name} reference.
type Ref struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Category is an expense category with usage information.
type Category struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	Items    int64      `json:"items"`
	LastUsed *time.Time `json:"last_used"`
}

// Totals are the money figures of a project. See ComputeTotals.
type Totals struct {
	CostCents      int64   `json:"cost_cents"`      // paid by Nando
	PriceCents     int64   `json:"price_cents"`     // to be paid by the client
	BenefitCents   int64   `json:"benefit_cents"`   // price - cost
	CollectedCents int64   `json:"collected_cents"` // already paid by the client
	PendingCents   int64   `json:"pending_cents"`   // price - collected (negative: client overpaid)
	BalanceCents   int64   `json:"balance_cents"`   // collected - cost: cash position right now
	LaborHours     float64 `json:"labor_hours"`
	LaborCents     int64   `json:"labor_cents"`
	Items          int64   `json:"items"`
	Payments       int64   `json:"payments"`
}

// Project is an obra with its totals.
type Project struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Town        Ref        `json:"town"`
	ClientName  string     `json:"client_name"`
	ClientPhone string     `json:"client_phone"`
	Address     string     `json:"address"`
	Notes       string     `json:"notes"`
	Status      string     `json:"status"`
	StartedOn   string     `json:"started_on"`
	FinishedAt  *time.Time `json:"finished_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Totals      Totals     `json:"totals"`
}

// Item is a partida of a project.
type Item struct {
	ID          int64     `json:"id"`
	ProjectID   int64     `json:"project_id"`
	Kind        string    `json:"kind"`
	Category    *Ref      `json:"category"`
	Description string    `json:"description"`
	CostCents   int64     `json:"cost_cents"`
	PriceCents  int64     `json:"price_cents"`
	Hours       *float64  `json:"hours"`
	ItemDate    string    `json:"item_date"`
	CreatedAt   time.Time `json:"created_at"`
}

// Payment is money received from the client.
type Payment struct {
	ID          int64     `json:"id"`
	ProjectID   int64     `json:"project_id"`
	AmountCents int64     `json:"amount_cents"`
	PaidOn      string    `json:"paid_on"`
	Note        string    `json:"note"`
	CreatedAt   time.Time `json:"created_at"`
}

// ProjectDetail is a project with all its items and payments.
type ProjectDetail struct {
	Project
	Items    []Item    `json:"items"`
	Payments []Payment `json:"payments"`
}

// sums are the raw aggregates a project's totals derive from.
type sums struct {
	Cost, Price, Labor, Collected int64
	Hours                         float64
	Items, Payments               int64
}

// ComputeTotals derives every figure shown for a project from the sums of its
// items and payments.
//
// Example (Rafelguaraf): material cost 100 sold 130, plumbing cost 150 sold
// 170, labour 300 → cost 250, price 600, benefit 350; with no payments the
// client still owes 600 and Nando's cash position is -250.
func ComputeTotals(s sums) Totals {
	return Totals{
		CostCents:      s.Cost,
		PriceCents:     s.Price,
		BenefitCents:   s.Price - s.Cost,
		CollectedCents: s.Collected,
		PendingCents:   s.Price - s.Collected,
		BalanceCents:   s.Collected - s.Cost,
		LaborHours:     s.Hours,
		LaborCents:     s.Labor,
		Items:          s.Items,
		Payments:       s.Payments,
	}
}
