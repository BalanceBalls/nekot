package datetime

import (
	"testing"
	"time"
)

func TestCurrentDatetimeResultFormatsLocalTime(t *testing.T) {
	location := time.FixedZone("AMT", 4*60*60)
	now := time.Date(2026, time.July, 9, 13, 14, 15, 0, location)

	got := CurrentDatetimeResult(now)
	if got.Date != "2026-07-09" ||
		got.Time != "13:14:15" ||
		got.Weekday != "Thursday" ||
		got.Timezone != "AMT" ||
		got.UTCOffset != "+04:00" ||
		got.DatetimeRFC3339 != "2026-07-09T13:14:15+04:00" ||
		got.Unix != now.Unix() {
		t.Fatalf("CurrentDatetimeResult() = %#v", got)
	}
}
