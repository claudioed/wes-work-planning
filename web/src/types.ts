/** Wire types mirroring wes-work-planning's dto.go response shapes
 *  (backlogSnapshotResponseDTO / rebalanceResponseDTO / laborPlanViewDTO /
 *  workUnitResponseDTO) exactly -- kept hand-in-sync with the Go DTOs
 *  rather than code-generated for v1; revisit with openapi-typescript once
 *  the OpenAPI spec is the enforced source of truth (see order-mgmt-mfe's
 *  types.ts for the same convention). */

/** GET /paths/{pathId}/telemetry -- SampleBacklog read model. */
export interface BacklogSnapshot {
  pathId: string;
  backlogDepth: number;
  wip: number;
  mode: string;
  overAlarmThreshold: boolean;
}

/** Nested in RebalanceResponse when a labor-reassignment recommendation is
 *  attached -- committed ShiftPlan snapshot at the moment of the decision. */
export interface LaborPlanView {
  pathId: string;
  plannedHeads: number;
  plannedRate: number;
  plannedHours: number;
  observedAt: string;
}

/** GET /paths/{pathId}/rebalance -- RebalanceDecision (throttle vs
 *  reassign vs none), with the backlog/WIP snapshot it was decided from. */
export interface RebalanceRecommendation {
  pathId: string;
  action: string;
  backlogDepth: number;
  wip: number;
  laborPlan?: LaborPlanView;
}

/** WorkUnit.State enum values (internal/domain/workunit/work_unit.go) --
 *  matches the domain's State.String() exactly: "Pending" | "Released" |
 *  "Completed". */
export type WorkUnitState = "Pending" | "Released" | "Completed" | (string & {});

/** GET /work-units?reference= (and the POST /paths/{pathId}/work-units
 *  response) -- a single releasable unit of work. */
export interface WorkUnit {
  id: string;
  pathId: string;
  cpt: string;
  reference: string;
  state: WorkUnitState;
  giftWrap: boolean;
  sku?: string;
  releasedAt?: string;
  completedAt?: string;
}
