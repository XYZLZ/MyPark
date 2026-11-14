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

// DeviceKind is the kind of a parking-lot device (design §6.2).
type DeviceKind string

const (
	DevicePodium        DeviceKind = "podium"
	DeviceDispenser     DeviceKind = "dispenser"
	DeviceEntryBarrier  DeviceKind = "entry_barrier"
	DeviceExitBarrier   DeviceKind = "exit_barrier"
	DevicePayStation    DeviceKind = "pay_station"
	DeviceSensorGateway DeviceKind = "sensor_gateway"
)

// Device is an authenticated device (design §7.3). It is set only by the
// device HMAC middleware and is never a user Principal.
type Device struct {
	ID       int64
	PublicID uuid.UUID
	LotID    int64
	Kind     DeviceKind
}

type deviceKey struct{}

// WithDevice returns ctx carrying d.
func WithDevice(ctx context.Context, d Device) context.Context {
	return context.WithValue(ctx, deviceKey{}, d)
}

// DeviceFrom returns the device stored in ctx.
func DeviceFrom(ctx context.Context) (Device, bool) {
	d, ok := ctx.Value(deviceKey{}).(Device)
	return d, ok
}
