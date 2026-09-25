package main

import "testing"

// The Kafka consumer group id must be overridable, not a fixed constant.
//
// Kafka consumer-group offsets are shared infrastructure state, not
// per-process state. When this binary runs against the same broker as the
// deployed wes-work-planning Deployment -- which is exactly what the
// e2e-tests harness does, since the fleet has ONE broker platform-wide --
// a hardcoded group id makes both processes join the SAME group. With one
// partition per topic, Kafka's rebalance protocol hands that partition to
// only one member, and the loser silently consumes nothing.
//
// That failure is invisible to any test that constructs the consumer
// directly: it only appears as a scenario timing out waiting for a
// projection that never arrives. Hence this test asserts the resolution
// rule itself.
func TestConsumerGroupID(t *testing.T) {
	t.Run("defaults to the service name so deployments keep one shared group", func(t *testing.T) {
		if got := consumerGroupID(""); got != defaultConsumerGroup {
			t.Fatalf("consumerGroupID(%q) = %q, want %q", "", got, defaultConsumerGroup)
		}
	})

	t.Run("an explicit override wins so a second process can isolate itself", func(t *testing.T) {
		const override = "wes-work-planning-e2e-12345"
		if got := consumerGroupID(override); got != override {
			t.Fatalf("consumerGroupID(%q) = %q, want %q", override, got, override)
		}
	})

	t.Run("whitespace-only is treated as unset rather than a valid group", func(t *testing.T) {
		if got := consumerGroupID("   "); got != defaultConsumerGroup {
			t.Fatalf("consumerGroupID(%q) = %q, want %q", "   ", got, defaultConsumerGroup)
		}
	})
}
