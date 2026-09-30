package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"currency-quotes/internal/domain"
)

type rateProviderFunc func(context.Context, string, string) (decimal.Decimal, time.Time, error)

func (f rateProviderFunc) FetchRate(ctx context.Context, base, quote string) (decimal.Decimal, time.Time, error) {
	return f(ctx, base, quote)
}

type testTransactionKey struct{}

// fixture stages both repositories in the same fake transaction. Rejected
// operations cannot publish either the quote or the modified job.
type jobFixture struct {
	job                     *domain.Job
	quote                   *domain.Quote
	stagedJob               *domain.Job
	stagedQuote             *domain.Quote
	active                  bool
	commits, rollbacks      int
	quoteError, commitError error
	lastUpdate              JobUpdate
	uc                      *QuotesUseCase
}

func cloneJob(job *domain.Job) *domain.Job {
	if job == nil {
		return nil
	}
	copy := *job
	return &copy
}

func newJobFixture(t *testing.T) *jobFixture {
	t.Helper()
	job, err := domain.NewJob("EUR/MXN", "request-1", []string{"EUR", "MXN", "USD"})
	if err != nil {
		t.Fatal(err)
	}
	f := &jobFixture{job: job}
	checkContext := func(ctx context.Context) {
		t.Helper()
		if ctx.Value(testTransactionKey{}) != f || !f.active {
			t.Fatal("repository called outside shared transaction")
		}
	}
	tx := transactionFunc(func(ctx context.Context, fn func(context.Context) error) error {
		if f.active {
			t.Fatal("nested transaction")
		}
		f.active = true
		defer func() { f.active = false }()
		f.stagedJob, f.stagedQuote = cloneJob(f.job), f.quote
		err := fn(context.WithValue(ctx, testTransactionKey{}, f))
		if err == nil {
			err = f.commitError
		}
		if err != nil {
			f.rollbacks++
			return err
		}
		f.job, f.quote = f.stagedJob, f.stagedQuote
		f.commits++
		return nil
	})
	jobs := jobsStub{
		lock: func(ctx context.Context) (*domain.Job, error) { checkContext(ctx); return cloneJob(f.stagedJob), nil },
		get: func(ctx context.Context, _ domain.JobID) (*domain.Job, error) {
			checkContext(ctx)
			return cloneJob(f.stagedJob), nil
		},
		getLocked: func(ctx context.Context, id domain.JobID) (*domain.Job, error) {
			checkContext(ctx)
			if f.stagedJob == nil || f.stagedJob.ID != id {
				return nil, domain.ErrNotFound
			}
			return cloneJob(f.stagedJob), nil
		},
		save: func(ctx context.Context, job *domain.Job, update JobUpdate) error {
			checkContext(ctx)
			if f.stagedJob.LeaseToken != update.ExpectedLeaseToken {
				return domain.ErrClaimLost
			}
			f.stagedJob = cloneJob(job)
			f.lastUpdate = update
			return nil
		},
	}
	quotes := quotesStub{
		save: func(ctx context.Context, quote *domain.Quote) error {
			checkContext(ctx)
			if f.quoteError != nil {
				return f.quoteError
			}
			f.stagedQuote = quote
			return nil
		},
		byJob: func(ctx context.Context, id domain.JobID) (*domain.Quote, error) {
			checkContext(ctx)
			if f.stagedQuote == nil || f.stagedQuote.JobID != id {
				return nil, domain.ErrNotFound
			}
			return f.stagedQuote, nil
		},
	}
	f.uc = New(jobs, quotes, tx, nil, Config{AllowedCurrencies: []string{"EUR", "MXN", "USD"}, LeaseDuration: 30 * time.Second, MaxAttempts: 5, RetryBase: time.Second, RetryMax: 30 * time.Second})
	return f
}

func completionInput(job *domain.Job) CompleteJobInput {
	return CompleteJobInput{JobID: job.ID, LeaseToken: job.LeaseToken, Price: decimal.RequireFromString("19.123456789012345678"), SourceTime: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
}

func TestClaimPendingStartsAttemptAndReplacesExpiredLease(t *testing.T) {
	for _, reclaim := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "expired"}[reclaim], func(t *testing.T) {
			f := newJobFixture(t)
			attempts := 1
			if reclaim {
				if err := f.job.Start(); err != nil {
					t.Fatal(err)
				}
				f.job.LeaseToken = uuid.New()
				attempts = 2
			}
			oldToken := f.job.LeaseToken
			now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
			f.uc.now = func() time.Time { return now }
			claimed, err := f.uc.ClaimPending(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if claimed.Status != domain.JobStatusProcessing || claimed.Attempts != attempts || claimed.LeaseToken == uuid.Nil || claimed.LeaseToken == oldToken {
				t.Fatalf("unexpected claim: %+v", claimed)
			}
			if f.lastUpdate.ExpectedLeaseToken != oldToken || !f.lastUpdate.LeaseUntil.Equal(now.Add(30*time.Second)) || f.commits != 1 {
				t.Fatalf("incorrect lease update: %+v", f.lastUpdate)
			}
		})
	}
}

func TestClaimPendingReturnsNilForEmptyQueue(t *testing.T) {
	f := newJobFixture(t)
	f.job = nil
	claimed, err := f.uc.ClaimPending(context.Background())
	if claimed != nil || err != nil {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
}

func TestCompleteJobPersistsBothEntitiesAndReadsResult(t *testing.T) {
	f := newJobFixture(t)
	job, err := f.uc.ClaimPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Admission changes must not invalidate an already accepted job.
	f.uc.config.AllowedCurrencies = []string{"CHF", "JPY"}
	if err := f.uc.CompleteJob(context.Background(), completionInput(job)); err != nil {
		t.Fatal(err)
	}
	if f.job.Status != domain.JobStatusDone || f.job.LeaseToken != uuid.Nil || f.quote == nil || f.quote.JobID != job.ID {
		t.Fatalf("job=%+v quote=%+v", f.job, f.quote)
	}
	if err := f.quote.Validate(); err != nil {
		t.Fatal(err)
	}
	if f.quote.Price.String() != "19.123456789012345678" {
		t.Fatal("decimal precision lost")
	}
	result, err := f.uc.GetJobResult(context.Background(), GetQuoteUpdateInput{job.ID})
	if err != nil || result.Value != f.quote {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestCompleteJobRollsBackBothRepositories(t *testing.T) {
	for _, failCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "quote save", true: "commit"}[failCommit], func(t *testing.T) {
			f := newJobFixture(t)
			job, err := f.uc.ClaimPending(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			failure := errors.New("persistence failed")
			if failCommit {
				f.commitError = failure
			} else {
				f.quoteError = failure
			}
			err = f.uc.CompleteJob(context.Background(), completionInput(job))
			if !errors.Is(err, failure) {
				t.Fatalf("err=%v", err)
			}
			if f.quote != nil || f.job.Status != domain.JobStatusProcessing || f.job.LeaseToken != job.LeaseToken || f.rollbacks != 1 {
				t.Fatalf("partially persisted completion: job=%+v quote=%+v", f.job, f.quote)
			}
		})
	}
}

func TestStaleWorkerCannotCompleteOrReleaseReclaimedJob(t *testing.T) {
	f := newJobFixture(t)
	old, err := f.uc.ClaimPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The repository selected the old processing row because its lease expired.
	current, err := f.uc.ClaimPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, release := range []bool{false, true} {
		var err error
		if release {
			err = f.uc.RetryJob(context.Background(), RetryJobInput{old.ID, old.LeaseToken})
		} else {
			err = f.uc.CompleteJob(context.Background(), completionInput(old))
		}
		if !errors.Is(err, domain.ErrClaimLost) {
			t.Fatalf("stale operation err=%v", err)
		}
		if f.job.LeaseToken != current.LeaseToken || f.quote != nil {
			t.Fatal("stale worker changed job")
		}
	}
}

func TestCompleteJobRejectsInvalidQuoteBeforeSaving(t *testing.T) {
	f := newJobFixture(t)
	job, err := f.uc.ClaimPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	input := completionInput(job)
	input.Price = decimal.Zero
	if err := f.uc.CompleteJob(context.Background(), input); !errors.Is(err, domain.ErrInvalidQuote) {
		t.Fatalf("err=%v", err)
	}
	if f.job.Status != domain.JobStatusProcessing || f.quote != nil {
		t.Fatal("invalid quote persisted")
	}
}

func TestProcessNextCallsProviderOutsideTransaction(t *testing.T) {
	f := newJobFixture(t)
	f.uc.provider = rateProviderFunc(func(ctx context.Context, base, quote string) (decimal.Decimal, time.Time, error) {
		if f.active || ctx.Value(testTransactionKey{}) != nil {
			t.Fatal("provider called inside transaction")
		}
		if base != "EUR" || quote != "MXN" {
			t.Fatalf("pair=%s/%s", base, quote)
		}
		return decimal.RequireFromString("20.12"), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), nil
	})
	claimed, err := f.uc.ProcessNext(context.Background())
	if !claimed || err != nil || f.job.Status != domain.JobStatusDone || f.commits != 2 {
		t.Fatalf("claimed=%v err=%v job=%+v", claimed, err, f.job)
	}
}

func TestProcessNextRetriesProviderErrorsAndInvalidData(t *testing.T) {
	for _, invalidData := range []bool{false, true} {
		for _, attempt := range []int{1, 3, 5} {
			t.Run(fmt.Sprintf("%s/attempt-%d", map[bool]string{false: "provider error", true: "invalid data"}[invalidData], attempt), func(t *testing.T) {
				f := newJobFixture(t)
				f.job.Attempts = attempt - 1
				now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
				f.uc.now = func() time.Time { return now }
				failure := errors.New("private provider credentials")
				f.uc.provider = rateProviderFunc(func(context.Context, string, string) (decimal.Decimal, time.Time, error) {
					if invalidData {
						return decimal.Zero, now, nil
					}
					return decimal.Zero, time.Time{}, failure
				})
				claimed, err := f.uc.ProcessNext(context.Background())
				if !claimed || err == nil {
					t.Fatalf("claimed=%v err=%v", claimed, err)
				}
				if !invalidData && !errors.Is(err, failure) {
					t.Fatalf("provider cause lost: %v", err)
				}
				expected := domain.JobStatusPending
				if attempt == 5 {
					expected = domain.JobStatusFailed
				}
				if f.job.Status != expected || f.job.LeaseToken != uuid.Nil || f.job.ErrorMessage != publicProviderError || strings.Contains(f.job.ErrorMessage, "credentials") {
					t.Fatalf("retry job=%+v", f.job)
				}
				if !f.lastUpdate.NextAttemptAt.Equal(now.Add(time.Second*time.Duration(1<<(attempt-1)))) || f.quote != nil {
					t.Fatalf("retry update=%+v", f.lastUpdate)
				}
			})
		}
	}
}

func TestRetryDelayCapsWithoutOverflow(t *testing.T) {
	uc := newTestUseCase(jobsStub{}, quotesStub{})
	for _, tc := range []struct {
		attempt int
		want    time.Duration
	}{{1, time.Second}, {3, 4 * time.Second}, {6, 30 * time.Second}, {1000, 30 * time.Second}} {
		if got := uc.retryDelay(tc.attempt); got != tc.want {
			t.Fatalf("attempt=%d got=%s want=%s", tc.attempt, got, tc.want)
		}
	}
}

func TestRequestUpdateUsesDomainConstructorBeforeTransaction(t *testing.T) {
	transactions := 0
	uc := newTestUseCase(jobsStub{createJob: func(_ context.Context, job *domain.Job) (*domain.Job, bool, error) {
		if job.ID == (domain.JobID{}) || job.Status != domain.JobStatusPending || job.CreatedAt.IsZero() || job.Pair != "EUR/MXN" {
			t.Fatalf("unconstructed job: %+v", job)
		}
		return job, true, nil
	}}, quotesStub{})
	uc.tx = transactionFunc(func(ctx context.Context, fn func(context.Context) error) error { transactions++; return fn(ctx) })
	if _, err := uc.RequestUpdate(context.Background(), RequestQuoteUpdateInput{Pair: "eur/mxn"}); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.RequestUpdate(context.Background(), RequestQuoteUpdateInput{Pair: "EUR/MXN", IdempotencyKey: strings.Repeat("x", 129)}); !errors.Is(err, domain.ErrInvalidIdempotency) {
		t.Fatalf("err=%v", err)
	}
	if transactions != 1 {
		t.Fatalf("invalid input opened a transaction: %d", transactions)
	}
}

func TestProcessNextDoesNotFetchWhenQueueEmptyOrClaimCommitFails(t *testing.T) {
	for _, failCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty queue", true: "claim commit failed"}[failCommit], func(t *testing.T) {
			f := newJobFixture(t)
			failure := errors.New("claim commit failed")
			if failCommit {
				f.commitError = failure
			} else {
				f.job = nil
			}
			f.uc.provider = rateProviderFunc(func(context.Context, string, string) (decimal.Decimal, time.Time, error) {
				t.Fatal("provider called without a committed claim")
				return decimal.Zero, time.Time{}, nil
			})
			claimed, err := f.uc.ProcessNext(context.Background())
			if claimed {
				t.Fatal("reported an uncommitted claim")
			}
			if failCommit && !errors.Is(err, failure) {
				t.Fatalf("err=%v", err)
			}
			if !failCommit && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProcessNextFailsInvalidStoredPairWithoutCallingProvider(t *testing.T) {
	f := newJobFixture(t)
	f.job.Pair = "USD/USD"
	f.uc.provider = rateProviderFunc(func(context.Context, string, string) (decimal.Decimal, time.Time, error) {
		t.Fatal("invalid stored pair reached provider")
		return decimal.Zero, time.Time{}, nil
	})
	claimed, err := f.uc.ProcessNext(context.Background())
	if !claimed || !errors.Is(err, domain.ErrInvalidPair) {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	if f.job.Status != domain.JobStatusFailed || f.job.Attempts != 1 || f.job.LeaseToken != uuid.Nil || f.quote != nil {
		t.Fatalf("invalid pair was not permanently failed: %+v", f.job)
	}
}
