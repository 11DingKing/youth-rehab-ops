package httpapi

import (
	"net/http"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
)

func (a *API) attemptSchedule(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	var input domain.ScheduleAttempt
	if err := decodeJSON(w, r, &input); err != nil {
		a.writeError(w, r, err)
		return
	}
	input.IdempotencyKey = r.Header.Get("Idempotency-Key")
	attempt, err := a.Schedule.Attempt(r.Context(), actor, input)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	status := http.StatusCreated
	if !attempt.Allowed {
		status = http.StatusConflict
	}
	writeJSON(w, status, attempt)
}

func (a *API) grantOverride(w http.ResponseWriter, r *http.Request) {
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
		Reason string        `json:"reason"`
		TTL    time.Duration `json:"ttl_nanoseconds"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		a.writeError(w, r, err)
		return
	}
	override, err := a.Schedule.Override(r.Context(), actor, incidentID, input.Reason, input.TTL)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, override)
}
