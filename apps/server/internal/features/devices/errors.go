package devices

import "errors"

// ErrNotFound means no device has the requested public id.
var ErrNotFound = errors.New("devices: device not found")
