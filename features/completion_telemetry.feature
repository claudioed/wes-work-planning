Feature: Recording completion and sampling Work Pool telemetry
  As the WES conductor
  I want completions recorded once and backlog telemetry projected from the Work Pool
  So that flow balancing reads live buffer state and a WorkUnit never completes twice

  Background:
    Given the WES Work Planning service is running

  # The telemetry read model is a projection over the Work Pool: backlog depth
  # counts pending entries, WIP counts entries released but not yet completed.
  # Recording a completion transitions the WorkUnit itself to Completed AND
  # frees its Work Pool entry's WIP slot (WorkPool.Complete) -- otherwise a
  # release-fed pool's WIP count only ever rises and the pool permanently
  # wedges shut once its WIP limit has EVER been reached, regardless of how
  # much of that work has since finished downstream (found via the e2e
  # soak_backlog_ramp scenario).
  @bdd
  Scenario: Completing a work unit updates the backlog telemetry read model
    Given a WorkUnit "wu-1" with CPT "2026-08-21T10:00:00Z" and reference "order-line-1" is enqueued to process path "pick-zone-a"
    And a WorkUnit "wu-2" with CPT "2026-08-21T16:00:00Z" and reference "order-line-2" is enqueued to process path "pick-zone-a"
    And the Work Pool telemetry for process path "pick-zone-a" reports backlog depth 2 and WIP 0
    And work is released from process path "pick-zone-a"
    And the released WorkUnit is "wu-1"
    And the Work Pool telemetry for process path "pick-zone-a" reports backlog depth 1 and WIP 1
    When the WorkUnit "wu-1" is recorded as completed
    Then the request is accepted with status 200
    And the WorkUnit in the response is in state "Completed"
    And the Work Pool telemetry for process path "pick-zone-a" reports backlog depth 1 and WIP 0
    And the Work Pool telemetry for process path "pick-zone-a" reports feed mode "ReleaseFed"
    And the Work Pool telemetry for process path "pick-zone-a" is not over its alarm threshold

  @bdd
  Scenario: Completing an already-completed work unit is rejected (no double-complete)
    Given a WorkUnit "wu-1" with CPT "2026-08-21T10:00:00Z" and reference "order-line-1" is enqueued to process path "pick-zone-a"
    And work is released from process path "pick-zone-a"
    And the WorkUnit "wu-1" is recorded as completed
    And the request is accepted with status 200
    When the WorkUnit "wu-1" is recorded as completed
    Then the request is rejected with status 409
    And the problem detail title is "Work unit already completed"
