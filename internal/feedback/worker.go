package feedback

import (
	"context"
	"log/slog"
	"time"
)

// Worker drains the e-mail outbox (pending reports) in the background, with
// retries and exponential-ish backoff. Reports survive restarts because the
// outbox is the reports table itself.
type Worker struct {
	store  *Store
	mailer Mailer // nil: e-mail disabled, reports are marked "skipped"
	log    *slog.Logger
	loc    *time.Location
	wake   chan struct{}
}

func NewWorker(store *Store, mailer Mailer, log *slog.Logger, loc *time.Location) *Worker {
	return &Worker{store: store, mailer: mailer, log: log, loc: loc, wake: make(chan struct{}, 1)}
}

// Wake asks the worker to look at the outbox now.
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run loops until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		w.drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-w.wake:
		}
	}
}

func (w *Worker) drain(ctx context.Context) {
	for ctx.Err() == nil {
		batch, err := w.store.ClaimOutbox(ctx, 10, 5*time.Minute)
		if err != nil {
			w.log.Error("claim outbox", "err", err)
			return
		}
		if len(batch) == 0 {
			return
		}
		for _, r := range batch {
			w.deliver(ctx, r)
		}
	}
}

func (w *Worker) deliver(ctx context.Context, r Report) {
	if w.mailer == nil {
		if err := w.store.MarkSkipped(ctx, r.ID); err != nil {
			w.log.Error("mark skipped", "report_id", r.ID, "err", err)
		}
		w.log.Info("report stored (e-mail not configured)", "report_id", r.ID, "kind", r.Kind)
		return
	}
	subject, body := composeEmail(r, w.loc)
	sendCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	err := w.mailer.Send(sendCtx, subject, body)
	cancel()
	if err == nil {
		if err := w.store.MarkSent(ctx, r.ID); err != nil {
			w.log.Error("mark sent", "report_id", r.ID, "err", err)
		}
		w.log.Info("report e-mailed", "report_id", r.ID, "kind", r.Kind)
		return
	}
	retry := backoff(r.EmailAttempts)
	w.log.Warn("report e-mail failed", "report_id", r.ID, "attempt", r.EmailAttempts, "retry_in", retry.String(), "err", err)
	if err := w.store.MarkFailed(ctx, r.ID, err.Error(), retry); err != nil {
		w.log.Error("mark failed", "report_id", r.ID, "err", err)
	}
}

// backoff returns the delay before the next attempt, or 0 to give up.
func backoff(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 30 * time.Minute
	case 4:
		return 2 * time.Hour
	case 5:
		return 6 * time.Hour
	}
	return 0
}
