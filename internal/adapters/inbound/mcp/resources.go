package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/claudioed/wes-work-planning/internal/application/usecases"
)

// backlogURIPrefix and backlogURISuffix bracket the {pathId} segment of the
// telemetry://{pathId}/backlog resource template.
const (
	backlogURIPrefix = "telemetry://"
	backlogURISuffix = "/backlog"
)

// registerResources adds the scoped read-model resource. Per the charter,
// resources are bounded-context contracts tied to a decision, not bulk dumps:
// this one answers "what is the live backlog telemetry for this one process
// path?", backed by the same SampleBacklog read model the tool uses. Because a
// path id is open-ended (unlike a fixed enum), it is exposed as an RFC 6570
// URI template rather than one concrete resource per path.
func (d Deps) registerResources(server *mcp.Server, scopeOf func(context.Context) Scope) {
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: backlogURIPrefix + "{pathId}" + backlogURISuffix,
		Name:        "process path backlog telemetry",
		Description: "Live backlog telemetry (depth, WIP, feed mode, alarm state) for one process path.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		if !scopeAllows(scopeOf(ctx), ScopeRead) {
			return nil, fmt.Errorf("resource %q requires read scope", uri)
		}
		rawPathId, err := parseBacklogURI(uri)
		if err != nil {
			return nil, err
		}
		pathId, err := parsePathId(rawPathId)
		if err != nil {
			return nil, err
		}
		snapshot, err := d.SampleBacklog.Execute(ctx, usecases.SampleBacklogRequest{PathId: pathId})
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(toBacklogTelemetry(snapshot))
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      uri,
				MIMEType: "application/json",
				Text:     string(body),
			}},
		}, nil
	})
}

// parseBacklogURI extracts the {pathId} segment from a concrete
// telemetry://{pathId}/backlog URI. The URI is client-supplied, so a
// malformed one is rejected rather than mis-parsed.
func parseBacklogURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, backlogURIPrefix) || !strings.HasSuffix(uri, backlogURISuffix) {
		return "", fmt.Errorf("unrecognised resource uri %q: want telemetry://{pathId}/backlog", uri)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(uri, backlogURIPrefix), backlogURISuffix)
	if inner == "" || strings.Contains(inner, "/") {
		return "", fmt.Errorf("unrecognised resource uri %q: want telemetry://{pathId}/backlog", uri)
	}
	return inner, nil
}
