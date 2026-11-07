package users

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// Handler serves the /v1/me endpoints.
type Handler struct {
	svc *Service
	v   *validation.Validator
	log *slog.Logger
}

// NewHandler returns a Handler over s.
func NewHandler(s *Service, v *validation.Validator, log *slog.Logger) *Handler {
	return &Handler{svc: s, v: v, log: log}
}

// Register adds the account routes to r. Reading and deleting the account and
// accepting new legal versions stay open to a driver who owes a re-acceptance:
// refusing the new terms must not block erasure (spec 8.5). Updating the
// profile is gated like any other driver route.
func (h *Handler) Register(r *router.Router) {
	r.Handle("GET /v1/me", router.Policy{Roles: router.AnyRole, LegalExempt: true}, h.get)
	r.Handle("PATCH /v1/me", router.Policy{Roles: router.AnyRole}, h.patch)
	r.Handle("DELETE /v1/me", router.Policy{Roles: router.DriverOnly, LegalExempt: true, Limit: &ratelimit.DeleteAccountUser}, h.delete)
	r.Handle("POST /v1/me/legal-acceptance", router.Policy{Roles: router.DriverOnly, LegalExempt: true}, h.acceptLegal)
	h.registerAdmin(r)
}

// patchRequest tells an absent field from an explicit null for the phone.
// Unknown fields, role and email among them, are ignored.
type patchRequest struct {
	FirstName *string                `json:"first_name"`
	LastName  *string                `json:"last_name"`
	Locale    *string                `json:"locale"`
	Phone     httpx.Nullable[string] `json:"phone"`
}

// patchValues carries the trimmed values for validation. A nil pointer is an
// absent field, so a blank value fails min=1 instead of reaching a CHECK.
type patchValues struct {
	FirstName *string `json:"first_name" validate:"omitnil,min=1,max=60"`
	LastName  *string `json:"last_name" validate:"omitnil,min=1,max=80"`
	Locale    *string `json:"locale" validate:"omitnil,oneof=es-DO en"`
	Phone     *string `json:"phone" validate:"omitnil,phone"`
}

type deleteRequest struct {
	Password secret.String `json:"password" validate:"required,max=128"`
}

type legalRequest struct {
	TermsVersion   string `json:"terms_version" validate:"required,max=64"`
	PrivacyVersion string `json:"privacy_version" validate:"required,max=64"`
}

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}

func (h *Handler) principal(w http.ResponseWriter, r *http.Request) (authz.Principal, bool) {
	p, ok := authz.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, h.log, apperr.Unauthenticated(errcodes.AuthUnauthenticated))
	}
	return p, ok
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	v, err := h.svc.Get(r.Context(), p)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, auth.ToUserJSON(v))
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeJSON[patchRequest](r, nil)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	in := PatchInput{FirstName: trimPtr(req.FirstName), LastName: trimPtr(req.LastName), Locale: trimPtr(req.Locale)}
	vals := patchValues{FirstName: in.FirstName, LastName: in.LastName, Locale: in.Locale}
	if req.Phone.Set {
		in.Phone = httpx.Nullable[string]{Set: true, Null: true}
		// An empty or blank phone means "no phone", like null.
		if v := strings.TrimSpace(req.Phone.Value); !req.Phone.Null && v != "" {
			in.Phone = httpx.Nullable[string]{Set: true, Value: v}
			vals.Phone = &v
		}
	}
	if err := httpx.Validate(h.v, vals); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.Update(r.Context(), p, in)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, auth.ToUserJSON(v))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeJSON[deleteRequest](r, h.v)
	if err == nil {
		err = h.svc.Delete(r.Context(), p, req.Password)
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.NoContent(w)
}

func (h *Handler) acceptLegal(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeJSON[legalRequest](r, h.v)
	if err == nil {
		err = h.svc.AcceptLegal(r.Context(), p, req.TermsVersion, req.PrivacyVersion)
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.NoContent(w)
}
