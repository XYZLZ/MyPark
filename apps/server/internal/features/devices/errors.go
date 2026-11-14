package devices

import "errors"

// ErrDeviceNotFound means no device has the requested public id.
var ErrDeviceNotFound = errors.New("devices: device not found")
