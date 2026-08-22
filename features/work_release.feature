Feature: Continuous, priority-ordered Release of work into a Work Pool
  As the WES conductor
  I want work released waveless in CPT priority order, at most once per WorkUnit
  So that the most urgent Charge clears first and no unit is ever handed out twice

  Background:
    Given the WES Work Planning service is running

  @bdd
  Scenario: Releasing work returns the earliest-CPT pending work unit first
    Given the Work Pool for process path "pick-zone-a" is empty
    And a WorkUnit "wu-late" with CPT "2026-08-21T18:00:00Z" and reference "order-line-1" is enqueued to process path "pick-zone-a"
    And a WorkUnit "wu-early" with CPT "2026-08-21T10:00:00Z" and reference "order-line-2" is enqueued to process path "pick-zone-a"
    And a WorkUnit "wu-mid" with CPT "2026-08-21T14:00:00Z" and reference "order-line-3" is enqueued to process path "pick-zone-a"
    And the Work Pool telemetry for process path "pick-zone-a" reports backlog depth 3 and WIP 0
    When work is released from process path "pick-zone-a"
    Then the request is accepted with status 200
    And the released WorkUnit is "wu-early"
    And the released WorkUnit is in state "Released"
    When work is released from process path "pick-zone-a"
    Then the released WorkUnit is "wu-mid"
    And the Work Pool telemetry for process path "pick-zone-a" reports backlog depth 1 and WIP 2

  @bdd
  Scenario: A work unit cannot be released twice (at-most-once assignment)
    Given a WorkUnit "wu-only" with CPT "2026-08-21T12:00:00Z" and reference "order-line-9" is enqueued to process path "pick-zone-a"
    And work is released from process path "pick-zone-a"
    And the released WorkUnit is "wu-only"
    When work is released from process path "pick-zone-a"
    Then the request is rejected with status 409
    And the problem detail title is "Work pool is empty"
    And the Work Pool telemetry for process path "pick-zone-a" reports backlog depth 0 and WIP 1
