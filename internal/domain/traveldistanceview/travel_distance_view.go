// Package traveldistanceview holds the read-only view of the travel
// distance between two coded locations, as read from facility-layout.
//
// Like productclassificationview (see its own package doc comment for the
// full rationale this mirrors), this is NOT a Kafka projection: there is
// no "distance changed" domain event to consume, and a travel distance
// between two static locations rarely changes at all. Instead this is a
// plain value returned by a synchronous outbound HTTP lookup against
// facility-layout's GET /distance?from=&to=, performed once at
// shift-plan-commit time (see ADR-0017 in this repo). The package lives in
// internal/domain because it is a pure value type with no adapter/
// framework dependency, matching this repo's convention that domain holds
// every plain read-model value regardless of how it is populated.
package traveldistanceview

// TravelDistanceView is the distance facility-layout reports between two
// LocationCodes at the moment it was looked up. Plain read-model value,
// not an aggregate with invariants — the travel graph is owned by
// facility-layout; this service only observes it to enrich a PathPlan.
//
// Known distinguishes "no distance available" (different zones, unknown
// location, or the lookup itself unavailable — Known=false, all treated
// identically: permissive/fail-open, mirroring ADR-0009's
// ProductClassificationView.Known convention) from "distance computed"
// (Known=true).
type TravelDistanceView struct {
	From      string
	To        string
	MetresM   float64
	Estimated bool
	Known     bool
}
