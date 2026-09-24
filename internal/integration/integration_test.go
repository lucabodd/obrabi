// Package integration tests the SQL of the projects and stats services
// against a real PostgreSQL. It runs only when OBRABI_TEST_DATABASE_URL points
// to a database whose name contains "test": the projects schema there is
// dropped and recreated.
//
//	createdb obrabi_test
//	OBRABI_TEST_DATABASE_URL=postgres://localhost/obrabi_test go test ./internal/integration/
package integration_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lucabodd/obrabi/internal/platform/database"
	"github.com/lucabodd/obrabi/internal/projects"
	"github.com/lucabodd/obrabi/internal/projects/migrations"
	"github.com/lucabodd/obrabi/internal/stats"
)

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("OBRABI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("OBRABI_TEST_DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.ConnConfig.Database, "test") {
		t.Fatalf("refusing to run on database %q: its name must contain \"test\"", cfg.ConnConfig.Database)
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool, err := database.Connect(ctx, url, 10*time.Second, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS projects CASCADE"); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, pool, "projects", migrations.FS, log); err != nil {
		t.Fatal(err)
	}
	return pool
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func TestProjectsAndStats(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	store := projects.NewStore(pool)
	const nando, other = int64(1), int64(2)
	hours := 20.0

	// The example of the specification: a renovation in Rafelguaraf.
	p := must[projects.ProjectDetail](t)(store.CreateProject(ctx, nando, projects.ProjectInput{
		Name: "Reforma del bany", TownName: "Rafelguaraf", StartedOn: "2026-09-01",
	}))
	items := []projects.ItemInput{
		{Kind: projects.KindExpense, CategoryName: "Material", CostCents: 10000, PriceCents: 13000, ItemDate: "2026-09-02"},
		{Kind: projects.KindLabor, Hours: &hours, PriceCents: 30000, ItemDate: "2026-09-03"},
		{Kind: projects.KindExpense, CategoryName: "fontaneria", CostCents: 15000, PriceCents: 17000, ItemDate: "2026-08-20"},
	}
	for _, in := range items {
		_, d, err := store.CreateItem(ctx, nando, p.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		p = d
	}
	tot := p.Totals
	if tot.CostCents != 25000 || tot.PriceCents != 60000 || tot.BenefitCents != 35000 || tot.PendingCents != 60000 || tot.LaborHours != 20 {
		t.Fatalf("totals after items: %+v", tot)
	}
	_, p, err := store.CreatePayment(ctx, nando, p.ID, projects.PaymentInput{AmountCents: 20000, PaidOn: "2026-09-10", Note: "Bizum"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Totals.PendingCents != 40000 || p.Totals.BalanceCents != -5000 {
		t.Fatalf("totals after payment: %+v", p.Totals)
	}

	// Categories are per user and case-insensitive.
	c := must[projects.Ref](t)(store.CreateCategory(ctx, nando, "MATERIAL"))
	if c.Name != "Material" {
		t.Errorf("upsert returned %+v, want the existing Material", c)
	}
	if _, err := store.DeleteCategory(ctx, nando, c.ID, 0); !errors.Is(err, projects.ErrInUse) {
		t.Errorf("deleting a used category: %v", err)
	}
	dup := must[projects.Ref](t)(store.CreateCategory(ctx, nando, "Materials"))
	if _, err := store.DeleteCategory(ctx, nando, c.ID, dup.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	d := must[projects.ProjectDetail](t)(store.GetProject(ctx, nando, p.ID))
	merged := 0
	for _, it := range d.Items {
		if it.Category != nil && it.Category.ID == dup.ID {
			merged++
		}
	}
	if merged != 1 {
		t.Errorf("items moved by the merge: %d", merged)
	}

	// Another user sees nothing and can touch nothing.
	if _, err := store.GetProject(ctx, other, p.ID); !errors.Is(err, projects.ErrNotFound) {
		t.Errorf("other user reading: %v", err)
	}
	if _, _, err := store.CreateItem(ctx, other, p.ID, items[0]); !errors.Is(err, projects.ErrNotFound) {
		t.Errorf("other user writing: %v", err)
	}
	if list := must[[]projects.Project](t)(store.ListProjects(ctx, other, "")); len(list) != 0 {
		t.Errorf("other user lists %d projects", len(list))
	}

	// A town with projects cannot be deleted; finishing archives.
	if err := store.DeleteTown(ctx, nando, p.Town.ID); !errors.Is(err, projects.ErrInUse) {
		t.Errorf("deleting a town in use: %v", err)
	}
	fin := must[projects.ProjectDetail](t)(store.SetStatus(ctx, nando, p.ID, projects.StatusFinished))
	if fin.Status != projects.StatusFinished || fin.FinishedAt == nil {
		t.Errorf("finish: %+v", fin.Project)
	}
	if active := must[[]projects.Project](t)(store.ListProjects(ctx, nando, projects.StatusActive)); len(active) != 0 {
		t.Errorf("finished project still active")
	}

	// ---- stats (read side) over the same data.
	st := stats.NewStore(pool)
	aug := must[stats.Month](t)(stats.ParseMonth("2026-08"))
	sep := must[stats.Month](t)(stats.ParseMonth("2026-09"))
	oct := must[stats.Month](t)(stats.ParseMonth("2026-10"))

	months := must[[]stats.MonthFigures](t)(st.Monthly(ctx, nando, aug.Add(-1), oct))
	if len(months) != 4 {
		t.Fatalf("zero-filled months: %d", len(months))
	}
	byMonth := map[string]stats.MonthFigures{}
	for _, m := range months {
		byMonth[m.Month] = m
	}
	if m := byMonth["2026-09"]; m.RevenueCents != 43000 || m.CostCents != 10000 || m.BenefitCents != 33000 || m.CollectedCents != 20000 || m.LaborHours != 20 {
		t.Errorf("September: %+v", m)
	}
	if m := byMonth["2026-08"]; m.RevenueCents != 17000 || m.BenefitCents != 2000 {
		t.Errorf("August: %+v", m)
	}
	if m := byMonth["2026-07"]; m.RevenueCents != 0 || m.Month != "2026-07" {
		t.Errorf("empty July: %+v", m)
	}

	cats := must[[]stats.CategoryFigures](t)(st.Categories(ctx, nando, aug, sep))
	var labor *stats.CategoryFigures
	for i := range cats {
		if cats[i].Kind == "labor" {
			labor = &cats[i]
		}
	}
	if labor == nil || labor.Name != "Mà d'obra" || labor.BenefitCents != 30000 || labor.ID != nil {
		t.Errorf("labour bucket: %+v", labor)
	}
	if cats[0].Kind != "labor" {
		t.Errorf("categories not sorted by benefit: %+v", cats)
	}

	fig := must[stats.Figures](t)(st.Period(ctx, nando, sep.First(), sep.Last()))
	if fig.BenefitCents != 33000 || fig.CollectedCents != 20000 {
		t.Errorf("period September: %+v", fig)
	}
	out := must[stats.Outstanding](t)(st.Outstanding(ctx, nando))
	if out.PendingCents != 40000 || out.ProjectsWithPending != 1 || out.ActiveProjects != 0 {
		t.Errorf("outstanding (finished projects still owe money): %+v", out)
	}
	if o := must[stats.Outstanding](t)(st.Outstanding(ctx, other)); o.PendingCents != 0 {
		t.Errorf("other user's outstanding: %+v", o)
	}

	// Deleting the project removes its items and payments.
	if err := store.DeleteProject(ctx, nando, p.ID); err != nil {
		t.Fatal(err)
	}
	if f := must[stats.Figures](t)(st.Period(ctx, nando, aug.First(), sep.Last())); f.RevenueCents != 0 || f.CollectedCents != 0 {
		t.Errorf("figures after delete: %+v", f)
	}
}
