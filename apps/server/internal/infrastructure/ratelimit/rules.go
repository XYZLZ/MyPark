package ratelimit

import "time"

// Rules from spec §10.1. Auth rules fail closed; the vehicle write rule fails open.
var (
	LoginIPEmail     = Rule{Name: "login_ip_email", Limit: 5, Window: time.Minute, Key: KeyCustom, Prefix: "login:"}
	LoginEmail       = Rule{Name: "login_email", Limit: 20, Window: time.Hour, Key: KeyCustom, Prefix: "login_e:"}
	RegisterIP       = Rule{Name: "register_ip", Limit: 3, Window: time.Minute, Key: KeyIP, Prefix: "reg:"}
	CodeRequestIP    = Rule{Name: "code_request_ip", Limit: 3, Window: time.Minute, Key: KeyIP, Prefix: "code:"}
	CodeRequestEmail = Rule{Name: "code_request_email", Limit: 5, Window: time.Hour, Key: KeyCustom, Prefix: "code_e:"}
	CodeSubmitIP     = Rule{Name: "code_submit_ip", Limit: 10, Window: time.Minute, Key: KeyIP, Prefix: "codesub:"}
	RefreshIP        = Rule{Name: "refresh_ip", Limit: 60, Window: time.Minute, Key: KeyIP, Prefix: "refresh:"}
	VehicleWriteUser = Rule{Name: "vehicle_write_user", Limit: 20, Window: time.Minute, Key: KeyUser, Prefix: "veh:", FailOpen: true}
)

// DeleteAccountUser throttles DELETE /v1/me. The endpoint verifies the
// password, so a stolen access token must not be able to brute-force it. It
// fails closed. Not in spec 10.1: recorded as a spec update (A20 ruling).
var DeleteAccountUser = Rule{Name: "delete_account_user", Limit: 5, Window: 15 * time.Minute, Key: KeyUser, Prefix: "delme:"}
