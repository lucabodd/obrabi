-- Suggestions and bug reports sent by users, plus automatic error reports
-- (telemetry). The table doubles as the e-mail outbox: rows with
-- email_status = 'pending' are picked up by the mail worker.
CREATE TABLE feedback.reports (
    id              BIGSERIAL PRIMARY KEY,
    kind            TEXT        NOT NULL CHECK (kind IN ('suggestion', 'bug', 'error')),
    user_id         BIGINT,
    username        TEXT        NOT NULL DEFAULT '',
    source          TEXT        NOT NULL DEFAULT '',
    message         TEXT        NOT NULL,
    details         JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- Automatic errors with the same fingerprint seen again within a few
    -- hours only bump occurrences instead of sending another e-mail.
    fingerprint     TEXT,
    occurrences     INTEGER     NOT NULL DEFAULT 1,
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    email_status    TEXT        NOT NULL DEFAULT 'pending'
                    CHECK (email_status IN ('pending', 'sent', 'failed', 'skipped')),
    email_attempts  INTEGER     NOT NULL DEFAULT 0,
    email_error     TEXT        NOT NULL DEFAULT '',
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    emailed_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX reports_user_idx ON feedback.reports (user_id, created_at DESC);
CREATE INDEX reports_fingerprint_idx ON feedback.reports (fingerprint, last_seen_at DESC)
    WHERE fingerprint IS NOT NULL;
CREATE INDEX reports_outbox_idx ON feedback.reports (next_attempt_at)
    WHERE email_status = 'pending';
