package version

import "time"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
	StartedAt = time.Now().UTC()
)
