// Package report holds the work-planning "Release Throughput & Backlog
// Health" read model: the shapes of the analytical report the data product
// serves, the query that selects it, and the outbound ports the writer and
// reader adapters implement. It is a read-model region that depends on
// nothing else in this module — the OLTP domain and application layers must
// not import it, and it must not import them (ADR-0011).
package report

import "time"

// Granularity is the time-bucket resolution a report is rolled up to. Only
// hourly buckets are modelled for this round.
type Granularity string

const (
	// GranularityHour rolls rows up into UTC hour buckets.
	GranularityHour Granularity = "hour"
)

// RowKey identifies a single throughput row: the process path (PathId) and
// the UTC hour bucket the row aggregates. HourBucket is the bucket start,
// truncated to the hour in UTC.
type RowKey struct {
	PathId     string
	HourBucket time.Time
}

// Row is one aggregated "Release Throughput & Backlog Health" row for a
// (pathId, hourBucket) key. Each counter is the number of the corresponding
// analytics event that fell in this bucket for this path.
type Row struct {
	Key RowKey
	// WorkReleased is the number of WorkReleased events in this bucket: how
	// much work the release policy admitted into the path.
	WorkReleased int
	// WorkUnitCompleted is the number of WorkUnitCompleted events in this
	// bucket: how much released work the path finished.
	WorkUnitCompleted int
	// BacklogThresholdBreached is the number of BacklogThresholdBreached
	// events in this bucket: how often the path's backlog crossed its alarm
	// threshold.
	BacklogThresholdBreached int
	// PathThrottled is the number of PathThrottled events in this bucket:
	// how often flow balancing throttled upstream release into the path.
	PathThrottled int
	// RateDeviationDetected is the number of RateDeviationDetected events in
	// this bucket: how often the path's actual throughput diverged from plan.
	RateDeviationDetected int
}

// ThroughputReport is the full result of a report query: the matching rows.
type ThroughputReport struct {
	Rows []Row
}

// ReportQuery selects and filters the rows a report covers. From is
// inclusive and To is exclusive, both compared against a row's HourBucket.
// PathId is an optional exact-match filter (empty means "no filter on this
// dimension").
type ReportQuery struct {
	From        time.Time
	To          time.Time
	PathId      string
	Granularity Granularity
}
