-- Users of Obrabi. Every other entity (towns, projects, ...) is owned by a
-- user id coming from this table, but lives in other services' schemas, so
-- there are deliberately no cross-schema foreign keys.
CREATE TABLE auth.users (
    id                  BIGSERIAL PRIMARY KEY,
    username            TEXT        NOT NULL CHECK (username ~ '^[a-z0-9._-]{2,32}$'),
    display_name        TEXT        NOT NULL DEFAULT '',
    password_hash       TEXT        NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    password_changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at       TIMESTAMPTZ
);

CREATE UNIQUE INDEX users_username_key ON auth.users (username);
