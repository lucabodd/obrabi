// Command gateway runs the public entry point of Obrabi: the web app, login
// sessions and the reverse proxy to the internal services.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lucabodd/obrabi/internal/gateway"
	"github.com/lucabodd/obrabi/internal/platform/buildinfo"
	"github.com/lucabodd/obrabi/internal/platform/config"
	"github.com/lucabodd/obrabi/internal/platform/httpx"
	"github.com/lucabodd/obrabi/internal/platform/logging"
	"github.com/lucabodd/obrabi/internal/platform/telemetry"
	"github.com/lucabodd/obrabi/web"
)

const service = "gateway"

func main() {
	var env config.Env
	addr := env.String("OBRABI_ADDR", ":8080")
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(httpx.Healthcheck(addr))
	}

	log := logging.New(service)
	internalToken := env.Secret("OBRABI_INTERNAL_TOKEN", 16)
	cookieSecurity, err := gateway.ParseCookieSecurity(env.String("OBRABI_COOKIE_SECURE", "auto"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "OBRABI_COOKIE_SECURE:", err)
		os.Exit(2)
	}
	cfg := gateway.Config{
		AuthURL:       env.String("OBRABI_AUTH_URL", "http://auth:8081"),
		ProjectsURL:   env.String("OBRABI_PROJECTS_URL", "http://projects:8082"),
		StatsURL:      env.String("OBRABI_STATS_URL", "http://stats:8083"),
		FeedbackURL:   env.String("OBRABI_FEEDBACK_URL", "http://feedback:8084"),
		InternalToken: internalToken,
		Sessions: gateway.NewSessions(
			env.Secret("OBRABI_SESSION_SECRET", 32),
			env.Duration("OBRABI_SESSION_TTL", 60*24*time.Hour),
			cookieSecurity,
		),
	}
	for _, p := range strings.Split(env.String("OBRABI_TRUSTED_PROXIES", ""), ",") {
		if p = strings.TrimSpace(p); p != "" {
			cfg.TrustedProxies = append(cfg.TrustedProxies, p)
		}
	}
	if err := env.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	static, err := gateway.NewStatic(web.Static())
	if err != nil {
		log.Error("load web app", "err", err)
		os.Exit(1)
	}
	reporter := telemetry.New(cfg.FeedbackURL, internalToken, service, log)
	g, err := gateway.New(cfg, static, log, reporter)
	if err != nil {
		log.Error("configure gateway", "err", err)
		os.Exit(1)
	}
	handler, err := g.Handler()
	if err != nil {
		log.Error("configure gateway", "err", err)
		os.Exit(1)
	}
	log.Info("starting", "version", buildinfo.Version, "web_version", static.Version, "trusted_proxies", cfg.TrustedProxies)
	if err := httpx.Run(addr, handler, log); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
