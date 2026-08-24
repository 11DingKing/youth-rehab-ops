package domain

import (
	"errors"
	"testing"
	"time"
)

func TestIncidentReportValidation(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	valid := Incident{ParticipantID: 3, Kind: InjurySprain, BodyArea: "left ankle", Description: "landed awkwardly",
		OccurredAt: now.Add(-time.Minute)}
	if err := valid.ValidateReport(now); err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Incident)
		field  string
	}{
		{"participant required", func(i *Incident) { i.ParticipantID = 0 }, "participant_id"},
		{"kind required", func(i *Incident) { i.Kind = "fracture" }, "kind"},
		{"body area required", func(i *Incident) { i.BodyArea = "  " }, "body_area"},
		{"description required", func(i *Incident) { i.Description = "" }, "description"},
		{"future occurrence rejected", func(i *Incident) { i.OccurredAt = now.Add(time.Hour) }, "occurred_at"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			err := candidate.ValidateReport(now)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("expected validation error, got %v", err)
			}
			var field *FieldError
			if !errors.As(err, &field) || field.Field != test.field {
				t.Fatalf("expected field %q, got %#v", test.field, err)
			}
		})
	}
}

func TestIncidentStateMachineFollowsOperationalLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	incident := Incident{PublicID: "inc_1", Status: IncidentReported, Version: 1, UpdatedAt: now}
	steps := []IncidentStatus{IncidentTriaged, IncidentReferred, IncidentRehabActive, IncidentReviewDue, IncidentCleared, IncidentClosed}
	for index, status := range steps {
		at := now.Add(time.Duration(index+1) * time.Minute)
		if err := incident.Transition(status, at); err != nil {
			t.Fatalf("transition to %s failed: %v", status, err)
		}
		if incident.Status != status {
			t.Fatalf("wanted %s, got %s", status, incident.Status)
		}
		if incident.Version != int64(index+2) {
			t.Fatalf("version after %s = %d", status, incident.Version)
		}
		if !incident.UpdatedAt.Equal(at) {
			t.Fatalf("updated time not advanced")
		}
	}
}

func TestIncidentStateMachineRejectsSkippedClinicalStages(t *testing.T) {
	incident := Incident{PublicID: "inc_2", Status: IncidentReported, Version: 4}
	for _, target := range []IncidentStatus{IncidentReferred, IncidentRehabActive, IncidentReviewDue, IncidentCleared, IncidentClosed} {
		before := incident
		err := incident.Transition(target, time.Now())
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("transition reported -> %s should conflict, got %v", target, err)
		}
		if incident != before {
			t.Fatalf("rejected transition mutated incident: before=%+v after=%+v", before, incident)
		}
	}
}

func TestTriagedIncidentCanReturnFromReferralAndReview(t *testing.T) {
	paths := []struct {
		from IncidentStatus
		to   IncidentStatus
	}{
		{IncidentReferred, IncidentTriaged},
		{IncidentRehabActive, IncidentTriaged},
		{IncidentReviewDue, IncidentRehabActive},
		{IncidentCleared, IncidentReviewDue},
	}
	for _, path := range paths {
		incident := Incident{PublicID: "inc_path", Status: path.from, Version: 1}
		if err := incident.Transition(path.to, time.Now()); err != nil {
			t.Errorf("expected %s -> %s to be valid: %v", path.from, path.to, err)
		}
	}
}

func TestGuardianNoticeRulesReflectRestrictionAndSeverity(t *testing.T) {
	tests := []struct {
		name     string
		incident Incident
		want     bool
	}{
		{"stopped training", Incident{Severity: SeverityLow, StopTraining: true}, true},
		{"high severity", Incident{Severity: SeverityHigh}, true},
		{"urgent severity", Incident{Severity: SeverityUrgent}, true},
		{"low observation", Incident{Severity: SeverityLow}, false},
		{"moderate observation", Incident{Severity: SeverityModerate}, false},
	}
	for _, test := range tests {
		if got := test.incident.NeedsGuardianNotice(); got != test.want {
			t.Errorf("%s: got %v want %v", test.name, got, test.want)
		}
	}
}

func TestRoleCapabilitiesRemainSeparated(t *testing.T) {
	tests := []struct {
		role     Role
		report   bool
		triage   bool
		clinical bool
		override bool
	}{
		{RoleCoach, true, false, false, false},
		{RoleSafetyOfficer, true, true, false, true},
		{RoleGuardian, false, false, false, false},
		{RoleHealthProfessional, false, false, true, true},
	}
	for _, test := range tests {
		if test.role.CanReportIncident() != test.report || test.role.CanTriage() != test.triage ||
			test.role.CanManageClinicalCare() != test.clinical || test.role.CanOverride() != test.override {
			t.Errorf("capability mismatch for %s", test.role)
		}
	}
	if !RoleHealthProfessional.CanViewClinicalNotes() || RoleSafetyOfficer.CanViewClinicalNotes() {
		t.Fatal("clinical note visibility crossed role boundary")
	}
}

func TestPageNormalizationBoundsQueries(t *testing.T) {
	tests := []struct {
		input Page
		want  Page
	}{
		{Page{}, Page{Limit: 25}},
		{Page{Limit: 10, Offset: 5}, Page{Limit: 10, Offset: 5}},
		{Page{Limit: 1000, Offset: -9}, Page{Limit: 100}},
	}
	for _, test := range tests {
		if got := test.input.Normalize(); got != test.want {
			t.Errorf("Normalize(%+v)=%+v want %+v", test.input, got, test.want)
		}
	}
}
