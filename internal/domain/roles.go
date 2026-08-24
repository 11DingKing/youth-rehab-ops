package domain

import "slices"

type Role string

const (
	RoleCoach              Role = "coach"
	RoleSafetyOfficer      Role = "safety_officer"
	RoleGuardian           Role = "guardian"
	RoleHealthProfessional Role = "health_professional"
)

var allRoles = []Role{RoleCoach, RoleSafetyOfficer, RoleGuardian, RoleHealthProfessional}

func (r Role) Valid() bool { return slices.Contains(allRoles, r) }

func (r Role) CanReportIncident() bool {
	return r == RoleCoach || r == RoleSafetyOfficer
}

func (r Role) CanTriage() bool { return r == RoleSafetyOfficer }

func (r Role) CanManageClinicalCare() bool { return r == RoleHealthProfessional }

func (r Role) CanOverride() bool { return r == RoleSafetyOfficer || r == RoleHealthProfessional }

func (r Role) CanViewClinicalNotes() bool { return r == RoleHealthProfessional }

func RequireRole(actual Role, allowed ...Role) error {
	if slices.Contains(allowed, actual) {
		return nil
	}
	return ErrForbidden
}
