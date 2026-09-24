// Package feedback implements the feedback service: suggestions and bug
// reports from users, automatic error reports (telemetry) from the other
// services and the browser, and the e-mails that notify the maintainer.
package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Report kinds.
const (
	KindSuggestion = "suggestion" // "Suggeriment" in the UI
	KindBug        = "bug"        // problem reported by the user
	KindError      = "error"      // automatic telemetry
)

// Email delivery states.
const (
	EmailPending = "pending"
	EmailSent    = "sent"
	EmailFailed  = "failed"
	EmailSkipped = "skipped"
)

// Report is one row of feedback.reports.
type Report struct {
	ID            int64          `json:"id"`
	Kind          string         `json:"kind"`
	UserID        *int64         `json:"-"`
	Username      string         `json:"-"`
	Source        string         `json:"-"`
	Message       string         `json:"message"`
	Details       map[string]any `json:"-"`
	Occurrences   int            `json:"-"`
	EmailStatus   string         `json:"email_status"`
	EmailAttempts int            `json:"-"`
	CreatedAt     time.Time      `json:"created_at"`
	LastSeenAt    time.Time      `json:"-"`
}

// Store persists reports.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func marshalDetails(d map[string]any) ([]byte, error) {
	if d == nil {
		d = map[string]any{}
	}
	return json.Marshal(d)
}

// Create stores a suggestion or bug report sent by a user.
func (s *Store) Create(ctx context.Context, r Report) (Report, error) {
	details, err := marshalDetails(r.Details)
	if err != nil {
		return Report{}, err
	}
	err = s.db.QueryRow(ctx, `
		INSERT INTO feedback.reports (kind, user_id, username, source, message, details)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, email_status, created_at, last_seen_at`,
		r.Kind, r.UserID, r.Username, r.Source, r.Message, details,
	).Scan(&r.ID, &r.EmailStatus, &r.CreatedAt, &r.LastSeenAt)
	r.Occurrences = 1
	return r, err
}

// RecordError stores an automatic error report. If the same fingerprint was
// seen within window the existing report is bumped instead and isNew is
// false, so that a recurring error produces a single e-mail.
func (s *Store) RecordError(ctx context.Context, r Report, fingerprint string, window time.Duration) (id int64, isNew bool, err error) {
	details, err := marshalDetails(r.Details)
	if err != nil {
		return 0, false, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// Serialise concurrent reports of the same error.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, fingerprint); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `
			UPDATE feedback.reports
			SET occurrences = occurrences + 1, last_seen_at = now()
			WHERE id = (
			    SELECT id FROM feedback.reports
			    WHERE fingerprint = $1 AND last_seen_at > now() - make_interval(secs => $2)
			    ORDER BY last_seen_at DESC
			    LIMIT 1
			)
			RETURNING id`, fingerprint, window.Seconds()).Scan(&id)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		isNew = true
		return tx.QueryRow(ctx, `
			INSERT INTO feedback.reports (kind, user_id, username, source, message, details, fingerprint)
			VALUES ('error', $1, $2, $3, $4, $5, $6)
			RETURNING id`,
			r.UserID, r.Username, r.Source, r.Message, details, fingerprint).Scan(&id)
	})
	return id, isNew, err
}

// ListByUser returns the suggestions and bug reports written by a user.
func (s *Store) ListByUser(ctx context.Context, uid int64, limit int) ([]Report, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, kind, message, email_status, created_at
		FROM feedback.reports
		WHERE user_id = $1 AND kind IN ('suggestion', 'bug')
		ORDER BY created_at DESC
		LIMIT $2`, uid, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Report, error) {
		var r Report
		err := row.Scan(&r.ID, &r.Kind, &r.Message, &r.EmailStatus, &r.CreatedAt)
		return r, err
	})
}

// ClaimOutbox leases up to limit reports whose e-mail is due. The lease
// (next_attempt_at pushed forward) keeps other workers away while sending.
func (s *Store) ClaimOutbox(ctx context.Context, limit int, lease time.Duration) ([]Report, error) {
	rows, err := s.db.Query(ctx, `
		UPDATE feedback.reports
		SET next_attempt_at = now() + make_interval(secs => $2), email_attempts = email_attempts + 1
		WHERE id IN (
		    SELECT id FROM feedback.reports
		    WHERE email_status = 'pending' AND next_attempt_at <= now()
		    ORDER BY id
		    LIMIT $1
		    FOR UPDATE SKIP LOCKED
		)
		RETURNING id, kind, user_id, username, source, message, details, occurrences, email_attempts, created_at, last_seen_at`,
		limit, lease.Seconds())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Report, error) {
		var r Report
		var details []byte
		err := row.Scan(&r.ID, &r.Kind, &r.UserID, &r.Username, &r.Source, &r.Message, &details,
			&r.Occurrences, &r.EmailAttempts, &r.CreatedAt, &r.LastSeenAt)
		if err == nil && len(details) > 0 {
			_ = json.Unmarshal(details, &r.Details)
		}
		return r, err
	})
}

// MarkSent records a delivered e-mail.
func (s *Store) MarkSent(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `
		UPDATE feedback.reports SET email_status = 'sent', emailed_at = now(), email_error = '' WHERE id = $1`, id)
	return err
}

// MarkSkipped records that no e-mail will be sent (SMTP not configured).
func (s *Store) MarkSkipped(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `UPDATE feedback.reports SET email_status = 'skipped' WHERE id = $1`, id)
	return err
}

// MarkFailed records a failed attempt; with retryAfter > 0 the e-mail is
// retried later, otherwise it is given up.
func (s *Store) MarkFailed(ctx context.Context, id int64, cause string, retryAfter time.Duration) error {
	if len(cause) > 1000 {
		cause = cause[:1000]
	}
	if retryAfter > 0 {
		_, err := s.db.Exec(ctx, `
			UPDATE feedback.reports
			SET email_error = $2, next_attempt_at = now() + make_interval(secs => $3)
			WHERE id = $1`, id, cause, retryAfter.Seconds())
		return err
	}
	_, err := s.db.Exec(ctx, `
		UPDATE feedback.reports SET email_status = 'failed', email_error = $2 WHERE id = $1`, id, cause)
	return err
}
