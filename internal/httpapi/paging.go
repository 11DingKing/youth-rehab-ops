package httpapi

import (
	"net/http"
	"strconv"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
)

func repositoryPage(r *http.Request) domain.Page {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return domain.Page{Limit: limit, Offset: offset}.Normalize()
}
