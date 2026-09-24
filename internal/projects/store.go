package projects

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lucabodd/obrabi/internal/platform/database"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("duplicate name")
	ErrInUse     = errors.New("still in use")
)

// querier is satisfied by both the pool and a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store runs every query of the projects service. All methods are scoped to
// the user id they receive.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case database.IsUniqueViolation(err):
		return ErrDuplicate
	case database.IsForeignKeyViolation(err):
		return ErrInUse
	}
	return err
}

func expectRow(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------- towns

// ListTowns returns the user's towns with their project counts.
func (s *Store) ListTowns(ctx context.Context, uid int64) ([]Town, error) {
	rows, err := s.db.Query(ctx, `
		SELECT t.id, t.name,
		       COUNT(p.id) FILTER (WHERE p.status = 'active'),
		       COUNT(p.id) FILTER (WHERE p.status = 'finished')
		FROM projects.towns t
		LEFT JOIN projects.projects p ON p.town_id = t.id
		WHERE t.user_id = $1
		GROUP BY t.id, t.name
		ORDER BY lower(t.name)`, uid)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Town, error) {
		var t Town
		err := row.Scan(&t.ID, &t.Name, &t.ActiveProjects, &t.FinishedProjects)
		return t, err
	})
}

// upsertTown returns the town with that name (case-insensitive), creating it
// when needed.
func upsertTown(ctx context.Context, q querier, uid int64, name string) (Ref, error) {
	var r Ref
	err := q.QueryRow(ctx, `
		INSERT INTO projects.towns AS t (user_id, name) VALUES ($1, $2)
		ON CONFLICT (user_id, lower(name)) DO UPDATE SET name = t.name
		RETURNING id, name`, uid, name).Scan(&r.ID, &r.Name)
	return r, err
}

// CreateTown creates a town, or returns the existing one with the same name.
func (s *Store) CreateTown(ctx context.Context, uid int64, name string) (Ref, error) {
	return upsertTown(ctx, s.db, uid, name)
}

// RenameTown renames a town.
func (s *Store) RenameTown(ctx context.Context, uid, id int64, name string) (Ref, error) {
	var r Ref
	err := s.db.QueryRow(ctx, `
		UPDATE projects.towns SET name = $3, updated_at = now()
		WHERE id = $2 AND user_id = $1
		RETURNING id, name`, uid, id, name).Scan(&r.ID, &r.Name)
	return r, mapErr(err)
}

// DeleteTown deletes a town; ErrInUse when it still groups projects.
func (s *Store) DeleteTown(ctx context.Context, uid, id int64) error {
	return expectRow(s.db.Exec(ctx, `DELETE FROM projects.towns WHERE id = $2 AND user_id = $1`, uid, id))
}

func townOwned(ctx context.Context, q querier, uid, id int64) error {
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM projects.towns WHERE id = $2 AND user_id = $1)`, uid, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------- categories

// ListCategories returns the user's categories, most recently used first.
func (s *Store) ListCategories(ctx context.Context, uid int64) ([]Category, error) {
	rows, err := s.db.Query(ctx, `
		SELECT c.id, c.name, COUNT(li.id), MAX(li.created_at)
		FROM projects.categories c
		LEFT JOIN projects.line_items li ON li.category_id = c.id
		WHERE c.user_id = $1
		GROUP BY c.id, c.name
		ORDER BY MAX(li.created_at) DESC NULLS LAST, lower(c.name)`, uid)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Category, error) {
		var c Category
		err := row.Scan(&c.ID, &c.Name, &c.Items, &c.LastUsed)
		return c, err
	})
}

func upsertCategory(ctx context.Context, q querier, uid int64, name string) (Ref, error) {
	var r Ref
	err := q.QueryRow(ctx, `
		INSERT INTO projects.categories AS c (user_id, name) VALUES ($1, $2)
		ON CONFLICT (user_id, lower(name)) DO UPDATE SET name = c.name
		RETURNING id, name`, uid, name).Scan(&r.ID, &r.Name)
	return r, err
}

// CreateCategory creates a category, or returns the existing one with the
// same name.
func (s *Store) CreateCategory(ctx context.Context, uid int64, name string) (Ref, error) {
	return upsertCategory(ctx, s.db, uid, name)
}

// RenameCategory renames a category.
func (s *Store) RenameCategory(ctx context.Context, uid, id int64, name string) (Ref, error) {
	var r Ref
	err := s.db.QueryRow(ctx, `
		UPDATE projects.categories SET name = $3, updated_at = now()
		WHERE id = $2 AND user_id = $1
		RETURNING id, name`, uid, id, name).Scan(&r.ID, &r.Name)
	return r, mapErr(err)
}

// DeleteCategory deletes a category. With mergeInto > 0 its items are first
// moved to that category (this is how duplicates get merged); otherwise a
// category still used by items is not deleted and ErrInUse is returned
// together with the number of items using it.
func (s *Store) DeleteCategory(ctx context.Context, uid, id, mergeInto int64) (int64, error) {
	var used int64
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := categoryOwned(ctx, tx, uid, id); err != nil {
			return err
		}
		if mergeInto > 0 {
			if err := categoryOwned(ctx, tx, uid, mergeInto); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE projects.line_items SET category_id = $3, updated_at = now()
				WHERE category_id = $2 AND user_id = $1`, uid, id, mergeInto); err != nil {
				return err
			}
		} else {
			if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM projects.line_items WHERE category_id = $1`, id).Scan(&used); err != nil {
				return err
			}
			if used > 0 {
				return ErrInUse
			}
		}
		_, err := tx.Exec(ctx, `DELETE FROM projects.categories WHERE id = $2 AND user_id = $1`, uid, id)
		return err
	})
	return used, mapErr(err)
}

func categoryOwned(ctx context.Context, q querier, uid, id int64) error {
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM projects.categories WHERE id = $2 AND user_id = $1)`, uid, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------- projects

// projectSelect reads projects with the aggregates their totals need.
const projectSelect = `
SELECT p.id, p.name, t.id, t.name, p.client_name, p.client_phone, p.address, p.notes, p.status,
       to_char(p.started_on, 'YYYY-MM-DD'), p.finished_at, p.created_at, p.updated_at,
       COALESCE(i.cost, 0), COALESCE(i.price, 0), COALESCE(i.labor, 0), COALESCE(i.hours, 0), i.n,
       COALESCE(pay.amount, 0), pay.n
FROM projects.projects p
JOIN projects.towns t ON t.id = p.town_id
CROSS JOIN LATERAL (
    SELECT SUM(li.cost_cents)::bigint                                  AS cost,
           SUM(li.price_cents)::bigint                                 AS price,
           (SUM(li.price_cents) FILTER (WHERE li.kind = 'labor'))::bigint AS labor,
           (SUM(li.hours) FILTER (WHERE li.kind = 'labor'))::float8       AS hours,
           COUNT(*)                                                    AS n
    FROM projects.line_items li
    WHERE li.project_id = p.id
) i
CROSS JOIN LATERAL (
    SELECT SUM(pm.amount_cents)::bigint AS amount, COUNT(*) AS n
    FROM projects.payments pm
    WHERE pm.project_id = p.id
) pay
`

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	var s sums
	err := row.Scan(&p.ID, &p.Name, &p.Town.ID, &p.Town.Name, &p.ClientName, &p.ClientPhone, &p.Address,
		&p.Notes, &p.Status, &p.StartedOn, &p.FinishedAt, &p.CreatedAt, &p.UpdatedAt,
		&s.Cost, &s.Price, &s.Labor, &s.Hours, &s.Items, &s.Collected, &s.Payments)
	if err != nil {
		return Project{}, err
	}
	p.Totals = ComputeTotals(s)
	return p, nil
}

// ListProjects returns the user's projects with the given status ("" = all),
// most recently active first.
func (s *Store) ListProjects(ctx context.Context, uid int64, status string) ([]Project, error) {
	rows, err := s.db.Query(ctx, projectSelect+`
		WHERE p.user_id = $1 AND ($2 = '' OR p.status = $2)
		ORDER BY p.updated_at DESC, p.id DESC`, uid, status)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Project, error) { return scanProject(row) })
}

// GetProject returns a project with its items and payments.
func (s *Store) GetProject(ctx context.Context, uid, id int64) (ProjectDetail, error) {
	return getProject(ctx, s.db, uid, id)
}

func getProject(ctx context.Context, q querier, uid, id int64) (ProjectDetail, error) {
	p, err := scanProject(q.QueryRow(ctx, projectSelect+` WHERE p.user_id = $1 AND p.id = $2`, uid, id))
	if err != nil {
		return ProjectDetail{}, mapErr(err)
	}
	d := ProjectDetail{Project: p}

	rows, err := q.Query(ctx, itemSelect+` WHERE li.project_id = $2 AND li.user_id = $1
		ORDER BY li.item_date DESC, li.id DESC`, uid, id)
	if err != nil {
		return ProjectDetail{}, err
	}
	if d.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Item, error) { return scanItem(row) }); err != nil {
		return ProjectDetail{}, err
	}

	rows, err = q.Query(ctx, paymentSelect+` WHERE project_id = $2 AND user_id = $1
		ORDER BY paid_on DESC, id DESC`, uid, id)
	if err != nil {
		return ProjectDetail{}, err
	}
	if d.Payments, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Payment, error) { return scanPayment(row) }); err != nil {
		return ProjectDetail{}, err
	}
	return d, nil
}

// ProjectInput holds the editable fields of a project. The town is given
// either by id or by name (created when missing).
type ProjectInput struct {
	Name        string `json:"name"`
	TownID      int64  `json:"town_id"`
	TownName    string `json:"town_name"`
	ClientName  string `json:"client_name"`
	ClientPhone string `json:"client_phone"`
	Address     string `json:"address"`
	Notes       string `json:"notes"`
	StartedOn   string `json:"started_on"`
}

func resolveTown(ctx context.Context, q querier, uid int64, in ProjectInput) (int64, error) {
	if in.TownID > 0 {
		return in.TownID, townOwned(ctx, q, uid, in.TownID)
	}
	t, err := upsertTown(ctx, q, uid, in.TownName)
	return t.ID, err
}

// CreateProject creates an active project and returns it.
func (s *Store) CreateProject(ctx context.Context, uid int64, in ProjectInput) (ProjectDetail, error) {
	var d ProjectDetail
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		townID, err := resolveTown(ctx, tx, uid, in)
		if err != nil {
			return err
		}
		var id int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO projects.projects (user_id, town_id, name, client_name, client_phone, address, notes, started_on)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date)
			RETURNING id`,
			uid, townID, in.Name, in.ClientName, in.ClientPhone, in.Address, in.Notes, in.StartedOn).Scan(&id); err != nil {
			return err
		}
		d, err = getProject(ctx, tx, uid, id)
		return err
	})
	return d, mapErr(err)
}

// UpdateProject replaces the editable fields of a project.
func (s *Store) UpdateProject(ctx context.Context, uid, id int64, in ProjectInput) (ProjectDetail, error) {
	var d ProjectDetail
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		townID, err := resolveTown(ctx, tx, uid, in)
		if err != nil {
			return err
		}
		if err := expectRow(tx.Exec(ctx, `
			UPDATE projects.projects
			SET town_id = $3, name = $4, client_name = $5, client_phone = $6, address = $7, notes = $8,
			    started_on = $9::date, updated_at = now()
			WHERE id = $2 AND user_id = $1`,
			uid, id, townID, in.Name, in.ClientName, in.ClientPhone, in.Address, in.Notes, in.StartedOn)); err != nil {
			return err
		}
		d, err = getProject(ctx, tx, uid, id)
		return err
	})
	return d, mapErr(err)
}

// SetStatus finishes (archives) or reopens a project.
func (s *Store) SetStatus(ctx context.Context, uid, id int64, status string) (ProjectDetail, error) {
	var d ProjectDetail
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := expectRow(tx.Exec(ctx, `
			UPDATE projects.projects
			SET status = $3::text,
			    finished_at = CASE WHEN $3::text = 'finished' THEN COALESCE(finished_at, now()) END,
			    updated_at = now()
			WHERE id = $2 AND user_id = $1`, uid, id, status)); err != nil {
			return err
		}
		var err error
		d, err = getProject(ctx, tx, uid, id)
		return err
	})
	return d, mapErr(err)
}

// DeleteProject deletes a project with all its items and payments.
func (s *Store) DeleteProject(ctx context.Context, uid, id int64) error {
	return expectRow(s.db.Exec(ctx, `DELETE FROM projects.projects WHERE id = $2 AND user_id = $1`, uid, id))
}

func projectOwned(ctx context.Context, q querier, uid, id int64) error {
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM projects.projects WHERE id = $2 AND user_id = $1)`, uid, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------- items

const itemSelect = `
SELECT li.id, li.project_id, li.kind, c.id, c.name, li.description, li.cost_cents, li.price_cents,
       li.hours::float8, to_char(li.item_date, 'YYYY-MM-DD'), li.created_at
FROM projects.line_items li
LEFT JOIN projects.categories c ON c.id = li.category_id
`

func scanItem(row pgx.Row) (Item, error) {
	var it Item
	var catID *int64
	var catName *string
	err := row.Scan(&it.ID, &it.ProjectID, &it.Kind, &catID, &catName, &it.Description,
		&it.CostCents, &it.PriceCents, &it.Hours, &it.ItemDate, &it.CreatedAt)
	if err != nil {
		return Item{}, err
	}
	if catID != nil && catName != nil {
		it.Category = &Ref{ID: *catID, Name: *catName}
	}
	return it, nil
}

// ItemInput holds the fields of a line item. Expenses need a category, given
// by id or by name (created when missing); labour has none.
type ItemInput struct {
	Kind         string   `json:"kind"`
	CategoryID   int64    `json:"category_id"`
	CategoryName string   `json:"category_name"`
	Description  string   `json:"description"`
	CostCents    int64    `json:"cost_cents"`
	PriceCents   int64    `json:"price_cents"`
	Hours        *float64 `json:"hours"`
	ItemDate     string   `json:"item_date"`
}

func resolveCategory(ctx context.Context, q querier, uid int64, in ItemInput) (*int64, error) {
	if in.Kind == KindLabor {
		return nil, nil
	}
	if in.CategoryID > 0 {
		return &in.CategoryID, categoryOwned(ctx, q, uid, in.CategoryID)
	}
	c, err := upsertCategory(ctx, q, uid, in.CategoryName)
	if err != nil {
		return nil, err
	}
	return &c.ID, nil
}

// CreateItem adds a line item and returns it with the updated project.
func (s *Store) CreateItem(ctx context.Context, uid, projectID int64, in ItemInput) (Item, ProjectDetail, error) {
	var (
		item Item
		d    ProjectDetail
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := projectOwned(ctx, tx, uid, projectID); err != nil {
			return err
		}
		catID, err := resolveCategory(ctx, tx, uid, in)
		if err != nil {
			return err
		}
		var id int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO projects.line_items
			    (project_id, user_id, kind, category_id, description, cost_cents, price_cents, hours, item_date)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::date)
			RETURNING id`,
			projectID, uid, in.Kind, catID, in.Description, in.CostCents, in.PriceCents, in.Hours, in.ItemDate).Scan(&id); err != nil {
			return err
		}
		if item, err = scanItem(tx.QueryRow(ctx, itemSelect+` WHERE li.id = $1`, id)); err != nil {
			return err
		}
		d, err = getProject(ctx, tx, uid, projectID)
		return err
	})
	return item, d, mapErr(err)
}

// UpdateItem replaces a line item and returns it with the updated project.
func (s *Store) UpdateItem(ctx context.Context, uid, projectID, itemID int64, in ItemInput) (Item, ProjectDetail, error) {
	var (
		item Item
		d    ProjectDetail
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		catID, err := resolveCategory(ctx, tx, uid, in)
		if err != nil {
			return err
		}
		if err := expectRow(tx.Exec(ctx, `
			UPDATE projects.line_items
			SET kind = $4, category_id = $5, description = $6, cost_cents = $7, price_cents = $8,
			    hours = $9, item_date = $10::date, updated_at = now()
			WHERE id = $3 AND project_id = $2 AND user_id = $1`,
			uid, projectID, itemID, in.Kind, catID, in.Description, in.CostCents, in.PriceCents, in.Hours, in.ItemDate)); err != nil {
			return err
		}
		if item, err = scanItem(tx.QueryRow(ctx, itemSelect+` WHERE li.id = $1`, itemID)); err != nil {
			return err
		}
		d, err = getProject(ctx, tx, uid, projectID)
		return err
	})
	return item, d, mapErr(err)
}

// DeleteItem removes a line item and returns the updated project.
func (s *Store) DeleteItem(ctx context.Context, uid, projectID, itemID int64) (ProjectDetail, error) {
	var d ProjectDetail
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := expectRow(tx.Exec(ctx, `DELETE FROM projects.line_items WHERE id = $3 AND project_id = $2 AND user_id = $1`,
			uid, projectID, itemID)); err != nil {
			return err
		}
		var err error
		d, err = getProject(ctx, tx, uid, projectID)
		return err
	})
	return d, mapErr(err)
}

// ---------------------------------------------------------------- payments

const paymentSelect = `
SELECT id, project_id, amount_cents, to_char(paid_on, 'YYYY-MM-DD'), note, created_at
FROM projects.payments
`

func scanPayment(row pgx.Row) (Payment, error) {
	var p Payment
	err := row.Scan(&p.ID, &p.ProjectID, &p.AmountCents, &p.PaidOn, &p.Note, &p.CreatedAt)
	return p, err
}

// PaymentInput holds the fields of a payment.
type PaymentInput struct {
	AmountCents int64  `json:"amount_cents"`
	PaidOn      string `json:"paid_on"`
	Note        string `json:"note"`
}

// CreatePayment records a payment and returns it with the updated project.
func (s *Store) CreatePayment(ctx context.Context, uid, projectID int64, in PaymentInput) (Payment, ProjectDetail, error) {
	var (
		pay Payment
		d   ProjectDetail
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := projectOwned(ctx, tx, uid, projectID); err != nil {
			return err
		}
		var err error
		if pay, err = scanPayment(tx.QueryRow(ctx, `
			INSERT INTO projects.payments (project_id, user_id, amount_cents, paid_on, note)
			VALUES ($1, $2, $3, $4::date, $5)
			RETURNING id, project_id, amount_cents, to_char(paid_on, 'YYYY-MM-DD'), note, created_at`,
			projectID, uid, in.AmountCents, in.PaidOn, in.Note)); err != nil {
			return err
		}
		d, err = getProject(ctx, tx, uid, projectID)
		return err
	})
	return pay, d, mapErr(err)
}

// UpdatePayment replaces a payment and returns it with the updated project.
func (s *Store) UpdatePayment(ctx context.Context, uid, projectID, paymentID int64, in PaymentInput) (Payment, ProjectDetail, error) {
	var (
		pay Payment
		d   ProjectDetail
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var err error
		if pay, err = scanPayment(tx.QueryRow(ctx, `
			UPDATE projects.payments
			SET amount_cents = $4, paid_on = $5::date, note = $6, updated_at = now()
			WHERE id = $3 AND project_id = $2 AND user_id = $1
			RETURNING id, project_id, amount_cents, to_char(paid_on, 'YYYY-MM-DD'), note, created_at`,
			uid, projectID, paymentID, in.AmountCents, in.PaidOn, in.Note)); err != nil {
			return err
		}
		d, err = getProject(ctx, tx, uid, projectID)
		return err
	})
	return pay, d, mapErr(err)
}

// DeletePayment removes a payment and returns the updated project.
func (s *Store) DeletePayment(ctx context.Context, uid, projectID, paymentID int64) (ProjectDetail, error) {
	var d ProjectDetail
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := expectRow(tx.Exec(ctx, `DELETE FROM projects.payments WHERE id = $3 AND project_id = $2 AND user_id = $1`,
			uid, projectID, paymentID)); err != nil {
			return err
		}
		var err error
		d, err = getProject(ctx, tx, uid, projectID)
		return err
	})
	return d, mapErr(err)
}
