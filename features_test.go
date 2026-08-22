// Package main_test contains the BDD / acceptance test suite: godog
// (Cucumber for Go) drives the Gherkin scenarios under features/ against the
// real chi router over HTTP, wired to the same in-memory adapters the
// service's own httptest suite uses. It is a black-box test — it only ever
// touches the REST API.
package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cucumber/godog"

	inboundhttp "github.com/claudioed/wes-work-planning/internal/adapters/inbound/http"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/events"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/memory"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
)

// fixedNow is the clock every scenario runs against, so the suite is
// deterministic.
var fixedNow = time.Date(2026, 8, 21, 8, 0, 0, 0, time.UTC)

// newServer builds the production router over fresh in-memory adapters and
// serves it from an httptest server, mirroring the wiring in
// internal/adapters/inbound/http/router_test.go.
func newServer() *httptest.Server {
	charges := memory.NewChargeRepo()
	plans := memory.NewPlanRepo()
	pools := memory.NewWorkPoolRepo()
	workUnits := memory.NewWorkUnitRepo()
	laborPlanViews := memory.NewLaborPlanViewRepo()
	inventoryViews := memory.NewInventoryViewRepo()
	// A nil logger keeps the buffered publisher silent during the suite.
	publisher := events.NewLogPublisher(nil)
	clock := memory.FixedClock{At: fixedNow}

	h := &inboundhttp.Handlers{
		ReceiveChargeForecast: usecases.NewReceiveChargeForecast(charges, publisher, clock),
		CommitShiftPlan:       usecases.NewCommitShiftPlan(plans, publisher, clock),
		EnqueueWorkUnit:       usecases.NewEnqueueWorkUnit(workUnits, pools, publisher, clock),
		ReleaseNextWork:       usecases.NewReleaseNextWork(pools, workUnits, publisher, clock),
		RecordCompletion:      usecases.NewRecordCompletion(workUnits, publisher, clock),
		SampleBacklog:         usecases.NewSampleBacklog(pools, publisher, clock),
		RebalanceDecision:     usecases.NewRebalanceDecision(pools, publisher, clock),
		LaborPlanView:         usecases.NewLaborPlanView(laborPlanViews),
		InventoryView:         usecases.NewInventoryView(inventoryViews),
	}

	return httptest.NewServer(inboundhttp.NewRouter(h))
}

// world is the per-scenario state: one server with its own in-memory
// adapters, plus the last HTTP response the steps made.
type world struct {
	server *httptest.Server

	// installedStations remembers the capacity a Given step declared for a
	// process path, so the ShiftPlan When step can send it.
	installedStations map[string]int

	lastStatus int
	lastBody   []byte
}

func (w *world) reset() {
	if w.server != nil {
		w.server.Close()
	}
	w.server = newServer()
	w.installedStations = make(map[string]int)
	w.lastStatus = 0
	w.lastBody = nil
}

func (w *world) close() {
	if w.server != nil {
		w.server.Close()
		w.server = nil
	}
}

// do performs a real net/http call against the httptest server and records
// the response as the "last" one for the assertion steps.
func (w *world) do(method, path string, body any) error {
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, w.server.URL+path, reader)
	if err != nil {
		return fmt.Errorf("build %s %s: %w", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.server.Client().Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s %s response: %w", method, path, err)
	}

	w.lastStatus = resp.StatusCode
	w.lastBody = raw
	return nil
}

// decodeLast unmarshals the last response body into a generic JSON object.
func (w *world) decodeLast() (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal(w.lastBody, &out); err != nil {
		return nil, fmt.Errorf("decode response %q: %w", string(w.lastBody), err)
	}
	return out, nil
}

func (w *world) stringField(field string) (string, error) {
	obj, err := w.decodeLast()
	if err != nil {
		return "", err
	}
	value, ok := obj[field].(string)
	if !ok {
		return "", fmt.Errorf("response has no string field %q: %s", field, string(w.lastBody))
	}
	return value, nil
}

func (w *world) numberField(field string) (float64, error) {
	obj, err := w.decodeLast()
	if err != nil {
		return 0, err
	}
	value, ok := obj[field].(float64)
	if !ok {
		return 0, fmt.Errorf("response has no numeric field %q: %s", field, string(w.lastBody))
	}
	return value, nil
}

// --- Given steps -------------------------------------------------------

func (w *world) serviceIsRunning() error {
	if err := w.do(http.MethodGet, "/healthz", nil); err != nil {
		return err
	}
	if w.lastStatus != http.StatusOK {
		return fmt.Errorf("healthz returned %d, want 200", w.lastStatus)
	}
	return nil
}

func (w *world) pathHasInstalledStations(pathId string, stations int) error {
	w.installedStations[pathId] = stations
	return nil
}

func (w *world) workPoolIsEmpty(pathId string) error {
	if err := w.do(http.MethodGet, "/paths/"+pathId+"/telemetry", nil); err != nil {
		return err
	}
	if w.lastStatus != http.StatusNotFound {
		return fmt.Errorf("telemetry for %q returned %d, want 404 (no Work Pool provisioned yet): %s",
			pathId, w.lastStatus, string(w.lastBody))
	}
	return nil
}

// --- When steps --------------------------------------------------------

func (w *world) commitShiftPlan(pathId string, heads int, rate, hours float64) error {
	return w.do(http.MethodPost, "/paths/"+pathId+"/plan", map[string]any{
		"plannedHeads":      heads,
		"installedStations": w.installedStations[pathId],
		"rateUnitsPerHour":  rate,
		"hours":             hours,
	})
}

func (w *world) enqueueWorkUnit(workUnitId, cpt, reference, pathId string) error {
	if err := w.do(http.MethodPost, "/paths/"+pathId+"/work-units", map[string]any{
		"workUnitId": workUnitId,
		"cpt":        cpt,
		"reference":  reference,
	}); err != nil {
		return err
	}
	if w.lastStatus != http.StatusCreated {
		return fmt.Errorf("enqueue %q returned %d, want 201: %s", workUnitId, w.lastStatus, string(w.lastBody))
	}
	return nil
}

func (w *world) enqueueNWorkUnits(count int, pathId string) error {
	base := fixedNow.Add(time.Hour)
	for i := range count {
		cpt := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		if err := w.enqueueWorkUnit(fmt.Sprintf("wu-%04d", i), cpt, fmt.Sprintf("order-line-%04d", i), pathId); err != nil {
			return err
		}
	}
	return nil
}

func (w *world) releaseWork(pathId string) error {
	return w.do(http.MethodPost, "/paths/"+pathId+"/release", nil)
}

func (w *world) releaseNWorkUnits(count int, pathId string) error {
	for i := range count {
		if err := w.releaseWork(pathId); err != nil {
			return err
		}
		if w.lastStatus != http.StatusOK {
			return fmt.Errorf("release %d returned %d, want 200: %s", i+1, w.lastStatus, string(w.lastBody))
		}
	}
	return nil
}

func (w *world) recordCompletion(workUnitId string) error {
	return w.do(http.MethodPost, "/work-units/"+workUnitId+"/complete", nil)
}

func (w *world) requestRebalanceDecision(pathId string) error {
	return w.do(http.MethodGet, "/paths/"+pathId+"/rebalance", nil)
}

// --- Then steps --------------------------------------------------------

func (w *world) requestAccepted(status int) error {
	if w.lastStatus != status {
		return fmt.Errorf("got status %d, want %d: %s", w.lastStatus, status, string(w.lastBody))
	}
	return nil
}

func (w *world) problemDetailTitle(title string) error {
	got, err := w.stringField("title")
	if err != nil {
		return err
	}
	if got != title {
		return fmt.Errorf("got problem title %q, want %q", got, title)
	}
	return nil
}

func (w *world) pathPlanReports(heads, stations int) error {
	gotHeads, err := w.numberField("plannedHeads")
	if err != nil {
		return err
	}
	gotStations, err := w.numberField("installedStations")
	if err != nil {
		return err
	}
	if int(gotHeads) != heads || int(gotStations) != stations {
		return fmt.Errorf("got %d planned heads against %d installed stations, want %d against %d",
			int(gotHeads), int(gotStations), heads, stations)
	}
	return nil
}

func (w *world) pathPlanThroughput(want float64) error {
	got, err := w.numberField("plannedThroughput")
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("got planned throughput %v, want %v", got, want)
	}
	return nil
}

func (w *world) releasedWorkUnitIs(workUnitId string) error {
	if err := w.requestAccepted(http.StatusOK); err != nil {
		return err
	}
	got, err := w.stringField("id")
	if err != nil {
		return err
	}
	if got != workUnitId {
		return fmt.Errorf("got released WorkUnit %q, want %q", got, workUnitId)
	}
	return nil
}

func (w *world) responseWorkUnitState(state string) error {
	got, err := w.stringField("state")
	if err != nil {
		return err
	}
	if got != state {
		return fmt.Errorf("got WorkUnit state %q, want %q", got, state)
	}
	return nil
}

// telemetry samples the read model without disturbing the "last response"
// used by the surrounding assertions of the scenario under test.
func (w *world) telemetry(pathId string) (map[string]any, error) {
	status, body := w.lastStatus, w.lastBody
	defer func() { w.lastStatus, w.lastBody = status, body }()

	if err := w.do(http.MethodGet, "/paths/"+pathId+"/telemetry", nil); err != nil {
		return nil, err
	}
	if w.lastStatus != http.StatusOK {
		return nil, fmt.Errorf("telemetry for %q returned %d, want 200: %s", pathId, w.lastStatus, string(w.lastBody))
	}
	return w.decodeLast()
}

func (w *world) telemetryReports(pathId string, backlogDepth, wip int) error {
	snapshot, err := w.telemetry(pathId)
	if err != nil {
		return err
	}
	gotBacklog, _ := snapshot["backlogDepth"].(float64)
	gotWIP, _ := snapshot["wip"].(float64)
	if int(gotBacklog) != backlogDepth || int(gotWIP) != wip {
		return fmt.Errorf("got backlog depth %d and WIP %d, want %d and %d",
			int(gotBacklog), int(gotWIP), backlogDepth, wip)
	}
	return nil
}

func (w *world) telemetryFeedMode(pathId, mode string) error {
	snapshot, err := w.telemetry(pathId)
	if err != nil {
		return err
	}
	got, _ := snapshot["mode"].(string)
	if got != mode {
		return fmt.Errorf("got feed mode %q, want %q", got, mode)
	}
	return nil
}

func (w *world) telemetryNotOverAlarmThreshold(pathId string) error {
	snapshot, err := w.telemetry(pathId)
	if err != nil {
		return err
	}
	if over, _ := snapshot["overAlarmThreshold"].(bool); over {
		return fmt.Errorf("Work Pool for %q is over its alarm threshold, want under it", pathId)
	}
	return nil
}

func (w *world) rebalanceAction(action string) error {
	got, err := w.stringField("action")
	if err != nil {
		return err
	}
	if got != action {
		return fmt.Errorf("got rebalance action %q, want %q", got, action)
	}
	return nil
}

func (w *world) rebalanceReports(backlogDepth, wip int) error {
	gotBacklog, err := w.numberField("backlogDepth")
	if err != nil {
		return err
	}
	gotWIP, err := w.numberField("wip")
	if err != nil {
		return err
	}
	if int(gotBacklog) != backlogDepth || int(gotWIP) != wip {
		return fmt.Errorf("got backlog depth %d and WIP %d, want %d and %d",
			int(gotBacklog), int(gotWIP), backlogDepth, wip)
	}
	return nil
}

// InitializeScenario registers every step definition and gives each scenario
// a fresh server over fresh in-memory adapters, so scenarios are independent.
func InitializeScenario(sc *godog.ScenarioContext) {
	w := &world{}

	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w.reset()
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.close()
		return ctx, nil
	})

	sc.Step(`^the WES Work Planning service is running$`, w.serviceIsRunning)
	sc.Step(`^process path "([^"]*)" has (\d+) installed stations$`, w.pathHasInstalledStations)
	sc.Step(`^the Work Pool for process path "([^"]*)" is empty$`, w.workPoolIsEmpty)

	sc.Step(`^a ShiftPlan is committed for process path "([^"]*)" with (\d+) planned heads at a rate of ([\d.]+) units per hour for ([\d.]+) hours$`, w.commitShiftPlan)
	sc.Step(`^a WorkUnit "([^"]*)" with CPT "([^"]*)" and reference "([^"]*)" is enqueued to process path "([^"]*)"$`, w.enqueueWorkUnit)
	sc.Step(`^(\d+) WorkUnits are enqueued to process path "([^"]*)"$`, w.enqueueNWorkUnits)
	sc.Step(`^work is released from process path "([^"]*)"$`, w.releaseWork)
	sc.Step(`^(\d+) WorkUnits are released from process path "([^"]*)"$`, w.releaseNWorkUnits)
	sc.Step(`^the WorkUnit "([^"]*)" is recorded as completed$`, w.recordCompletion)
	sc.Step(`^the rebalance decision is requested for process path "([^"]*)"$`, w.requestRebalanceDecision)

	sc.Step(`^the request is accepted with status (\d+)$`, w.requestAccepted)
	sc.Step(`^the request is rejected with status (\d+)$`, w.requestAccepted)
	sc.Step(`^the problem detail title is "([^"]*)"$`, w.problemDetailTitle)
	sc.Step(`^the committed PathPlan reports (\d+) planned heads against (\d+) installed stations$`, w.pathPlanReports)
	sc.Step(`^the committed PathPlan reports a planned throughput of ([\d.]+) units$`, w.pathPlanThroughput)
	sc.Step(`^the released WorkUnit is "([^"]*)"$`, w.releasedWorkUnitIs)
	sc.Step(`^the released WorkUnit is in state "([^"]*)"$`, w.responseWorkUnitState)
	sc.Step(`^the WorkUnit in the response is in state "([^"]*)"$`, w.responseWorkUnitState)
	sc.Step(`^the Work Pool telemetry for process path "([^"]*)" reports backlog depth (\d+) and WIP (\d+)$`, w.telemetryReports)
	sc.Step(`^the Work Pool telemetry for process path "([^"]*)" reports feed mode "([^"]*)"$`, w.telemetryFeedMode)
	sc.Step(`^the Work Pool telemetry for process path "([^"]*)" is not over its alarm threshold$`, w.telemetryNotOverAlarmThreshold)
	sc.Step(`^the rebalance recommendation action is "([^"]*)"$`, w.rebalanceAction)
	sc.Step(`^the rebalance recommendation reports backlog depth (\d+) and WIP (\d+)$`, w.rebalanceReports)
}

// TestFeatures runs the Gherkin acceptance suite under features/.
func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Format: "pretty",
			Paths:  []string{"features"},
			// Strict makes an undefined or pending step fail the suite instead
			// of silently skipping it.
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}
