// Command projects runs the Obrabi projects service: towns, projects, line
// items, categories, client payments and PDF export.
//
//	projects                          serve the HTTP API (default)
//	projects healthcheck              probe the running server
//	projects seed-demo <user-id>      load demo data into an empty account
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/lucabodd/obrabi/internal/platform/config"
	"github.com/lucabodd/obrabi/internal/platform/database"
	"github.com/lucabodd/obrabi/internal/platform/httpx"
	"github.com/lucabodd/obrabi/internal/platform/logging"
	"github.com/lucabodd/obrabi/internal/platform/telemetry"
	"github.com/lucabodd/obrabi/internal/projects"
	"github.com/lucabodd/obrabi/internal/projects/migrations"
)

const service = "projects"

func main() {
	var env config.Env
	addr := env.String("OBRABI_ADDR", ":8082")

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "healthcheck" {
		os.Exit(httpx.Healthcheck(addr))
	}

	log := logging.New(service)
	dbURL := env.Required("DATABASE_URL")
	internalToken := env.Secret("OBRABI_INTERNAL_TOKEN", 16)
	feedbackURL := env.String("OBRABI_FEEDBACK_URL", "")
	loc := env.Location("OBRABI_TIMEZONE", "Europe/Madrid")
	if err := env.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx := context.Background()
	db, err := database.Connect(ctx, dbURL, 60*time.Second, log)
	if err != nil {
		log.Error("database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db, "projects", migrations.FS, log); err != nil {
		log.Error("migrations", "err", err)
		os.Exit(1)
	}
	store := projects.NewStore(db)

	if len(args) > 0 && args[0] == "seed-demo" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: projects seed-demo <user-id>   (see `auth user list`)")
			os.Exit(2)
		}
		uid, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || uid <= 0 {
			fmt.Fprintln(os.Stderr, "invalid user id:", args[1])
			os.Exit(2)
		}
		n, err := projects.SeedDemo(ctx, store, uid, time.Now().In(loc))
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Printf("%d obres de demostració creades per a l'usuari %d\n", n, uid)
		return
	}

	reporter := telemetry.New(feedbackURL, internalToken, service, log)
	r := httpx.NewEngine(service, log, reporter)
	r.Use(httpx.InternalOnly(internalToken))
	(&projects.Handlers{Store: store, Log: log, Loc: loc}).Register(r)

	if err := httpx.Run(addr, r, log); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
