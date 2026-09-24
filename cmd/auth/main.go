// Command auth runs the Obrabi identity service and its admin CLI.
//
//	auth                                  serve the HTTP API (default)
//	auth healthcheck                      probe the running server
//	auth user list                        list users
//	auth user add <username> [name]       create a user with a random password
//	auth user reset-password <username>   print a new random password
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lucabodd/obrabi/internal/auth"
	"github.com/lucabodd/obrabi/internal/auth/migrations"
	"github.com/lucabodd/obrabi/internal/platform/config"
	"github.com/lucabodd/obrabi/internal/platform/database"
	"github.com/lucabodd/obrabi/internal/platform/httpx"
	"github.com/lucabodd/obrabi/internal/platform/logging"
	"github.com/lucabodd/obrabi/internal/platform/telemetry"
)

const service = "auth"

func main() {
	var env config.Env
	addr := env.String("OBRABI_ADDR", ":8081")

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "healthcheck" {
		os.Exit(httpx.Healthcheck(addr))
	}

	log := logging.New(service)
	dbURL := env.Required("DATABASE_URL")
	internalToken := env.Secret("OBRABI_INTERNAL_TOKEN", 16)
	feedbackURL := env.String("OBRABI_FEEDBACK_URL", "")
	bootstrapUsers := env.String("OBRABI_BOOTSTRAP_USERS", "nando")
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
	if err := database.Migrate(ctx, db, "auth", migrations.FS, log); err != nil {
		log.Error("migrations", "err", err)
		os.Exit(1)
	}
	store := auth.NewStore(db)

	if len(args) > 0 && args[0] != "serve" {
		if err := runCLI(ctx, store, args); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if err := auth.EnsureUsers(ctx, store, strings.Split(bootstrapUsers, ","), os.Stdout, log); err != nil {
		log.Error("bootstrap users", "err", err)
		os.Exit(1)
	}

	reporter := telemetry.New(feedbackURL, internalToken, service, log)
	r := httpx.NewEngine(service, log, reporter)
	r.Use(httpx.InternalOnly(internalToken))
	(&auth.Handlers{Store: store, Log: log}).Register(r)

	if err := httpx.Run(addr, r, log); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

func runCLI(ctx context.Context, store *auth.Store, args []string) error {
	usage := errors.New("usage: auth user list | auth user add <username> [display name] | auth user reset-password <username>")
	if len(args) < 2 || args[0] != "user" {
		return usage
	}
	switch args[1] {
	case "list":
		users, err := store.List(ctx)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tUSUARI\tNOM\tCREAT\tÚLTIM ACCÉS")
		for _, u := range users {
			last := "-"
			if u.LastLoginAt != nil {
				last = u.LastLoginAt.Format("2006-01-02 15:04")
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", u.ID, u.Username, u.DisplayName, u.CreatedAt.Format("2006-01-02"), last)
		}
		return w.Flush()
	case "add":
		if len(args) < 3 {
			return usage
		}
		username, err := auth.NormalizeUsername(args[2])
		if err != nil {
			return fmt.Errorf("%q: use 2-32 lowercase letters, digits, '.', '_' or '-'", args[2])
		}
		display := auth.DefaultDisplayName(username)
		if len(args) > 3 {
			display = strings.Join(args[3:], " ")
		}
		password, _, err := auth.CreateWithRandomPassword(ctx, store, username, display)
		if err != nil {
			return err
		}
		auth.PrintCredentials(os.Stdout, "Usuari creat", username, password)
		return nil
	case "reset-password":
		if len(args) < 3 {
			return usage
		}
		username, err := auth.NormalizeUsername(args[2])
		if err != nil {
			return err
		}
		password, err := auth.ResetPassword(ctx, store, username)
		if err != nil {
			return err
		}
		auth.PrintCredentials(os.Stdout, "Contrasenya restablida", username, password)
		return nil
	}
	return usage
}
