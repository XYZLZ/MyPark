// Package clock abstracts time so tests can control it.
package clock

import "time"

// Clock returns the current time.
type Clock interface{ Now() time.Time }

// System is the real clock, in UTC.
type System struct{}

// Now returns the current UTC time.
func (System) Now() time.Time { return time.Now().UTC() }
