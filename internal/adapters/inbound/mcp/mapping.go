// Package mcp is the inbound Model Context Protocol adapter: it exposes this
// bounded context to the AI ecosystem as a second driving adapter over the
// same application-layer use cases the HTTP adapter uses. It is built on the
// official MCP Go SDK and served over Streamable HTTP.
//
// Per ADR-0008 and the MCP governance charter, this package depends inward on
// the application layer (use cases and ports) and the domain only — never on
// an outbound adapter. The composition root (cmd/mcp) wires concrete
// repositories into the use cases. Tool handlers call use cases; domain
// structs never leak across the tool boundary.
package mcp

import (
	"time"

	"github.com/claudioed/wes-work-planning/internal/application/usecases"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

// backlogTelemetry is the tool-boundary DTO for get_backlog_telemetry: the
// SampleBacklog read model flattened for a model client. It is not a domain
// type — the domain BacklogSnapshot never crosses the tool boundary.
type backlogTelemetry struct {
	PathId             string `json:"pathId"`
	BacklogDepth       int    `json:"backlogDepth"`
	WIP                int    `json:"wip"`
	Mode               string `json:"mode"`
	OverAlarmThreshold bool   `json:"overAlarmThreshold"`
}

// toBacklogTelemetry maps the SampleBacklog read model into the tool-boundary
// DTO.
func toBacklogTelemetry(s usecases.BacklogSnapshot) backlogTelemetry {
	return backlogTelemetry{
		PathId:             s.PathId.String(),
		BacklogDepth:       s.BacklogDepth,
		WIP:                s.WIP,
		Mode:               s.Mode,
		OverAlarmThreshold: s.OverAlarmThreshold,
	}
}

// rebalanceRecommendation is the tool-boundary DTO for
// get_rebalance_recommendation: the RebalanceDecision read model with its
// action rendered as a stable string the model can reason about.
type rebalanceRecommendation struct {
	PathId       string `json:"pathId"`
	Action       string `json:"action"`
	BacklogDepth int    `json:"backlogDepth"`
	WIP          int    `json:"wip"`
}

// toRebalanceRecommendation maps the RebalanceDecision read model into the
// tool-boundary DTO.
func toRebalanceRecommendation(r usecases.RebalanceRecommendation) rebalanceRecommendation {
	return rebalanceRecommendation{
		PathId:       r.PathId.String(),
		Action:       r.Action.String(),
		BacklogDepth: r.BacklogDepth,
		WIP:          r.WIP,
	}
}

// releasedWork is the tool-boundary DTO for release_next_work: a minimal view
// of the work unit the release policy admitted. Only the id, CPT, and external
// reference are exposed — the domain WorkUnit aggregate (with its lifecycle
// state and timestamps) never leaks across the tool boundary.
type releasedWork struct {
	WorkUnitId string `json:"workUnitId"`
	CPT        string `json:"cpt"`
	Ref        string `json:"ref"`
}

// toReleasedWork maps a released domain WorkUnit into the narrow tool-boundary
// DTO.
func toReleasedWork(u *workunit.WorkUnit) releasedWork {
	return releasedWork{
		WorkUnitId: u.Id(),
		CPT:        u.CPT().Time().UTC().Format(time.RFC3339),
		Ref:        u.Reference(),
	}
}
