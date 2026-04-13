package appserver

import (
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

func parseLogTimestamp(raw string) (time.Time, string) {
	return apprefresh.ParseLogTimestamp(raw)
}
