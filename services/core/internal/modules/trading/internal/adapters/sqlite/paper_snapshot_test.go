package sqlite

import (
	"strings"
)

const paperSnapshotHeader = "bar_at,symbol,venue,timezone,interval,open,high,low,close,volume,open_at,source_available_at,fetched_at\n"

func paperSnapshotCSV(rows ...string) []byte {
	return []byte(paperSnapshotHeader + strings.Join(rows, "\n") + "\n")
}
