package httpapi

import (
	"net/http"

	"github.com/11DingKing/youth-rehab-ops/internal/middleware"
)

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		a.writeError(w, r, err)
		return
	}
	result, err := a.Auth.Login(r.Context(), input.Email, input.Password)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.Auth.Logout(r.Context(), middleware.Token(r.Context())); err != nil {
		a.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
