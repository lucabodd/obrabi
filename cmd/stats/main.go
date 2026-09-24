// Command stats runs the Obrabi analytics service. It only reads the projects
// schema, with a database role that has SELECT privileges and nothing else.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/lucabodd/obrabi/internal/platform/config"
	"github.com/lucabodd/obrabi/internal/platform/database"
	"github.com/lucabodd/obrabi/internal/platform/httpx"
	"github.com/lucabodd/obrabi/internal/platform/logging"
	"github.com/lucabodd/obrabi/internal/platform/telemetry"
	"github.com/lucabodd/obrabi/internal/stats"
)

const service = "stats"

func main() {
	var env config.Env
	addr := env.String("OBRABI_ADDR", ":8083")
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
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

	db, err := database.Connect(context.Background(), dbURL, 60*time.Second, log)
	if err != nil {
		log.Error("database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	reporter := telemetry.New(feedbackURL, internalToken, service, log)
	r := httpx.NewEngine(service, log, reporter)
	r.Use(httpx.InternalOnly(internalToken))
	(&stats.Handlers{Store: stats.NewStore(db), Log: log, Loc: loc}).Register(r)

	if err := httpx.Run(addr, r, log); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
