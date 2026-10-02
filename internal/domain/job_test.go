package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

var testCurrencies = []string{"EUR", "MXN", "USD"}

var testLeaseUntil = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

func newTestJob(t *testing.T) *Job {
	t.Helper()
	job, err := NewJob("EUR/MXN", testCurrencies)
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
	job, err := NewJob(" eur/mxn ", testCurrencies)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID == (JobID{}) || job.Pair != "EUR/MXN" || job.Status != JobStatusPending {
		t.Fatalf("unexpected job: %+v", job)
	}
	if job.Attempts != 0 || job.ErrorMessage != "" || !job.LeaseUntil.IsZero() || !job.NextAttemptAt.Equal(job.CreatedAt) {
		t.Fatalf("unexpected initial state: %+v", job)
	}
	if job.CreatedAt.IsZero() || !job.UpdatedAt.Equal(job.CreatedAt) || job.CreatedAt.Location() != time.UTC {
		t.Fatalf("unexpected timestamps: created=%s updated=%s", job.CreatedAt, job.UpdatedAt)
	}
	if _, err := NewJob("EUR/GBP", testCurrencies); !errors.Is(err, ErrInvalidPair) {
		t.Fatalf("invalid pair error = %v", err)
	}
}

func TestJobTransitionMatrix(t *testing.T) {
	for _, operation := range []struct {
		name     string
		from, to JobStatus
		apply    func(*Job, *Quote) error
	}{
		{"start", JobStatusPending, JobStatusProcessing, func(j *Job, _ *Quote) error { return j.Start(testLeaseUntil) }},
		{"reclaim", JobStatusProcessing, JobStatusProcessing, func(j *Job, _ *Quote) error { return j.Reclaim(testLeaseUntil.Add(time.Minute)) }},
		{"complete", JobStatusProcessing, JobStatusDone, func(j *Job, q *Quote) error { return j.Complete(q) }},
		{"retry", JobStatusProcessing, JobStatusPending, func(j *Job, _ *Quote) error {
			return j.RetryOrFail(3, "provider unavailable", testLeaseUntil.Add(time.Second))
		}},
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
	if err := job.Start(testLeaseUntil); err != nil {
		t.Fatal(err)
	}
	id, pair := job.ID, job.Pair
	if err := job.Reclaim(testLeaseUntil.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if job.Attempts != 2 || job.Status != JobStatusProcessing || job.ID != id || job.Pair != pair || !job.LeaseUntil.Equal(testLeaseUntil.Add(time.Minute)) {
		t.Fatalf("unexpected reclaimed job: %+v", job)
	}
}

func TestJobRejectsEmptyLeaseDeadline(t *testing.T) {
	for _, reclaim := range []bool{false, true} {
		t.Run(map[bool]string{false: "start", true: "reclaim"}[reclaim], func(t *testing.T) {
			job := newTestJob(t)
			if reclaim {
				if err := job.Start(testLeaseUntil); err != nil {
					t.Fatal(err)
				}
			}
			before := *job
			var err error
			if reclaim {
				err = job.Reclaim(time.Time{})
			} else {
				err = job.Start(time.Time{})
			}
			if !errors.Is(err, ErrInvalidJob) {
				t.Fatalf("empty lease error = %v", err)
			}
			if *job != before {
				t.Fatal("empty lease changed the job")
			}
		})
	}
}

func TestJobRetryLifecycle(t *testing.T) {
	job := newTestJob(t)
	for attempt := 1; attempt <= 3; attempt++ {
		if err := job.Start(testLeaseUntil); err != nil {
			t.Fatal(err)
		}
		if job.Attempts != attempt {
			t.Fatalf("attempts = %d, want %d", job.Attempts, attempt)
		}
		if err := job.RetryOrFail(3, "provider unavailable", testLeaseUntil.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		want := JobStatusPending
		if attempt == 3 {
			want = JobStatusFailed
		}
		if job.Status != want || !job.LeaseUntil.IsZero() || job.ErrorMessage != "provider unavailable" || !job.NextAttemptAt.Equal(testLeaseUntil.Add(time.Second)) {
			t.Fatalf("unexpected retry state: %+v", job)
		}
	}
	if err := job.Start(testLeaseUntil); !errors.Is(err, ErrInvalidTransition) {
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
			if err := job.Start(testLeaseUntil); err != nil {
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
	if err := job.Start(testLeaseUntil); err != nil {
		t.Fatal(err)
	}
	job.ErrorMessage = "previous attempt failed"
	if err := job.Complete(newTestQuote(t, job)); err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusDone || job.ErrorMessage != "" || !job.LeaseUntil.IsZero() || job.Attempts != 1 {
		t.Fatalf("unexpected completed state: %+v", job)
	}
}

func TestJobRejectsInvalidRetryAndFailure(t *testing.T) {
	for _, apply := range []func(*Job) error{
		func(j *Job) error { return j.RetryOrFail(0, "failure", testLeaseUntil) },
		func(j *Job) error { return j.RetryOrFail(3, " ", testLeaseUntil) },
		func(j *Job) error { return j.Fail("") },
		func(j *Job) error { return j.RetryOrFail(3, "failure", time.Time{}) },
	} {
		job := newTestJob(t)
		if err := job.Start(testLeaseUntil); err != nil {
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
