#!/usr/bin/env bash
# Runs the five services locally (built with the local Go toolchain) against
# the PostgreSQL of docker compose. Used by `make dev`; Ctrl-C stops all.
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ ! -f .env ]]; then
	echo "Missing .env: run ./scripts/gen-env.sh first." >&2
	exit 1
fi
set -a
# shellcheck disable=SC1091
source .env
set +a

docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d db
until docker compose exec -T db pg_isready -U obrabi_admin -d obrabi >/dev/null 2>&1; do sleep 1; done

mkdir -p bin
for svc in gateway auth projects stats feedback; do
	go build -o "bin/$svc" "./cmd/$svc"
done

export OBRABI_ENV=development OBRABI_LOG_FORMAT=text
export OBRABI_FEEDBACK_URL=http://127.0.0.1:8084
export OBRABI_AUTH_URL=http://127.0.0.1:8081 OBRABI_PROJECTS_URL=http://127.0.0.1:8082 OBRABI_STATS_URL=http://127.0.0.1:8083
dsn() { echo "postgres://$1:$2@127.0.0.1:${OBRABI_DEV_DB_PORT:-5432}/obrabi?sslmode=disable"; }

pids=()
cleanup() { kill "${pids[@]}" 2>/dev/null || true; }
trap cleanup EXIT INT TERM
run() {
	local name=$1
	shift
	("$@" 2>&1 | sed -u "s/^/[$name] /") &
	pids+=($!)
}

OBRABI_ADDR=:8084 DATABASE_URL="$(dsn obrabi_feedback "$FEEDBACK_DB_PASSWORD")" run feedback bin/feedback
OBRABI_ADDR=:8081 DATABASE_URL="$(dsn obrabi_auth "$AUTH_DB_PASSWORD")" run auth bin/auth
OBRABI_ADDR=:8082 DATABASE_URL="$(dsn obrabi_projects "$PROJECTS_DB_PASSWORD")" run projects bin/projects
sleep 2 # stats reads the tables created by the projects service
OBRABI_ADDR=:8083 DATABASE_URL="$(dsn obrabi_stats "$STATS_DB_PASSWORD")" run stats bin/stats
OBRABI_ADDR=:8080 run gateway bin/gateway

echo "Obrabi: http://localhost:8080  (Ctrl-C per aturar)"
wait
