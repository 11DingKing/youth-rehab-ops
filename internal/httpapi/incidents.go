package httpapi

import (
	"net/http"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/audit"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
	"github.com/11DingKing/youth-rehab-ops/internal/service"
)

func (a *API) reportIncident(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	var input service.ReportIncidentInput
	if err := decodeJSON(w, r, &input); err != nil {
		a.writeError(w, r, err)
		return
	}
	input.IdempotencyKey = r.Header.Get("Idempotency-Key")
	incident, err := a.Incidents.Report(r.Context(), actor, input)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, incident)
}

func (a *API) getIncident(w http.ResponseWriter, r *http.Request) {
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
	incident, err := a.Incidents.Get(r.Context(), actor, id)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (a *API) correctIncident(w http.ResponseWriter, r *http.Request) {
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
	var input repository.IncidentCorrection
	if err := decodeJSON(w, r, &input); err != nil {
		a.writeError(w, r, err)
		return
	}
	incident, err := a.Incidents.Correct(r.Context(), actor, id, input)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (a *API) triageIncident(w http.ResponseWriter, r *http.Request) {
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
	var input service.TriageIncidentInput
	if err := decodeJSON(w, r, &input); err != nil {
		a.writeError(w, r, err)
		return
	}
	incident, err := a.Incidents.Triage(r.Context(), actor, id, input)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	actor, err := actorFrom(r)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	if !actor.Role.CanViewClinicalNotes() && !actor.Role.CanTriage() {
		_ = a.Store.AppendAudit(r.Context(), audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "audit.viewed",
			ObjectType: "incident", ObjectID: r.PathValue("id"), Result: audit.Denied, Reason: "role_not_authorized",
			RequestID: actor.RequestID, CreatedAt: time.Now().UTC()})
		a.writeError(w, r, domain.ErrForbidden)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	incident, err := a.Incidents.Get(r.Context(), actor, id)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	result, err := a.Store.ListAudit(r.Context(), "incident", incident.PublicID, repositoryPage(r))
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	audit.AppendBestEffort(r.Context(), a.Store, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "audit.viewed",
		ObjectType: "incident", ObjectID: incident.PublicID, Result: audit.Succeeded, RequestID: actor.RequestID, CreatedAt: time.Now().UTC()})
	writeJSON(w, http.StatusOK, result)
}
