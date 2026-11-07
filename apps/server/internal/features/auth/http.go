package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// Handler serves the auth endpoints.
type Handler struct {
	svc     *Service
	v       *validation.Validator
	limiter ratelimit.Limiter
	log     *slog.Logger
}

// NewHandler returns a Handler over s.
func NewHandler(s *Service, v *validation.Validator, l ratelimit.Limiter, log *slog.Logger) *Handler {
	return &Handler{svc: s, v: v, limiter: l, log: log}
}

// Register adds the auth routes to r. The per-IP rules run in the router; the
// per-email rules run here, after decoding and before any hashing or lookup.
func (h *Handler) Register(r *router.Router) {
	r.Handle("POST /v1/auth/register", router.Policy{Public: true, Limit: &ratelimit.RegisterIP}, h.register)
	r.Handle("POST /v1/auth/verify", router.Policy{Public: true, Limit: &ratelimit.CodeSubmitIP}, h.verify)
	r.Handle("POST /v1/auth/verify/resend", router.Policy{Public: true, Limit: &ratelimit.CodeRequestIP}, h.resend)
	r.Handle("POST /v1/auth/password/forgot", router.Policy{Public: true, Limit: &ratelimit.CodeRequestIP}, h.forgot)
	r.Handle("POST /v1/auth/password/reset", router.Policy{Public: true, Limit: &ratelimit.CodeSubmitIP}, h.reset)
	r.Handle("POST /v1/auth/login", router.Policy{Public: true}, h.login)
	r.Handle("POST /v1/auth/refresh", router.Policy{Public: true, Limit: &ratelimit.RefreshIP}, h.refresh)
	// Logout stays open to a driver who still owes a legal re-acceptance.
	signedIn := router.Policy{Roles: router.AnyRole, LegalExempt: true}
	r.Handle("POST /v1/auth/logout", signedIn, h.logout)
	r.Handle("POST /v1/auth/logout-all", signedIn, h.logoutAll)
}

// Request bodies. Secret fields are secret.String so they never print.
type (
	registerRequest struct {
		Email                  string        `json:"email" validate:"required,email,max=254"`
		Password               secret.String `json:"password" validate:"required,min=10,max=128,password_common"`
		FirstName              string        `json:"first_name" validate:"required,min=1,max=60"`
		LastName               string        `json:"last_name" validate:"required,min=1,max=80"`
		Phone                  string        `json:"phone" validate:"omitempty,phone"`
		Locale                 string        `json:"locale" validate:"omitempty,oneof=es-DO en"`
		AcceptedTermsVersion   string        `json:"accepted_terms_version" validate:"required"`
		AcceptedPrivacyVersion string        `json:"accepted_privacy_version" validate:"required"`
	}
	verifyRequest struct {
		Email string        `json:"email" validate:"required,email,max=254"`
		Code  secret.String `json:"code" validate:"required,code6"`
	}
	forgotRequest struct {
		Email string `json:"email" validate:"required,email,max=254"`
	}
	resetRequest struct {
		Email       string        `json:"email" validate:"required,email,max=254"`
		Code        secret.String `json:"code" validate:"required,code6"`
		NewPassword secret.String `json:"new_password" validate:"required,min=10,max=128,password_common"`
	}
	resendRequest struct {
		Email string `json:"email" validate:"required,email,max=254"`
	}
	// refreshRequest carries no validation rule on purpose: an empty or
	// malformed token is a 401 auth.refresh_invalid, never a 422.
	refreshRequest struct {
		RefreshToken secret.String `json:"refresh_token"`
	}
	loginRequest struct {
		Email      string        `json:"email" validate:"required,email,max=254"`
		Password   secret.String `json:"password" validate:"required,max=128"`
		ClientKind string        `json:"client_kind" validate:"required,oneof=web mobile"`
	}
)

// normalize trims what the validator would otherwise accept as non-empty
// (blank names) or reject (padded phone) and normalizes the email.
func (q *registerRequest) normalize() {
	q.Email = NormalizeEmail(q.Email)
	q.FirstName = strings.TrimSpace(q.FirstName)
	q.LastName = strings.TrimSpace(q.LastName)
	q.Phone = strings.TrimSpace(q.Phone)
}

func (q *verifyRequest) normalize()  { q.Email = NormalizeEmail(q.Email) }
func (q *resendRequest) normalize()  { q.Email = NormalizeEmail(q.Email) }
func (q *forgotRequest) normalize()  { q.Email = NormalizeEmail(q.Email) }
func (q *resetRequest) normalize()   { q.Email = NormalizeEmail(q.Email) }
func (q *loginRequest) normalize()   { q.Email = NormalizeEmail(q.Email) }
func (q *refreshRequest) normalize() {}

// decode reads the JSON body, normalizes it, then validates it, so a blank
// name fails validation instead of reaching a database CHECK.
func decode[T any, P interface {
	*T
	normalize()
}](h *Handler, r *http.Request) (T, error) {
	req, err := httpx.DecodeJSON[T](r, nil)
	if err != nil {
		return req, err
	}
	P(&req).normalize()
	return req, httpx.Validate(h.v, req)
}

// allowEmail applies the per-email code request rule.
func (h *Handler) allowEmail(r *http.Request, email string) error {
	return h.allow(r, ratelimit.CodeRequestEmail, email)
}

// allow applies a custom-keyed rule. A limiter failure fails closed
// (spec 10.2).
func (h *Handler) allow(r *http.Request, rule ratelimit.Rule, key string) error {
	d, err := h.limiter.Allow(r.Context(), rule, key)
	if err != nil {
		return err
	}
	if !d.Allowed {
		return apperr.RateLimited(d.RetryAfter)
	}
	return nil
}

type statusDTO struct {
	Status string `json:"status"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	req, err := decode[registerRequest](h, r)
	if err == nil {
		err = h.allowEmail(r, req.Email)
	}
	if err == nil {
		var phone *string
		if req.Phone != "" {
			phone = &req.Phone
		}
		err = h.svc.Register(r.Context(), RegisterInput{
			Email: req.Email, Password: req.Password, FirstName: req.FirstName, LastName: req.LastName,
			Phone: phone, Locale: req.Locale,
			TermsVersion: req.AcceptedTermsVersion, PrivacyVersion: req.AcceptedPrivacyVersion,
		})
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusAccepted, statusDTO{Status: "verification_pending"})
}

func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	req, err := decode[verifyRequest](h, r)
	if err == nil {
		err = h.svc.Verify(r.Context(), req.Email, req.Code)
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.NoContent(w)
}

func (h *Handler) resend(w http.ResponseWriter, r *http.Request) {
	req, err := decode[resendRequest](h, r)
	if err == nil {
		err = h.allowEmail(r, req.Email)
	}
	if err == nil {
		err = h.svc.Resend(r.Context(), req.Email)
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusAccepted, statusDTO{Status: "accepted"})
}

func (h *Handler) forgot(w http.ResponseWriter, r *http.Request) {
	req, err := decode[forgotRequest](h, r)
	if err == nil {
		err = h.allowEmail(r, req.Email)
	}
	if err == nil {
		err = h.svc.Forgot(r.Context(), req.Email)
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusAccepted, statusDTO{Status: "accepted"})
}

func (h *Handler) reset(w http.ResponseWriter, r *http.Request) {
	req, err := decode[resetRequest](h, r)
	if err == nil {
		err = h.svc.Reset(r.Context(), req.Email, req.Code, req.NewPassword)
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.NoContent(w)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	req, err := decode[loginRequest](h, r)
	ip := reqctx.ClientIP(r.Context())
	// Both limits run before any lookup or hashing, on the normalized email.
	if err == nil {
		err = h.allow(r, ratelimit.LoginIPEmail, ratelimit.IPKey(ip)+"|"+req.Email)
	}
	if err == nil {
		err = h.allow(r, ratelimit.LoginEmail, req.Email)
	}
	var pair TokenPair
	if err == nil {
		pair, err = h.svc.Login(r.Context(), LoginInput{
			Email: req.Email, Password: req.Password, ClientKind: ClientKind(req.ClientKind),
			IP: ip, UserAgent: r.UserAgent(),
		})
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, newTokenResponse(pair))
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	req, err := decode[refreshRequest](h, r)
	var pair TokenPair
	if err == nil {
		pair, err = h.svc.Refresh(r.Context(), req.RefreshToken, reqctx.ClientIP(r.Context()))
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, newTokenResponse(pair))
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	h.revoke(w, r, func(p authz.Principal) error { return h.svc.Logout(r.Context(), p) })
}

func (h *Handler) logoutAll(w http.ResponseWriter, r *http.Request) {
	h.revoke(w, r, func(p authz.Principal) error {
		_, err := h.svc.LogoutAll(r.Context(), p)
		return err
	})
}

// revoke runs a logout use case for the authenticated principal.
func (h *Handler) revoke(w http.ResponseWriter, r *http.Request, run func(authz.Principal) error) {
	p, ok := authz.PrincipalFrom(r.Context())
	if !ok {
		// The router mounts these routes behind Authn; reaching here is a wiring bug.
		httpx.WriteError(w, r, h.log, errors.New("auth: logout without a principal"))
		return
	}
	if err := run(p); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.NoContent(w)
}
