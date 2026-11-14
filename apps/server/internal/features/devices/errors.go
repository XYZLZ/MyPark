package devices

import "errors"

// ErrNotFound means no device has the requested public id.
var ErrNotFound = errors.New("devices: device not found")

// ErrDeviceNotFound is the Registry name of ErrNotFound; both are one value.
var ErrDeviceNotFound = ErrNotFound
