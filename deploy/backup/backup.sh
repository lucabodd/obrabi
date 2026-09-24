#!/bin/sh
# Periodic logical backups of the Obrabi database (pg_dump, custom format).
# Runs in the "backup" service of docker-compose.yml; the files land in
# ./backups on the host. Restore: see README.md, section "Backup".
set -u

INTERVAL_HOURS="${BACKUP_INTERVAL_HOURS:-24}"
KEEP_DAYS="${BACKUP_KEEP_DAYS:-30}"
mkdir -p /backups

backup() {
	name="/backups/obrabi-$(date +%Y%m%d-%H%M%S).dump"
	if pg_dump --format=custom --file="$name.partial"; then
		mv "$name.partial" "$name"
		echo "$(date -Iseconds) backup ok: $name ($(du -h "$name" | cut -f1))"
	else
		rm -f "$name.partial"
		echo "$(date -Iseconds) backup FAILED" >&2
	fi
	find /backups -name 'obrabi-*.dump' -type f -mtime "+$KEEP_DAYS" -delete
}

# On a fresh install give the services time to create their schemas.
sleep 60
while true; do
	backup
	sleep "$((INTERVAL_HOURS * 3600))"
done
