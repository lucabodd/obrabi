-- Core domain of Obrabi. Every row carries the owning user id (from the auth
-- service) and every query filters on it.

-- Pobles: towns used to group the projects.
CREATE TABLE projects.towns (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      NOT NULL,
    name       TEXT        NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX towns_user_name_key ON projects.towns (user_id, lower(name));

-- Categories de despesa: created on the fly while adding line items.
CREATE TABLE projects.categories (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      NOT NULL,
    name       TEXT        NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 60),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX categories_user_name_key ON projects.categories (user_id, lower(name));

-- Obres.
CREATE TABLE projects.projects (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT      NOT NULL,
    town_id      BIGINT      NOT NULL REFERENCES projects.towns (id),
    name         TEXT        NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    client_name  TEXT        NOT NULL DEFAULT '',
    client_phone TEXT        NOT NULL DEFAULT '',
    address      TEXT        NOT NULL DEFAULT '',
    notes        TEXT        NOT NULL DEFAULT '',
    status       TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'finished')),
    started_on   DATE        NOT NULL DEFAULT CURRENT_DATE,
    finished_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT projects_finished_at_matches_status CHECK ((status = 'finished') = (finished_at IS NOT NULL))
);
CREATE INDEX projects_user_status_idx ON projects.projects (user_id, status);
CREATE INDEX projects_town_idx ON projects.projects (town_id);

-- Partides (line items). kind = 'expense' is material or any other cost with
-- an optional mark-up to the client; kind = 'labor' is Nando's own work
-- (hours + price) and has no category.
--   benefit of an item = price_cents - cost_cents
CREATE TABLE projects.line_items (
    id          BIGSERIAL PRIMARY KEY,
    project_id  BIGINT       NOT NULL REFERENCES projects.projects (id) ON DELETE CASCADE,
    user_id     BIGINT       NOT NULL,
    kind        TEXT         NOT NULL CHECK (kind IN ('expense', 'labor')),
    category_id BIGINT       REFERENCES projects.categories (id),
    description TEXT         NOT NULL DEFAULT '',
    cost_cents  BIGINT       NOT NULL DEFAULT 0 CHECK (cost_cents >= 0),
    price_cents BIGINT       NOT NULL DEFAULT 0 CHECK (price_cents >= 0),
    hours       NUMERIC(8,2) CHECK (hours IS NULL OR hours >= 0),
    item_date   DATE         NOT NULL DEFAULT CURRENT_DATE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT line_items_expense_has_category CHECK (kind = 'labor' OR category_id IS NOT NULL),
    CONSTRAINT line_items_labor_has_no_category CHECK (kind = 'expense' OR category_id IS NULL)
);
CREATE INDEX line_items_project_idx ON projects.line_items (project_id);
CREATE INDEX line_items_user_date_idx ON projects.line_items (user_id, item_date);
CREATE INDEX line_items_category_idx ON projects.line_items (category_id);

-- Cobraments: money received from the client; they reduce what is pending.
CREATE TABLE projects.payments (
    id           BIGSERIAL PRIMARY KEY,
    project_id   BIGINT      NOT NULL REFERENCES projects.projects (id) ON DELETE CASCADE,
    user_id      BIGINT      NOT NULL,
    amount_cents BIGINT      NOT NULL CHECK (amount_cents > 0),
    paid_on      DATE        NOT NULL DEFAULT CURRENT_DATE,
    note         TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX payments_project_idx ON projects.payments (project_id);
CREATE INDEX payments_user_date_idx ON projects.payments (user_id, paid_on);

-- Keep projects.updated_at as "last activity", so lists can show the projects
-- Nando touched most recently first.
CREATE FUNCTION projects.touch_project() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        UPDATE projects.projects SET updated_at = now() WHERE id = OLD.project_id;
    ELSE
        UPDATE projects.projects SET updated_at = now() WHERE id = NEW.project_id;
    END IF;
    RETURN NULL;
END
$$;

CREATE TRIGGER line_items_touch_project
    AFTER INSERT OR UPDATE OR DELETE ON projects.line_items
    FOR EACH ROW EXECUTE FUNCTION projects.touch_project();

CREATE TRIGGER payments_touch_project
    AFTER INSERT OR UPDATE OR DELETE ON projects.payments
    FOR EACH ROW EXECUTE FUNCTION projects.touch_project();
