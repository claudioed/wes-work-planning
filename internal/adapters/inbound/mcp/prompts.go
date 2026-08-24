package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// balanceFlowSOP is the operational standard-operating-procedure the
// balance_flow prompt hands to the model. Per the charter, prompts encode how
// to interpret results, when to act, and when to escalate — they standardise
// agent behaviour across clients rather than leaving procedure implicit.
const balanceFlowSOP = `You are balancing flow for a WES process path (Drum-Buffer-Rope, with CPT as the drum). Use only the MCP tools; never assume pool state.

Procedure:
1. Call get_backlog_telemetry(pathId) to read the live read model: backlogDepth (pending work), wip (released/outstanding work), mode (ReleaseFed or FlowFed), and overAlarmThreshold.
2. Call get_rebalance_recommendation(pathId) for the flow-balancing action. It returns one of:
   - NoActionNeeded: backlog is within tolerance. Do nothing.
   - ThrottleUpstream: a flow-fed path is over its alarm threshold. Slow admission UPSTREAM of this path; do not release more into it.
   - ReassignLabor: a release-fed path is saturated at its WIP limit with work still backlogged. Flag headcount reassignment; releasing more will be rejected by the WIP invariant.
3. Release work only when it will actually help: on a healthy or under-fed path with backlog waiting, call release_next_work(pathId) to admit the next priority-ordered (earliest-CPT) unit. Re-read telemetry after releasing to confirm the effect.

Interpretation:
- A deep backlog with low WIP on a release-fed path means work is waiting to be admitted — release is appropriate (until the WIP limit).
- overAlarmThreshold on a flow-fed path is a signal to throttle the source, NOT to release more here (you do not control a flow-fed path's admission volume).
- A ReassignLabor recommendation is a people problem, not a release problem: releasing more is a no-op at best and rejected at worst.

When to escalate to a human: the same path stays over its alarm threshold across repeated checks despite throttling, or release_next_work keeps being rejected by the WIP limit while backlog grows (capacity, not admission, is the constraint).

Done means: for the path you were asked about, you have reported its backlog telemetry, the rebalance recommendation and why, any release you performed and its effect, and any escalation you are raising. Do not release work that the recommendation shows will not help.`

// registerPrompts adds the workflow prompts (operational SOPs).
func (d Deps) registerPrompts(server *mcp.Server, scopeOf func(context.Context) Scope) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "balance_flow",
		Description: "Standard operating procedure for balancing flow on a process path: read telemetry, decide when to throttle, release, or reassign, and know when to escalate.",
	}, func(ctx context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "How to balance flow on a process path: read backlog telemetry, apply the rebalance recommendation, release only when it helps, and know when to escalate.",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: balanceFlowSOP},
			}},
		}, nil
	})
}
