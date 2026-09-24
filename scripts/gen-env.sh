#!/usr/bin/env bash
# Creates .env from .env.example, filling every empty secret with a random
# value. Refuses to overwrite an existing .env unless --force is given
# (changing DB passwords after the first start requires resetting them in
# PostgreSQL too).
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ -f .env && "${1:-}" != "--force" ]]; then
	echo ".env already exists; use --force to overwrite it." >&2
	exit 1
fi

rand() {
	# 32 random bytes as hex (64 chars).
	if command -v openssl >/dev/null 2>&1; then
		openssl rand -hex 32
	else
		head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
	fi
}

secrets=(OBRABI_SESSION_SECRET OBRABI_INTERNAL_TOKEN POSTGRES_PASSWORD AUTH_DB_PASSWORD PROJECTS_DB_PASSWORD STATS_DB_PASSWORD FEEDBACK_DB_PASSWORD)
umask 077
cp .env.example .env.tmp
for key in "${secrets[@]}"; do
	value="$(rand)"
	sed -i.bak "s|^${key}=\$|${key}=${value}|" .env.tmp
done
rm -f .env.tmp.bak
mv .env.tmp .env
chmod 600 .env
echo "Created .env with random secrets."
echo "Still to fill in by hand: SMTP_USERNAME and SMTP_PASSWORD (Google App Password)."
