package devices

import "errors"

// ErrDeviceNotFound means no device has the requested public id.
var ErrDeviceNotFound = errors.New("devices: device not found")

// ErrDuplicateEvent means the device already stored an event with this id:
// a concurrent duplicate committed first.
var ErrDuplicateEvent = errors.New("devices: duplicate event id")
