// Package worker реализует фоновую обработку задач на обновление котировок.
// Очередь живёт в Postgres: несколько инстансов сервиса могут запускать
// воркер одновременно — SELECT ... FOR UPDATE SKIP LOCKED гарантирует, что
// конкретную задачу возьмёт в работу только один из них, а при падении
// инстанса задача просто останется в БД со статусом processing/pending для
// последующей обработки (см. README про доработку зависших processing).
package worker

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"currency-quotes/internal/domain"
	"currency-quotes/internal/provider"
	"currency-quotes/internal/storage"
)

// maxAttempts зарезервировано под ретраи с backoff — см. TODO в processOnce.
const maxAttempts = 5

type Worker struct {
	jobs     *storage.JobsRepo
	quotes   *storage.QuotesRepo
	provider provider.RateProvider
	interval time.Duration
	log      *slog.Logger
}

func New(jobs *storage.JobsRepo, quotes *storage.QuotesRepo, p provider.RateProvider, interval time.Duration, log *slog.Logger) *Worker {
	return &Worker{jobs: jobs, quotes: quotes, provider: p, interval: interval, log: log}
}

// Run — бесконечный цикл поллинга до отмены контекста. В cmd/service
// запускается несколько горутин Run() параллельно (см. WorkerCount).
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processOnce(ctx)
		}
	}
}

func (w *Worker) processOnce(ctx context.Context) {
	job, err := w.jobs.ClaimNextPending(ctx)
	if err != nil {
		w.log.Error("claim job failed", "err", err)
		return
	}
	if job == nil {
		return // очередь пуста
	}

	base, quote, ok := splitPair(job.Pair)
	if !ok {
		_ = w.jobs.MarkFailed(ctx, job.ID, "invalid pair stored in job")
		return
	}

	price, rateTime, err := w.provider.FetchRate(ctx, base, quote)
	if err != nil {
		w.log.Warn("fetch rate failed", "pair", job.Pair, "err", err)
		_ = w.jobs.MarkFailed(ctx, job.ID, err.Error())
		// TODO: если job.Attempts < maxAttempts — вернуть задачу в pending
		// с экспоненциальным backoff вместо немедленного failed.
		return
	}

	err = w.quotes.Insert(ctx, domain.QuoteValue{
		JobID:    job.ID,
		Pair:     job.Pair,
		Price:    price,
		RateTime: rateTime,
	})
	if err != nil {
		w.log.Error("insert quote value failed", "err", err)
		_ = w.jobs.MarkFailed(ctx, job.ID, err.Error())
		return
	}

	if err := w.jobs.MarkDone(ctx, job.ID); err != nil {
		w.log.Error("mark done failed", "err", err)
	}
}

func splitPair(pair string) (base, quote string, ok bool) {
	parts := strings.Split(pair, "/")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
