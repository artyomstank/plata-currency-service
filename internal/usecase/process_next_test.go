package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"currency-quotes/internal/domain"
)

type rateProviderFunc func(context.Context, string, string) (decimal.Decimal, time.Time, error)

func (f rateProviderFunc) FetchRate(ctx context.Context, base, quote string) (decimal.Decimal, time.Time, error) {
	return f(ctx, base, quote)
}

type testTransactionKey struct{}

type jobFixture struct {
	job                     *domain.Job
	quote                   *domain.Quote
	stagedJob               *domain.Job
	stagedQuote             *domain.Quote
	active                  bool
	commits, rollbacks      int
	quoteError, commitError error
	lastExpectedAttempt     int
	claim                   *ClaimPending
	complete                *CompleteJob
	retry                   *RetryJob
	process                 *ProcessNext
	getResult               *GetJobResult
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
	job, err := domain.NewJob("EUR/MXN", []string{"EUR", "MXN", "USD"})
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
		save: func(ctx context.Context, job *domain.Job, expectedAttempt int) error {
			checkContext(ctx)
			if f.stagedJob.Attempts != expectedAttempt {
				return domain.ErrClaimLost
			}
			f.stagedJob = cloneJob(job)
			f.lastExpectedAttempt = expectedAttempt
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
	f.claim = NewClaimPending(jobs, tx, ClaimPendingConfig{LeaseDuration: 30 * time.Second})
	f.complete = NewCompleteJob(jobs, quotes, tx)
	f.retry = NewRetryJob(jobs, tx, RetryConfig{MaxAttempts: 5, RetryBase: time.Second, RetryMax: 30 * time.Second})
	f.process = NewProcessNext(f.claim, f.complete, f.retry, nil)
	f.getResult = NewGetJobResult(jobs, quotes, tx)
	return f
}

func completionInput(job *domain.Job) CompleteJobInput {
	return CompleteJobInput{JobID: job.ID, ExpectedAttempt: job.Attempts, Price: decimal.RequireFromString("19.123456789012345678"), SourceTime: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
}

func TestClaimPendingStartsAttemptAndReplacesExpiredLease(t *testing.T) {
	for _, reclaim := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "expired"}[reclaim], func(t *testing.T) {
			f := newJobFixture(t)
			attempts := 1
			if reclaim {
				if err := f.job.Start(time.Now().Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
				attempts = 2
			}
			oldAttempt := f.job.Attempts
			now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
			f.claim.now = func() time.Time { return now }
			f.retry.now = f.claim.now
			claimed, err := f.claim.Execute(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if claimed.Status != domain.JobStatusProcessing || claimed.Attempts != attempts || claimed.Attempts <= oldAttempt {
				t.Fatalf("unexpected claim: %+v", claimed)
			}
			if f.lastExpectedAttempt != oldAttempt || !claimed.LeaseUntil.Equal(now.Add(30*time.Second)) || f.commits != 1 {
				t.Fatalf("incorrect lease update: %+v", f.job)
			}
		})
	}
}

func TestClaimPendingReturnsNilForEmptyQueue(t *testing.T) {
	f := newJobFixture(t)
	f.job = nil
	claimed, err := f.claim.Execute(context.Background())
	if claimed != nil || err != nil {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
}

func TestCompleteJobPersistsBothEntitiesAndReadsResult(t *testing.T) {
	f := newJobFixture(t)
	job, err := f.claim.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewGetLatest(quotesStub{}, CurrencyConfig{AllowedCurrencies: []string{"CHF", "JPY"}}).Execute(context.Background(), GetLatestQuoteInput{Pair: job.Pair}); !errors.Is(err, domain.ErrInvalidPair) {
		t.Fatalf("admission list was not changed: %v", err)
	}
	if err := f.complete.Execute(context.Background(), completionInput(job)); err != nil {
		t.Fatal(err)
	}
	if f.job.Status != domain.JobStatusDone || !f.job.LeaseUntil.IsZero() || f.quote == nil || f.quote.JobID != job.ID {
		t.Fatalf("job=%+v quote=%+v", f.job, f.quote)
	}
	if err := f.quote.Validate(); err != nil {
		t.Fatal(err)
	}
	if f.quote.Price.String() != "19.123456789012345678" {
		t.Fatal("decimal precision lost")
	}
	result, err := f.getResult.Execute(context.Background(), GetQuoteUpdateInput{job.ID})
	if err != nil || result.Value != f.quote {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestCompleteJobRollsBackBothRepositories(t *testing.T) {
	for _, failCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "quote save", true: "commit"}[failCommit], func(t *testing.T) {
			f := newJobFixture(t)
			job, err := f.claim.Execute(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			failure := errors.New("persistence failed")
			if failCommit {
				f.commitError = failure
			} else {
				f.quoteError = failure
			}
			err = f.complete.Execute(context.Background(), completionInput(job))
			if !errors.Is(err, failure) {
				t.Fatalf("err=%v", err)
			}
			if f.quote != nil || f.job.Status != domain.JobStatusProcessing || f.job.Attempts != job.Attempts || !f.job.LeaseUntil.Equal(job.LeaseUntil) || f.rollbacks != 1 {
				t.Fatalf("partially persisted completion: job=%+v quote=%+v", f.job, f.quote)
			}
		})
	}
}

func TestStaleWorkerCannotCompleteOrReleaseReclaimedJob(t *testing.T) {
	f := newJobFixture(t)
	old, err := f.claim.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.claim.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, release := range []bool{false, true} {
		var err error
		if release {
			err = f.retry.Execute(context.Background(), RetryJobInput{old.ID, old.Attempts})
		} else {
			err = f.complete.Execute(context.Background(), completionInput(old))
		}
		if !errors.Is(err, domain.ErrClaimLost) {
			t.Fatalf("stale operation err=%v", err)
		}
		if f.job.Attempts != current.Attempts || f.quote != nil {
			t.Fatal("stale worker changed job")
		}
	}
}

func TestReleasedAttemptCannotCompleteOrRetryAgain(t *testing.T) {
	f := newJobFixture(t)
	job, err := f.claim.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	input := RetryJobInput{JobID: job.ID, ExpectedAttempt: job.Attempts}
	if err := f.retry.Execute(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	before := *f.job
	if err := f.complete.Execute(context.Background(), completionInput(job)); !errors.Is(err, domain.ErrClaimLost) {
		t.Fatalf("released completion error = %v", err)
	}
	if err := f.retry.Execute(context.Background(), input); !errors.Is(err, domain.ErrClaimLost) {
		t.Fatalf("duplicate retry error = %v", err)
	}
	if *f.job != before || f.quote != nil {
		t.Fatal("released attempt changed the job")
	}
}

func TestInvalidAttemptDoesNotReachRepository(t *testing.T) {
	for _, attempt := range []int{0, -1} {
		jobs := jobsStub{getLocked: func(context.Context, domain.JobID) (*domain.Job, error) {
			t.Fatal("invalid attempt reached repository")
			return nil, nil
		}}
		jobID := domain.NewJobID()
		complete := NewCompleteJob(jobs, quotesStub{}, directTransaction())
		if err := complete.Execute(context.Background(), CompleteJobInput{JobID: jobID, ExpectedAttempt: attempt}); !errors.Is(err, domain.ErrClaimLost) {
			t.Fatalf("attempt %d completion error = %v", attempt, err)
		}
		retry := NewRetryJob(jobs, directTransaction(), RetryConfig{})
		if err := retry.Execute(context.Background(), RetryJobInput{JobID: jobID, ExpectedAttempt: attempt}); !errors.Is(err, domain.ErrClaimLost) {
			t.Fatalf("attempt %d retry error = %v", attempt, err)
		}
	}
}

func TestCompleteJobRejectsInvalidQuoteBeforeSaving(t *testing.T) {
	f := newJobFixture(t)
	job, err := f.claim.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	input := completionInput(job)
	input.Price = decimal.Zero
	if err := f.complete.Execute(context.Background(), input); !errors.Is(err, domain.ErrInvalidQuote) {
		t.Fatalf("err=%v", err)
	}
	if f.job.Status != domain.JobStatusProcessing || f.quote != nil {
		t.Fatal("invalid quote persisted")
	}
}

func TestProcessNextCallsProviderOutsideTransaction(t *testing.T) {
	f := newJobFixture(t)
	f.process.provider = rateProviderFunc(func(ctx context.Context, base, quote string) (decimal.Decimal, time.Time, error) {
		if f.active || ctx.Value(testTransactionKey{}) != nil {
			t.Fatal("provider called inside transaction")
		}
		if base != "EUR" || quote != "MXN" {
			t.Fatalf("pair=%s/%s", base, quote)
		}
		return decimal.RequireFromString("20.12"), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), nil
	})
	claimed, err := f.process.Execute(context.Background())
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
				f.claim.now = func() time.Time { return now }
				f.retry.now = f.claim.now
				failure := errors.New("private provider credentials")
				f.process.provider = rateProviderFunc(func(context.Context, string, string) (decimal.Decimal, time.Time, error) {
					if invalidData {
						return decimal.Zero, now, nil
					}
					return decimal.Zero, time.Time{}, failure
				})
				claimed, err := f.process.Execute(context.Background())
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
				if f.job.Status != expected || !f.job.LeaseUntil.IsZero() || f.job.ErrorMessage != publicProviderError || strings.Contains(f.job.ErrorMessage, "credentials") {
					t.Fatalf("retry job=%+v", f.job)
				}
				if !f.job.NextAttemptAt.Equal(now.Add(time.Second*time.Duration(1<<(attempt-1)))) || f.quote != nil {
					t.Fatalf("retry update=%+v", f.job)
				}
			})
		}
	}
}

func TestRetryDelayCapsWithoutOverflow(t *testing.T) {
	uc := NewRetryJob(jobsStub{}, directTransaction(), RetryConfig{RetryBase: time.Second, RetryMax: 30 * time.Second})
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
	uc := NewRequestUpdate(jobsStub{createJob: func(_ context.Context, job *domain.Job) (*domain.Job, error) {
		if job.ID == (domain.JobID{}) || job.Status != domain.JobStatusPending || job.CreatedAt.IsZero() || job.Pair != "EUR/MXN" {
			t.Fatalf("unconstructed job: %+v", job)
		}
		return job, nil
	}}, directTransaction(), testCurrencies)
	uc.tx = transactionFunc(func(ctx context.Context, fn func(context.Context) error) error { transactions++; return fn(ctx) })
	if _, err := uc.Execute(context.Background(), RequestQuoteUpdateInput{Pair: "eur/mxn"}); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Execute(context.Background(), RequestQuoteUpdateInput{Pair: "EUR/EUR"}); !errors.Is(err, domain.ErrInvalidPair) {
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
			f.process.provider = rateProviderFunc(func(context.Context, string, string) (decimal.Decimal, time.Time, error) {
				t.Fatal("provider called without a committed claim")
				return decimal.Zero, time.Time{}, nil
			})
			claimed, err := f.process.Execute(context.Background())
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
	f.process.provider = rateProviderFunc(func(context.Context, string, string) (decimal.Decimal, time.Time, error) {
		t.Fatal("invalid stored pair reached provider")
		return decimal.Zero, time.Time{}, nil
	})
	claimed, err := f.process.Execute(context.Background())
	if !claimed || !errors.Is(err, domain.ErrInvalidPair) {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	if f.job.Status != domain.JobStatusFailed || f.job.Attempts != 1 || !f.job.LeaseUntil.IsZero() || f.quote != nil {
		t.Fatalf("invalid pair was not permanently failed: %+v", f.job)
	}
}
