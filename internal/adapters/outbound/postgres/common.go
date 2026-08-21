package postgres

import "time"

type forecastRow struct {
	ReceivedAt time.Time
	Buckets    []byte
}

func unixNanoToTime(nanos int64) time.Time {
	return time.Unix(0, nanos).UTC()
}
