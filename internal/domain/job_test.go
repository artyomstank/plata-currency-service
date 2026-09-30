package domain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var testCurrencies = []string{"EUR", "MXN", "USD"}

func newTestJob(t *testing.T) *Job {
	t.Helper()
	job, err := NewJob("EUR/MXN", "request-1", testCurrencies)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func newTestQuote(t *testing.T, job *Job) *Quote {
	t.Helper()
	quote, err := NewQuote(job.ID, job.Pair, decimal.RequireFromString("19.123456789012345678"), time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), testCurrencies)
	if err != nil {
		t.Fatal(err)
	}
	return quote
}

func TestNewJob(t *testing.T) {
	job, err := NewJob(" eur/mxn ", " key with spaces ", testCurrencies)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID == (JobID{}) || job.Pair != "EUR/MXN" || job.Status != JobStatusPending {
		t.Fatalf("unexpected job: %+v", job)
	}
	if job.IdempotencyKey != " key with spaces " || job.Attempts != 0 || job.ErrorMessage != "" || job.LeaseToken != uuid.Nil {
		t.Fatalf("unexpected initial state: %+v", job)
	}
	if job.CreatedAt.IsZero() || !job.UpdatedAt.Equal(job.CreatedAt) || job.CreatedAt.Location() != time.UTC {
		t.Fatalf("unexpected timestamps: created=%s updated=%s", job.CreatedAt, job.UpdatedAt)
	}
	for _, key := range []string{"", strings.Repeat("x", 128)} {
		if _, err := NewJob("EUR/MXN", key, testCurrencies); err != nil {
			t.Fatalf("valid key rejected: %v", err)
		}
	}
	if _, err := NewJob("EUR/MXN", strings.Repeat("x", 129), testCurrencies); !errors.Is(err, ErrInvalidIdempotency) {
		t.Fatalf("oversized key error = %v", err)
	}
	if _, err := NewJob("EUR/GBP", "", testCurrencies); !errors.Is(err, ErrInvalidPair) {
		t.Fatalf("invalid pair error = %v", err)
	}
}

func TestJobTransitionMatrix(t *testing.T) {
	for _, operation := range []struct {
		name     string
		from, to JobStatus
		apply    func(*Job, *Quote) error
	}{
		{"start", JobStatusPending, JobStatusProcessing, func(j *Job, _ *Quote) error { return j.Start() }},
		{"reclaim", JobStatusProcessing, JobStatusProcessing, func(j *Job, _ *Quote) error { return j.Reclaim() }},
		{"complete", JobStatusProcessing, JobStatusDone, func(j *Job, q *Quote) error { return j.Complete(q) }},
		{"retry", JobStatusProcessing, JobStatusPending, func(j *Job, _ *Quote) error { return j.RetryOrFail(3, "provider unavailable") }},
		{"fail", JobStatusProcessing, JobStatusFailed, func(j *Job, _ *Quote) error { return j.Fail("permanent failure") }},
	} {
		for _, status := range []JobStatus{JobStatusPending, JobStatusProcessing, JobStatusDone, JobStatusFailed, "unknown"} {
			t.Run(operation.name+"/"+string(status), func(t *testing.T) {
				job := newTestJob(t)
				job.Status = status
				job.Attempts = 1
				before := *job
				err := operation.apply(job, newTestQuote(t, job))
				if status != operation.from {
					if !errors.Is(err, ErrInvalidTransition) {
						t.Fatalf("transition error = %v", err)
					}
					if *job != before {
						t.Fatal("rejected transition changed the job")
					}
					return
				}
				if err != nil || job.Status != operation.to {
					t.Fatalf("transition status = %s, error = %v", job.Status, err)
				}
			})
		}
	}
}

func TestReclaimIncrementsAttemptsWithoutChangingIdentity(t *testing.T) {
	job := newTestJob(t)
	if err := job.Start(); err != nil {
		t.Fatal(err)
	}
	id, pair := job.ID, job.Pair
	if err := job.Reclaim(); err != nil {
		t.Fatal(err)
	}
	if job.Attempts != 2 || job.Status != JobStatusProcessing || job.ID != id || job.Pair != pair {
		t.Fatalf("unexpected reclaimed job: %+v", job)
	}
}

func TestJobRetryLifecycle(t *testing.T) {
	job := newTestJob(t)
	for attempt := 1; attempt <= 3; attempt++ {
		if err := job.Start(); err != nil {
			t.Fatal(err)
		}
		if job.Attempts != attempt {
			t.Fatalf("attempts = %d, want %d", job.Attempts, attempt)
		}
		job.LeaseToken = uuid.New()
		if err := job.RetryOrFail(3, "provider unavailable"); err != nil {
			t.Fatal(err)
		}
		want := JobStatusPending
		if attempt == 3 {
			want = JobStatusFailed
		}
		if job.Status != want || job.LeaseToken != uuid.Nil || job.ErrorMessage != "provider unavailable" {
			t.Fatalf("unexpected retry state: %+v", job)
		}
	}
	if err := job.Start(); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("failed job restarted: %v", err)
	}
}

func TestJobCompletionRequiresMatchingValidQuote(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Quote) *Quote
	}{
		{"nil quote", func(*Quote) *Quote { return nil }},
		{"another job", func(q *Quote) *Quote { q.JobID = NewJobID(); return q }},
		{"another pair", func(q *Quote) *Quote { q.Pair = "USD/MXN"; return q }},
		{"invalid price", func(q *Quote) *Quote { q.Price = decimal.Zero; return q }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := newTestJob(t)
			if err := job.Start(); err != nil {
				t.Fatal(err)
			}
			before := *job
			if err := job.Complete(tc.change(newTestQuote(t, job))); !errors.Is(err, ErrInvalidQuote) {
				t.Fatalf("completion error = %v", err)
			}
			if *job != before {
				t.Fatal("rejected quote changed the job")
			}
		})
	}
	job := newTestJob(t)
	if err := job.Start(); err != nil {
		t.Fatal(err)
	}
	job.ErrorMessage = "previous attempt failed"
	job.LeaseToken = uuid.New()
	if err := job.Complete(newTestQuote(t, job)); err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusDone || job.ErrorMessage != "" || job.LeaseToken != uuid.Nil || job.Attempts != 1 {
		t.Fatalf("unexpected completed state: %+v", job)
	}
}

func TestJobRejectsInvalidRetryAndFailure(t *testing.T) {
	for _, apply := range []func(*Job) error{
		func(j *Job) error { return j.RetryOrFail(0, "failure") },
		func(j *Job) error { return j.RetryOrFail(3, " ") },
		func(j *Job) error { return j.Fail("") },
	} {
		job := newTestJob(t)
		if err := job.Start(); err != nil {
			t.Fatal(err)
		}
		before := *job
		if err := apply(job); !errors.Is(err, ErrInvalidJob) {
			t.Fatalf("invalid failure error = %v", err)
		}
		if *job != before {
			t.Fatal("invalid failure changed the job")
		}
	}
}

func TestJobIDRoundTrip(t *testing.T) {
	id := NewJobID()
	parsed, err := ParseJobID(id.String())
	if err != nil || parsed != id || id == (JobID{}) {
		t.Fatalf("job ID round trip: id=%s parsed=%s error=%v", id, parsed, err)
	}
}

func TestParseJobIDRejectsInvalidValues(t *testing.T) {
	for _, raw := range []string{"", "not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		id, err := ParseJobID(raw)
		if !errors.Is(err, ErrInvalidJobID) || id != (JobID{}) {
			t.Fatalf("ParseJobID(%q) = %s, %v", raw, id, err)
		}
	}
}
