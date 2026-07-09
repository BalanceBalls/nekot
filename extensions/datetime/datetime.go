package datetime

import (
	"fmt"
	"time"
)

type CurrentDatetimeToolResult struct {
	Date            string `json:"date"`
	Time            string `json:"time"`
	Weekday         string `json:"weekday"`
	Timezone        string `json:"timezone"`
	UTCOffset       string `json:"utc_offset"`
	DatetimeRFC3339 string `json:"datetime_rfc3339"`
	Unix            int64  `json:"unix"`
}

func CurrentDatetimeResult(now time.Time) CurrentDatetimeToolResult {
	timezone, offsetSeconds := now.Zone()
	return CurrentDatetimeToolResult{
		Date:            now.Format("2006-01-02"),
		Time:            now.Format("15:04:05"),
		Weekday:         now.Weekday().String(),
		Timezone:        timezone,
		UTCOffset:       formatUTCOffset(offsetSeconds),
		DatetimeRFC3339: now.Format(time.RFC3339),
		Unix:            now.Unix(),
	}
}

func formatUTCOffset(offsetSeconds int) string {
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}

	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}
