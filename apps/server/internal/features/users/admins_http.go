package users

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// registerAdmin adds the admin-area routes. Both are admin-only: the router
// refuses any /v1/admin/ route with another policy.
func (h *Handler) registerAdmin(r *router.Router) {
	r.Handle("GET /v1/admin/admins", router.Policy{Roles: router.AdminOnly}, h.listAdmins)
	r.Handle("POST /v1/admin/admins", router.Policy{Roles: router.AdminOnly}, h.createAdmin)
}

// createAdminRequest ignores unknown fields: role and status cannot be set
// by the client.
type createAdminRequest struct {
	Email     string `json:"email" validate:"required,email,max=254"`
	FirstName string `json:"first_name" validate:"required,min=1,max=60"`
	LastName  string `json:"last_name" validate:"required,min=1,max=80"`
	Locale    string `json:"locale" validate:"omitempty,oneof=es-DO en"`
}

type adminUserJSON struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Status    string    `json:"status"`
	Locale    string    `json:"locale"`
	CreatedAt time.Time `json:"created_at"`
}

func toAdminJSON(a AdminUser) adminUserJSON {
	return adminUserJSON{
		ID: a.PublicID.String(), Email: a.Email, FirstName: a.FirstName, LastName: a.LastName,
		Status: a.Status, Locale: a.Locale, CreatedAt: a.CreatedAt.UTC(),
	}
}

func (h *Handler) createAdmin(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeJSON[createAdminRequest](r, nil)
	if err == nil {
		// Normalize before validating, so a blank name fails validation
		// instead of reaching a database CHECK.
		req.Email = auth.NormalizeEmail(req.Email)
		req.FirstName = strings.TrimSpace(req.FirstName)
		req.LastName = strings.TrimSpace(req.LastName)
		req.Locale = strings.TrimSpace(req.Locale)
		err = httpx.Validate(h.v, req)
	}
	var admin AdminUser
	var created bool
	if err == nil {
		admin, created, err = h.svc.CreateAdmin(r.Context(), p, CreateAdminInput(req))
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	response.OK(w, r, status, toAdminJSON(admin))
}

func (h *Handler) listAdmins(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	limit := DefaultAdminPageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			httpx.WriteError(w, r, h.log, fieldError("limit", "numeric", ""))
			return
		}
		limit = n
	}
	admins, next, err := h.svc.ListAdmins(r.Context(), p, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	items := make([]adminUserJSON, len(admins))
	for i, a := range admins {
		items[i] = toAdminJSON(a)
	}
	response.List(w, r, items, response.Page{Limit: limit, NextCursor: next})
}
