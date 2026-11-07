// Package authz carries the authenticated principal through the request
// context. Authorization is deny-by-default; the role never comes from client input.
package authz

import (
	"context"
	"slices"

	"github.com/google/uuid"
)

// Role is a user role.
type Role string

const (
	RoleDriver Role = "driver"
	RoleAdmin  Role = "admin"
)

// ParseRole returns the Role for s and whether s is a known role.
func ParseRole(s string) (Role, bool) {
	switch r := Role(s); r {
	case RoleDriver, RoleAdmin:
		return r, true
	}
	return "", false
}

// Principal is the authenticated caller.
type Principal struct {
	UserID          int64
	UserPublicID    uuid.UUID
	Role            Role
	SessionID       int64
	SessionPublicID uuid.UUID
	LegalPending    bool
}

// Is reports whether the principal has one of roles.
func (p Principal) Is(roles ...Role) bool { return slices.Contains(roles, p.Role) }

type ctxKey struct{}

// WithPrincipal returns ctx carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// PrincipalFrom returns the principal stored in ctx.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}
