package kafka_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/claudioed/wes-work-planning/internal/adapters/kafka/envelope"
	outboundkafka "github.com/claudioed/wes-work-planning/internal/adapters/outbound/kafka"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/memory"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/productclassification"
	"github.com/claudioed/wes-work-planning/internal/domain/productclassificationview"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

// fakeWriter is a stub outboundkafka.Writer so these tests never hit a
// real broker; it captures every message written for inspection.
type fakeWriter struct {
	msgs   []kafkago.Message
	err    error
	closed bool
}

func (f *fakeWriter) WriteMessages(_ context.Context, msgs ...kafkago.Message) error {
	if f.err != nil {
		return f.err
	}
	f.msgs = append(f.msgs, msgs...)
	return nil
}

func (f *fakeWriter) Close() error {
	f.closed = true
	return nil
}

// fakeClassificationLookup is a stub ports.ProductClassificationLookup.
type fakeClassificationLookup struct {
	views map[string]productclassificationview.ProductClassificationView
	err   error
}

func (f *fakeClassificationLookup) GetClassification(_ context.Context, sku string) (productclassificationview.ProductClassificationView, error) {
	if f.err != nil {
		return productclassificationview.ProductClassificationView{}, f.err
	}
	return f.views[sku], nil
}

func newReleasedWorkUnit(t *testing.T, id, sku string) *memory.WorkUnitRepo {
	t.Helper()
	workUnits := memory.NewWorkUnitRepo()
	pathId, err := shared.NewPathId("pick-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cpt := shared.NewCPT(time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC))
	unit, err := workunit.NewWorkUnit(id, pathId, cpt, "ref-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	unit.SetSKU(sku)
	if err := unit.Release(time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := workUnits.Save(context.Background(), unit); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return workUnits
}

func decodeWorkReleasedData(t *testing.T, msg kafkago.Message) map[string]any {
	t.Helper()
	var env envelope.Envelope
	if err := json.Unmarshal(msg.Value, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	return data
}

func TestPublisher_WorkReleased_HazmatSKU_AppendsRequiredCapability(t *testing.T) {
	workUnits := newReleasedWorkUnit(t, "wu-1", "sku-hazmat")
	lookup := &fakeClassificationLookup{views: map[string]productclassificationview.ProductClassificationView{
		"sku-hazmat": {SKU: "sku-hazmat", HandlingTags: []string{"Hazmat"}, Known: true},
	}}
	writer := &fakeWriter{}
	pub := outboundkafka.NewPublisherWithWriter(writer, workUnits, lookup, func() string { return "evt-1" })

	pathId, _ := shared.NewPathId("pick-a")
	event := shared.NewWorkReleased("wu-1", pathId, time.Now())
	if err := pub.Publish(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(writer.msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(writer.msgs))
	}
	data := decodeWorkReleasedData(t, writer.msgs[0])

	caps, ok := data["required_capabilities"].([]any)
	if !ok {
		t.Fatalf("expected required_capabilities in payload, got %v", data)
	}
	if len(caps) != 1 || caps[0] != "hazmat" {
		t.Fatalf("got required_capabilities %v, want [hazmat]", caps)
	}
	if _, ok := data["fragile"]; ok {
		t.Fatalf("did not expect fragile field for a non-fragile SKU, got %v", data)
	}
}

func TestPublisher_WorkReleased_FragileSKU_SetsFragileTrue(t *testing.T) {
	workUnits := newReleasedWorkUnit(t, "wu-2", "sku-fragile")
	lookup := &fakeClassificationLookup{views: map[string]productclassificationview.ProductClassificationView{
		"sku-fragile": {SKU: "sku-fragile", HandlingTags: []string{"Fragile"}, Known: true},
	}}
	writer := &fakeWriter{}
	pub := outboundkafka.NewPublisherWithWriter(writer, workUnits, lookup, func() string { return "evt-2" })

	pathId, _ := shared.NewPathId("pick-a")
	event := shared.NewWorkReleased("wu-2", pathId, time.Now())
	if err := pub.Publish(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := decodeWorkReleasedData(t, writer.msgs[0])
	fragile, ok := data["fragile"].(bool)
	if !ok || !fragile {
		t.Fatalf("expected fragile=true, got %v", data)
	}
	if _, ok := data["required_capabilities"]; ok {
		t.Fatalf("did not expect required_capabilities for a non-hazmat SKU, got %v", data)
	}
}

func TestPublisher_WorkReleased_UnclassifiedSKU_OmitsBothFields(t *testing.T) {
	workUnits := newReleasedWorkUnit(t, "wu-3", "sku-plain")
	lookup := &fakeClassificationLookup{views: map[string]productclassificationview.ProductClassificationView{
		"sku-plain": {SKU: "sku-plain", Known: true}, // classified, but no relevant tags
	}}
	writer := &fakeWriter{}
	pub := outboundkafka.NewPublisherWithWriter(writer, workUnits, lookup, func() string { return "evt-3" })

	pathId, _ := shared.NewPathId("pick-a")
	event := shared.NewWorkReleased("wu-3", pathId, time.Now())
	if err := pub.Publish(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := decodeWorkReleasedData(t, writer.msgs[0])
	if _, ok := data["required_capabilities"]; ok {
		t.Fatalf("did not expect required_capabilities, got %v", data)
	}
	if _, ok := data["fragile"]; ok {
		t.Fatalf("did not expect fragile, got %v", data)
	}
}

func TestPublisher_WorkReleased_LookupUnavailable_OmitsBothFieldsAndStillPublishes(t *testing.T) {
	workUnits := newReleasedWorkUnit(t, "wu-4", "sku-hazmat")
	lookup := &fakeClassificationLookup{err: errors.New("inventory-storage unreachable")}
	writer := &fakeWriter{}
	pub := outboundkafka.NewPublisherWithWriter(writer, workUnits, lookup, func() string { return "evt-4" })

	pathId, _ := shared.NewPathId("pick-a")
	event := shared.NewWorkReleased("wu-4", pathId, time.Now())
	if err := pub.Publish(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: publish must not fail on a classification lookup error, got %v", err)
	}

	data := decodeWorkReleasedData(t, writer.msgs[0])
	if _, ok := data["required_capabilities"]; ok {
		t.Fatalf("did not expect required_capabilities on lookup failure, got %v", data)
	}
	if _, ok := data["fragile"]; ok {
		t.Fatalf("did not expect fragile on lookup failure, got %v", data)
	}
	// Base fields must still be present — enrichment failure is not
	// allowed to degrade the existing contract.
	if data["work_unit_id"] != "wu-4" {
		t.Fatalf("expected work_unit_id wu-4 to still be published, got %v", data)
	}
}

func TestPublisher_WorkReleased_NilClassificationsPort_OmitsBothFields(t *testing.T) {
	workUnits := newReleasedWorkUnit(t, "wu-5", "sku-hazmat")
	writer := &fakeWriter{}
	pub := outboundkafka.NewPublisherWithWriter(writer, workUnits, nil, func() string { return "evt-5" })

	pathId, _ := shared.NewPathId("pick-a")
	event := shared.NewWorkReleased("wu-5", pathId, time.Now())
	if err := pub.Publish(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := decodeWorkReleasedData(t, writer.msgs[0])
	if _, ok := data["required_capabilities"]; ok {
		t.Fatalf("did not expect required_capabilities with a nil classifications port, got %v", data)
	}
	if _, ok := data["fragile"]; ok {
		t.Fatalf("did not expect fragile with a nil classifications port, got %v", data)
	}
}

func TestPublisher_WorkReleased_EmptySKU_SkipsLookupEntirely(t *testing.T) {
	workUnits := newReleasedWorkUnit(t, "wu-6", "") // no SKU known
	lookup := &fakeClassificationLookup{views: map[string]productclassificationview.ProductClassificationView{
		"": {SKU: "", HandlingTags: []string{"Hazmat"}, Known: true}, // would be wrong to ever hit this
	}}
	writer := &fakeWriter{}
	pub := outboundkafka.NewPublisherWithWriter(writer, workUnits, lookup, func() string { return "evt-6" })

	pathId, _ := shared.NewPathId("pick-a")
	event := shared.NewWorkReleased("wu-6", pathId, time.Now())
	if err := pub.Publish(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := decodeWorkReleasedData(t, writer.msgs[0])
	if _, ok := data["required_capabilities"]; ok {
		t.Fatalf("did not expect required_capabilities for an empty SKU, got %v", data)
	}
}

func TestPublisher_WorkReleased_PermissiveLookup_OmitsBothFields(t *testing.T) {
	workUnits := newReleasedWorkUnit(t, "wu-7", "sku-hazmat")
	writer := &fakeWriter{}
	pub := outboundkafka.NewPublisherWithWriter(writer, workUnits, productclassification.NewPermissiveLookup(), func() string { return "evt-7" })

	pathId, _ := shared.NewPathId("pick-a")
	event := shared.NewWorkReleased("wu-7", pathId, time.Now())
	if err := pub.Publish(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := decodeWorkReleasedData(t, writer.msgs[0])
	if _, ok := data["required_capabilities"]; ok {
		t.Fatalf("did not expect required_capabilities under PermissiveLookup, got %v", data)
	}
	if _, ok := data["fragile"]; ok {
		t.Fatalf("did not expect fragile under PermissiveLookup, got %v", data)
	}
}

func TestPublisher_WorkReleased_BaseFieldsStillPopulated(t *testing.T) {
	workUnits := newReleasedWorkUnit(t, "wu-8", "sku-plain")
	writer := &fakeWriter{}
	pub := outboundkafka.NewPublisherWithWriter(writer, workUnits, productclassification.NewPermissiveLookup(), func() string { return "evt-8" })

	pathId, _ := shared.NewPathId("pick-a")
	event := shared.NewWorkReleased("wu-8", pathId, time.Now())
	if err := pub.Publish(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := decodeWorkReleasedData(t, writer.msgs[0])
	if data["path_id"] != "pick-a" || data["work_unit_id"] != "wu-8" || data["ref"] != "ref-1" {
		t.Fatalf("unexpected base fields: %v", data)
	}
}
