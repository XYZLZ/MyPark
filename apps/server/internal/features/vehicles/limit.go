package vehicles

// LimitReached reports whether a driver with active live vehicles may not
// register another one under the cap max.
func LimitReached(active, max int) bool { return active >= max }
