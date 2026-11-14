package availability

import "github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"

// AdmitInput is what the entry decision reads under the lot lock (design
// §9.2). Capacity is the lot's; OutOfService spaces are subtracted from it;
// Pending counts entries accepted in the last 60 s that have not passed the
// barrier; Holds are the spaces reserved right now.
type AdmitInput struct {
	LotClosed                                        bool
	Capacity, OutOfService, Occupied, Pending, Holds int
}

// CanAdmitDriveUp decides a credential at the entry (spec §5.2 rules 1 and 5):
// a closed lot denies with GateLotClosed, whatever room it has; a lot with no
// effective space left denies with GateLotFull.
func CanAdmitDriveUp(in AdmitInput) (bool, errcodes.Code) {
	if in.LotClosed {
		return false, errcodes.GateLotClosed
	}
	if in.Capacity-in.OutOfService-(in.Occupied+in.Pending)-in.Holds <= 0 {
		return false, errcodes.GateLotFull
	}
	return true, ""
}
