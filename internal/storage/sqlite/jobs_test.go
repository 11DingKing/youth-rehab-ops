package sqlite

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
)

func seedNotificationJob(t *testing.T, store *Store, maxAttempts int, due time.Time) int64 {
	t.Helper()
	guardian := createUser(t, store, fmt.Sprintf("guardian-%d@example.test", time.Now().UnixNano()), domain.RoleGuardian)
	reporter := createUser(t, store, fmt.Sprintf("coach-%d@example.test", time.Now().UnixNano()), domain.RoleCoach)
	participant, err := store.CreateParticipant(context.Background(), domain.Participant{PublicID: fmt.Sprintf("participant_%d", time.Now().UnixNano()), Name: "Youth",
		BirthDate: testTime.AddDate(-12, 0, 0), GuardianUserID: guardian.ID, VenueID: "venue", Active: true, CreatedAt: testTime})
	if err != nil {
		t.Fatalf("CreateParticipant: %v", err)
	}
	incident, err := store.CreateIncident(context.Background(), domain.Actor{UserID: reporter.ID, Role: reporter.Role, RequestID: "seed-job"},
		domain.Incident{PublicID: fmt.Sprintf("inc_%d", time.Now().UnixNano()), ParticipantID: participant.ID, ReporterUserID: reporter.ID,
			Kind: domain.InjuryStrain, BodyArea: "leg", OccurredAt: testTime, Description: "pain", Status: domain.IncidentReported,
			Severity: domain.SeverityLow, StopTraining: true, Version: 1, CreatedAt: testTime, UpdatedAt: testTime},
		fmt.Sprintf("idem_%d", time.Now().UnixNano()), "sha")
	if err != nil {
		t.Fatalf("CreateIncident: %v", err)
	}
	notification, err := store.db.Exec(`INSERT INTO guardian_notifications(incident_id,guardian_user_id,channel,message_class,status,created_at) VALUES(?,?,?,?,?,?)`,
		incident.ID, guardian.ID, "email", fmt.Sprintf("test_%d", time.Now().UnixNano()), "pending", timeText(testTime))
	if err != nil {
		t.Fatalf("insert notification: %v", err)
	}
	notificationID, _ := notification.LastInsertId()
	job, err := store.db.Exec(`INSERT INTO notification_jobs(notification_id,status,max_attempts,next_attempt_at,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		notificationID, "pending", maxAttempts, timeText(due), timeText(testTime), timeText(testTime))
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	jobID, _ := job.LastInsertId()
	return jobID
}

func TestClaimNotificationJobsOnlyReturnsDueWork(t *testing.T) {
	store := openTestStore(t)
	dueID := seedNotificationJob(t, store, 3, testTime.Add(-time.Minute))
	_ = seedNotificationJob(t, store, 3, testTime.Add(time.Hour))
	jobs, err := store.ClaimNotificationJobs(context.Background(), "worker-a", 20, testTime, 30*time.Second)
	if err != nil {
		t.Fatalf("ClaimNotificationJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != dueID {
		t.Fatalf("claimed jobs=%+v", jobs)
	}
	if jobs[0].Status != "processing" || jobs[0].LeaseOwner != "worker-a" || jobs[0].LeaseUntil == nil || !jobs[0].LeaseUntil.Equal(testTime.Add(30*time.Second)) {
		t.Fatalf("lease mismatch: %+v", jobs[0])
	}
}

func TestActiveLeasePreventsDoubleClaim(t *testing.T) {
	store := openTestStore(t)
	jobID := seedNotificationJob(t, store, 3, testTime)
	first, err := store.ClaimNotificationJobs(context.Background(), "worker-a", 1, testTime, time.Minute)
	if err != nil || len(first) != 1 || first[0].ID != jobID {
		t.Fatalf("first claim=%+v err=%v", first, err)
	}
	second, err := store.ClaimNotificationJobs(context.Background(), "worker-b", 1, testTime.Add(30*time.Second), time.Minute)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("active lease was stolen: %+v", second)
	}
}

func TestExpiredProcessingLeaseCanBeRecoveredAfterRestart(t *testing.T) {
	store := openTestStore(t)
	jobID := seedNotificationJob(t, store, 4, testTime)
	first, err := store.ClaimNotificationJobs(context.Background(), "crashed-worker", 1, testTime, time.Minute)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: %+v err=%v", first, err)
	}
	if _, err := store.db.Exec(`UPDATE notification_jobs SET status='retry' WHERE id=?`, jobID); err != nil {
		t.Fatalf("simulate restart recovery status: %v", err)
	}
	recovered, err := store.ClaimNotificationJobs(context.Background(), "replacement-worker", 1, testTime.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("recovery claim: %v", err)
	}
	if len(recovered) != 1 || recovered[0].ID != jobID || recovered[0].LeaseOwner != "replacement-worker" {
		t.Fatalf("job not recovered: %+v", recovered)
	}
}

func TestRetryPersistsAttemptErrorAndFutureDueTime(t *testing.T) {
	store := openTestStore(t)
	jobID := seedNotificationJob(t, store, 4, testTime)
	jobs, err := store.ClaimNotificationJobs(context.Background(), "worker-a", 1, testTime, time.Minute)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim: %+v err=%v", jobs, err)
	}
	if err := store.RetryNotificationJob(context.Background(), jobID, "worker-a", "temporary gateway failure", testTime, 8*time.Second); err != nil {
		t.Fatalf("RetryNotificationJob: %v", err)
	}
	var status, next, owner, lastError string
	var attempts int
	var lease any
	if err := store.db.QueryRow(`SELECT status,attempts,next_attempt_at,lease_owner,lease_until,last_error FROM notification_jobs WHERE id=?`, jobID).
		Scan(&status, &attempts, &next, &owner, &lease, &lastError); err != nil {
		t.Fatalf("read retried job: %v", err)
	}
	if status != "retry" || attempts != 1 || owner != "" || lease != nil || lastError != "temporary gateway failure" {
		t.Fatalf("retry state mismatch status=%s attempts=%d owner=%q lease=%v last=%q", status, attempts, owner, lease, lastError)
	}
	parsed, _ := parseTime(next)
	if !parsed.Equal(testTime.Add(8 * time.Second)) {
		t.Fatalf("next attempt=%s", parsed)
	}
}

func TestCompletingJobAlsoMarksNotificationDelivered(t *testing.T) {
	store := openTestStore(t)
	jobID := seedNotificationJob(t, store, 2, testTime)
	jobs, err := store.ClaimNotificationJobs(context.Background(), "worker", 1, testTime, time.Minute)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim: %+v err=%v", jobs, err)
	}
	if err := store.CompleteNotificationJob(context.Background(), jobID, "worker", testTime); err != nil {
		t.Fatalf("CompleteNotificationJob: %v", err)
	}
	var jobStatus, notificationStatus string
	if err := store.db.QueryRow(`SELECT j.status,n.status FROM notification_jobs j JOIN guardian_notifications n ON n.id=j.notification_id WHERE j.id=?`, jobID).
		Scan(&jobStatus, &notificationStatus); err != nil {
		t.Fatalf("read completed state: %v", err)
	}
	if jobStatus != "completed" || notificationStatus != "delivered" {
		t.Fatalf("job=%s notification=%s", jobStatus, notificationStatus)
	}
}

func TestPermanentFailureAlsoMarksNotificationFailed(t *testing.T) {
	store := openTestStore(t)
	jobID := seedNotificationJob(t, store, 2, testTime)
	jobs, err := store.ClaimNotificationJobs(context.Background(), "worker", 1, testTime, time.Minute)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim: %+v err=%v", jobs, err)
	}
	if err := store.FailNotificationJob(context.Background(), jobID, "worker", "invalid guardian address", testTime); err != nil {
		t.Fatalf("FailNotificationJob: %v", err)
	}
	var jobStatus, notificationStatus, lastError string
	if err := store.db.QueryRow(`SELECT j.status,n.status,j.last_error FROM notification_jobs j JOIN guardian_notifications n ON n.id=j.notification_id WHERE j.id=?`, jobID).
		Scan(&jobStatus, &notificationStatus, &lastError); err != nil {
		t.Fatalf("read failed state: %v", err)
	}
	if jobStatus != "failed" || notificationStatus != "failed" || lastError != "invalid guardian address" {
		t.Fatalf("job=%s notification=%s error=%q", jobStatus, notificationStatus, lastError)
	}
}

func TestOnlyLeaseOwnerCanFinishJob(t *testing.T) {
	store := openTestStore(t)
	jobID := seedNotificationJob(t, store, 2, testTime)
	jobs, err := store.ClaimNotificationJobs(context.Background(), "worker-a", 1, testTime, time.Minute)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim: %+v err=%v", jobs, err)
	}
	for _, finish := range []func() error{
		func() error { return store.CompleteNotificationJob(context.Background(), jobID, "worker-b", testTime) },
		func() error {
			return store.RetryNotificationJob(context.Background(), jobID, "worker-b", "x", testTime, time.Second)
		},
		func() error { return store.FailNotificationJob(context.Background(), jobID, "worker-b", "x", testTime) },
	} {
		if err := finish(); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("non-owner finish should conflict, got %v", err)
		}
	}
}

func TestClaimHonorsBatchBoundaries(t *testing.T) {
	store := openTestStore(t)
	for index := 0; index < 4; index++ {
		seedNotificationJob(t, store, 3, testTime.Add(time.Duration(index)*time.Second))
	}
	jobs, err := store.ClaimNotificationJobs(context.Background(), "worker", 2, testTime.Add(10*time.Second), time.Minute)
	if err != nil {
		t.Fatalf("ClaimNotificationJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("claimed %d jobs want 2", len(jobs))
	}
	if jobs[0].NextAttemptAt.After(jobs[1].NextAttemptAt) {
		t.Fatalf("jobs not ordered by due time: %+v", jobs)
	}
}

func TestCanceledClaimDoesNotMutateJobs(t *testing.T) {
	store := openTestStore(t)
	jobID := seedNotificationJob(t, store, 3, testTime)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if jobs, err := store.ClaimNotificationJobs(ctx, "worker", 1, testTime, time.Minute); err == nil || len(jobs) != 0 {
		t.Fatalf("canceled claim returned jobs=%+v err=%v", jobs, err)
	}
	var status string
	if err := store.db.QueryRow(`SELECT status FROM notification_jobs WHERE id=?`, jobID).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("canceled claim mutated status=%q err=%v", status, err)
	}
}
