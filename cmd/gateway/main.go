// Gateway: the single front door that agents (Claude Code, Cursor, the investigator) connect to.
// For now it passes every MCP call straight through to the toolbox. Next it will decide who may
// call what, ask humans for approval, and log every call.
package main

import (
	"context"
	"log"
	"net/http"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const addr = "localhost:8080"

func main() {
	ctx := context.Background()

	toolbox, err := connectToolbox(ctx)
	if err != nil {
		log.Fatalf("can't start the toolbox: %v", err)
	}

	server, err := newGateway(ctx, toolbox)
	if err != nil {
		log.Fatalf("can't load the toolbox's tools: %v", err)
	}

	// Agents talk MCP to us over HTTP, at http://localhost:8080/mcp.
	http.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	log.Printf("gateway listening on http://%s/mcp", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// connectToolbox starts the toolbox and opens an MCP session with it, as its client.
// shortcut: the toolbox runs as the gateway's child process, on the same machine; step 4c moves
// it behind the proxy, on the private side.
func connectToolbox(ctx context.Context) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "gateway", Version: "0.1.0"}, nil)
	transport := &mcp.CommandTransport{Command: exec.Command("go", "run", "./cmd/toolbox")}
	return client.Connect(ctx, transport, nil)
}

// newGateway builds the MCP server agents see: it offers exactly the toolbox's tools,
// and each one forwards its calls to the toolbox.
func newGateway(ctx context.Context, toolbox *mcp.ClientSession) (*mcp.Server, error) {
	tools, err := toolbox.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "gateway", Version: "0.1.0"}, nil)
	for _, tool := range tools.Tools {
		server.AddTool(tool, forwardTo(toolbox, tool.Name))
	}
	log.Printf("loaded %d tools from the toolbox", len(tools.Tools))
	return server, nil
}

// forwardTo returns a handler that passes a call to the toolbox unchanged and returns its answer.
func forwardTo(toolbox *mcp.ClientSession, toolName string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		result, err := toolbox.CallTool(ctx, &mcp.CallToolParams{
			Name:      toolName,
			Arguments: req.Params.Arguments,
		})

		outcome := "ok"
		if err != nil {
			outcome = "failed: " + err.Error()
		} else if result.IsError {
			outcome = "tool returned an error"
		}
		log.Printf("call %s %s (%s)", toolName, outcome, time.Since(start).Round(time.Millisecond))
		return result, err
	}
}
