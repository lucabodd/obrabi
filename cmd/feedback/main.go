// Command feedback runs the Obrabi feedback service: suggestions and bug
// reports from users, automatic error reports from the other services and the
// browser, and the e-mails that notify the maintainer.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lucabodd/obrabi/internal/feedback"
	"github.com/lucabodd/obrabi/internal/feedback/migrations"
	"github.com/lucabodd/obrabi/internal/platform/config"
	"github.com/lucabodd/obrabi/internal/platform/database"
	"github.com/lucabodd/obrabi/internal/platform/httpx"
	"github.com/lucabodd/obrabi/internal/platform/logging"
)

const service = "feedback"

func main() {
	var env config.Env
	addr := env.String("OBRABI_ADDR", ":8084")
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(httpx.Healthcheck(addr))
	}

	log := logging.New(service)
	dbURL := env.Required("DATABASE_URL")
	internalToken := env.Secret("OBRABI_INTERNAL_TOKEN", 16)
	loc := env.Location("OBRABI_TIMEZONE", "Europe/Madrid")
	smtpCfg := feedback.SMTPConfig{
		Host:     env.String("SMTP_HOST", ""),
		Port:     env.Int("SMTP_PORT", 587),
		Username: env.String("SMTP_USERNAME", ""),
		Password: env.String("SMTP_PASSWORD", ""),
		From:     env.String("SMTP_FROM", env.String("SMTP_USERNAME", "")),
	}
	for _, to := range strings.Split(env.String("OBRABI_REPORT_EMAIL_TO", ""), ",") {
		if to = strings.TrimSpace(to); to != "" {
			smtpCfg.To = append(smtpCfg.To, to)
		}
	}
	if err := env.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	var mailer feedback.Mailer
	if smtpCfg.Enabled() {
		if err := smtpCfg.Validate(); err != nil {
			fmt.Fprintln(os.Stderr, "invalid e-mail configuration:", err)
			os.Exit(2)
		}
		mailer = feedback.NewSMTPMailer(smtpCfg)
		log.Info("e-mail notifications enabled", "smtp", smtpCfg.Host, "to", strings.Join(smtpCfg.To, ","))
	} else {
		log.Warn("e-mail notifications disabled: set SMTP_HOST, SMTP_USERNAME, SMTP_PASSWORD and OBRABI_REPORT_EMAIL_TO")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Connect(ctx, dbURL, 60*time.Second, log)
	if err != nil {
		log.Error("database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db, "feedback", migrations.FS, log); err != nil {
		log.Error("migrations", "err", err)
		os.Exit(1)
	}

	store := feedback.NewStore(db)
	worker := feedback.NewWorker(store, mailer, log, loc)
	go worker.Run(ctx)

	handlers := feedback.NewHandlers(store, worker, log)
	r := httpx.NewEngine(service, log, handlers.Reporter())
	r.Use(httpx.InternalOnly(internalToken))
	handlers.Register(r)

	if err := httpx.Run(addr, r, log); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
