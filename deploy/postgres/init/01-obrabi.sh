#!/bin/bash
# Creates one database role and one schema per Obrabi service.
#
# Run automatically by the postgres image the first time its data volume is
# initialised (/docker-entrypoint-initdb.d). Each service can only write its
# own schema; the stats service can only read the projects schema.
set -euo pipefail

: "${POSTGRES_USER:?}" "${POSTGRES_DB:?}"
: "${AUTH_DB_PASSWORD:?AUTH_DB_PASSWORD is required}"
: "${PROJECTS_DB_PASSWORD:?PROJECTS_DB_PASSWORD is required}"
: "${STATS_DB_PASSWORD:?STATS_DB_PASSWORD is required}"
: "${FEEDBACK_DB_PASSWORD:?FEEDBACK_DB_PASSWORD is required}"

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
	-v db="$POSTGRES_DB" \
	-v auth_pw="$AUTH_DB_PASSWORD" \
	-v projects_pw="$PROJECTS_DB_PASSWORD" \
	-v stats_pw="$STATS_DB_PASSWORD" \
	-v feedback_pw="$FEEDBACK_DB_PASSWORD" <<'EOSQL'
REVOKE ALL ON DATABASE :"db" FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;

CREATE ROLE obrabi_auth     LOGIN PASSWORD :'auth_pw';
CREATE ROLE obrabi_projects LOGIN PASSWORD :'projects_pw';
CREATE ROLE obrabi_stats    LOGIN PASSWORD :'stats_pw';
CREATE ROLE obrabi_feedback LOGIN PASSWORD :'feedback_pw';

GRANT CONNECT ON DATABASE :"db" TO obrabi_auth, obrabi_projects, obrabi_stats, obrabi_feedback;

CREATE SCHEMA auth     AUTHORIZATION obrabi_auth;
CREATE SCHEMA projects AUTHORIZATION obrabi_projects;
CREATE SCHEMA feedback AUTHORIZATION obrabi_feedback;

-- stats: read-only access to the projects schema, future tables included.
GRANT USAGE ON SCHEMA projects TO obrabi_stats;
ALTER DEFAULT PRIVILEGES FOR ROLE obrabi_projects IN SCHEMA projects
    GRANT SELECT ON TABLES TO obrabi_stats;

ALTER ROLE obrabi_auth     SET search_path = auth;
ALTER ROLE obrabi_projects SET search_path = projects;
ALTER ROLE obrabi_stats    SET search_path = projects;
ALTER ROLE obrabi_feedback SET search_path = feedback;
EOSQL

echo "obrabi: roles and schemas created"
