package vehicles

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// Handler serves the vehicle endpoints.
type Handler struct {
	svc      *Service
	v        *validation.Validator
	pageSize int
	log      *slog.Logger
}

// NewHandler returns a Handler. pageSize is the per-driver cap, reported as
// meta.page.limit on the unpaginated list.
func NewHandler(svc *Service, v *validation.Validator, pageSize int, log *slog.Logger) *Handler {
	return &Handler{svc: svc, v: v, pageSize: pageSize, log: log}
}

// Register adds the vehicle routes to r. All of them are driver-only.
func (h *Handler) Register(r *router.Router) {
	write := router.Policy{Roles: router.DriverOnly, Limit: &ratelimit.VehicleWriteUser}
	read := router.Policy{Roles: router.DriverOnly}
	r.Handle("GET /v1/vehicles", read, h.list)
	r.Handle("POST /v1/vehicles", write, h.create)
	r.Handle("GET /v1/vehicles/{id}", read, h.get)
	r.Handle("PATCH /v1/vehicles/{id}", write, h.update)
	r.Handle("DELETE /v1/vehicles/{id}", write, h.delete)
}

type vehicleDTO struct {
	ID              string    `json:"id"`
	Plate           string    `json:"plate"`
	PlateNormalized string    `json:"plate_normalized"`
	Make            string    `json:"make"`
	Model           string    `json:"model"`
	Color           string    `json:"color"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func toDTO(v Vehicle) vehicleDTO {
	return vehicleDTO{
		ID: v.PublicID.String(), Plate: v.Plate, PlateNormalized: v.PlateNormalized,
		Make: v.Make, Model: v.Model, Color: v.Color,
		CreatedAt: v.CreatedAt.UTC(), UpdatedAt: v.UpdatedAt.UTC(),
	}
}

type createRequest struct {
	Plate string `json:"plate" validate:"required,min=2,max=15"`
	Make  string `json:"make" validate:"required,min=1,max=50"`
	Model string `json:"model" validate:"required,min=1,max=50"`
	Color string `json:"color" validate:"required,min=1,max=30"`
}

type patchRequest struct {
	Plate httpx.Nullable[string] `json:"plate"`
	Make  httpx.Nullable[string] `json:"make"`
	Model httpx.Nullable[string] `json:"model"`
	Color httpx.Nullable[string] `json:"color"`
}

// patchValues carries the trimmed values of a patch for validation. A nil
// pointer is an absent field; blank values fail min=1.
type patchValues struct {
	Plate *string `json:"plate" validate:"omitnil,min=2,max=15"`
	Make  *string `json:"make" validate:"omitnil,min=1,max=50"`
	Model *string `json:"model" validate:"omitnil,min=1,max=50"`
	Color *string `json:"color" validate:"omitnil,min=1,max=30"`
}

func (h *Handler) principal(w http.ResponseWriter, r *http.Request) (authz.Principal, bool) {
	p, ok := authz.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, h.log, apperr.Unauthenticated(errcodes.AuthUnauthenticated))
	}
	return p, ok
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	vs, err := h.svc.List(r.Context(), p)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out := make([]vehicleDTO, len(vs))
	for i, v := range vs {
		out[i] = toDTO(v)
	}
	response.List(w, r, out, response.Page{Limit: h.pageSize})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeJSON[createRequest](r, nil)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	req.Plate, req.Make, req.Model, req.Color = strings.TrimSpace(req.Plate),
		strings.TrimSpace(req.Make), strings.TrimSpace(req.Model), strings.TrimSpace(req.Color)
	if err := httpx.Validate(h.v, req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.Create(r.Context(), p, CreateInput(req))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusCreated, toDTO(v))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	id, ok := httpx.PathUUID(r, "id")
	if !ok {
		httpx.WriteError(w, r, h.log, apperr.NotFound(errcodes.VehicleNotFound))
		return
	}
	v, err := h.svc.Get(r.Context(), p, id)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, toDTO(v))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	id, ok := httpx.PathUUID(r, "id")
	if !ok {
		httpx.WriteError(w, r, h.log, apperr.NotFound(errcodes.VehicleNotFound))
		return
	}
	req, err := httpx.DecodeJSON[patchRequest](r, nil)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	var nulls []validation.FieldError
	var vals patchValues
	for _, f := range []struct {
		name string
		in   httpx.Nullable[string]
		out  **string
	}{
		{"plate", req.Plate, &vals.Plate}, {"make", req.Make, &vals.Make},
		{"model", req.Model, &vals.Model}, {"color", req.Color, &vals.Color},
	} {
		switch {
		case f.in.Set && f.in.Null:
			// Every column is required: null is not a way to clear one.
			nulls = append(nulls, validation.FieldError{Field: f.name, Rule: "required"})
		case f.in.Set:
			s := strings.TrimSpace(f.in.Value)
			*f.out = &s
		}
	}
	if len(nulls) > 0 {
		httpx.WriteError(w, r, h.log, apperr.Invalid(errcodes.ValidationFailed).
			WithDetails(map[string]any{"fields": nulls}))
		return
	}
	if err := httpx.Validate(h.v, vals); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.Update(r.Context(), p, id, UpdateInput(vals))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, toDTO(v))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	id, ok := httpx.PathUUID(r, "id")
	if !ok {
		httpx.WriteError(w, r, h.log, apperr.NotFound(errcodes.VehicleNotFound))
		return
	}
	if err := h.svc.Delete(r.Context(), p, id); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.NoContent(w)
}
