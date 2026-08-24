package httpapi

import (
	"net/http"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
)

func (a *API) createReferral(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	incidentID, err := pathID(r, "id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	var input struct {
		Organization string `json:"organization"`
		Reason       string `json:"reason"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		a.writeError(w, r, err)
		return
	}
	referral, err := a.Care.Refer(r.Context(), actor, incidentID, input.Organization, input.Reason)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, referral)
}

func (a *API) acceptReferral(w http.ResponseWriter, r *http.Request) {
	a.referralDecision(w, r, true)
}

func (a *API) returnReferral(w http.ResponseWriter, r *http.Request) {
	a.referralDecision(w, r, false)
}

func (a *API) referralDecision(w http.ResponseWriter, r *http.Request, accept bool) {
	actor, err := actorFrom(r)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	var input struct {
		ExpectedVersion int64  `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		a.writeError(w, r, err)
		return
	}
	var referral domain.Referral
	if accept {
		referral, err = a.Care.AcceptReferral(r.Context(), actor, id, input.ExpectedVersion)
	} else {
		referral, err = a.Care.ReturnReferral(r.Context(), actor, id, input.ExpectedVersion, input.Reason)
	}
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, referral)
}

func (a *API) createPlan(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	referralID, err := pathID(r, "id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	plan, err := a.Care.CreatePlan(r.Context(), actor, referralID)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}

func (a *API) publishPlan(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	planID, err := pathID(r, "id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	var input struct {
		ExpectedVersion int64     `json:"expected_version"`
		Goals           string    `json:"goals"`
		Restrictions    string    `json:"restrictions"`
		Exercises       string    `json:"exercises"`
		ReviewDueAt     time.Time `json:"review_due_at"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		a.writeError(w, r, err)
		return
	}
	version, err := a.Care.PublishPlan(r.Context(), actor, planID, input.ExpectedVersion, input.Goals, input.Restrictions, input.Exercises, input.ReviewDueAt)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

func (a *API) recordFollowUp(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	planID, err := pathID(r, "id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	var input domain.FollowUp
	if err := decodeJSON(w, r, &input); err != nil {
		a.writeError(w, r, err)
		return
	}
	input.PlanID = planID
	follow, err := a.Care.FollowUp(r.Context(), actor, input)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, follow)
}

func (a *API) grantClearance(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	incidentID, err := pathID(r, "id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	var input domain.Clearance
	if err := decodeJSON(w, r, &input); err != nil {
		a.writeError(w, r, err)
		return
	}
	input.IncidentID = incidentID
	clearance, err := a.Care.Clear(r.Context(), actor, input)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, clearance)
}
