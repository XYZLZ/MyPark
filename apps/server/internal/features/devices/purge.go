package devices

import (
	"context"
	"fmt"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// PurgeEvents deletes up to batch device events older than 12 months (the
// floor is enforced inside the SQL function, M2C-43) and returns how many it
// deleted. The worker calls it until it returns less than batch.
func PurgeEvents(ctx context.Context, db database.DBTX, batch int32) (int64, error) {
	n, err := dbgen.New(db).PurgeDeviceEvents(ctx, batch)
	if err != nil {
		return 0, fmt.Errorf("devices: purge events: %w", err)
	}
	return n, nil
}
