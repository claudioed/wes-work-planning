package mcp

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer builds the MCP server for this bounded context with every tool,
// resource, and workflow prompt registered.
func NewServer(deps Deps) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "wes-work-planning-mcp", Version: "1.0.0"},
		&mcp.ServerOptions{
			Instructions: "Flow control for the WES work-planning core: read a process path's live backlog telemetry (depth, WIP, mode, alarm state), read a Drum-Buffer-Rope rebalance recommendation (throttle vs reassign), and release the next priority-ordered work unit into a pool. Start with the balance_flow prompt.",
		},
	)

	deps.registerTools(server)
	deps.registerReportTool(server)
	deps.registerResources(server)
	deps.registerPrompts(server)

	return server
}

// Handler returns the Streamable HTTP handler for the MCP server. There is
// no inbound auth on this surface (the fleet's static-bearer identity layer
// was removed; see the adoption/removal ADR under docs/docs/adr).
func Handler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
}
