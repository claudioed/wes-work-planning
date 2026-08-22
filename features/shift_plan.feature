Feature: Committing a ShiftPlan for a Process Path
  As the WES conductor
  I want to commit a ShiftPlan that splits headcount across process paths
  So that each PathPlan stays within the stations actually installed on that path

  Background:
    Given the WES Work Planning service is running

  @bdd
  Scenario: Committing a shift plan within installed-station capacity succeeds
    Given process path "pick-zone-a" has 5 installed stations
    When a ShiftPlan is committed for process path "pick-zone-a" with 3 planned heads at a rate of 120 units per hour for 8 hours
    Then the request is accepted with status 201
    And the committed PathPlan reports 3 planned heads against 5 installed stations
    And the committed PathPlan reports a planned throughput of 2880 units

  @bdd
  Scenario: Committing a shift plan that exceeds installed stations is rejected
    Given process path "pack-station-1" has 2 installed stations
    When a ShiftPlan is committed for process path "pack-station-1" with 4 planned heads at a rate of 90 units per hour for 8 hours
    Then the request is rejected with status 400
    And the problem detail title is "Planned heads exceed installed stations"
