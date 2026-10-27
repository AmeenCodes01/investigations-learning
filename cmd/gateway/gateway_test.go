package main

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect links a client and a server in memory, so the test needs no network or database.
func connect(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverSide, clientSide := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverSide, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

type echoInput struct {
	Text string `json:"text"`
}

type echoOutput struct {
	Echo string `json:"echo"`
}

func TestGatewayForwardsCallsToToolbox(t *testing.T) {
	ctx := context.Background()

	// A stand-in toolbox with a single tool.
	fakeToolbox := mcp.NewServer(&mcp.Implementation{Name: "fake-toolbox", Version: "0"}, nil)
	mcp.AddTool(fakeToolbox, &mcp.Tool{Name: "echo", Description: "echoes text"},
		func(ctx context.Context, req *mcp.CallToolRequest, in echoInput) (*mcp.CallToolResult, echoOutput, error) {
			return nil, echoOutput{Echo: in.Text}, nil
		})

	gateway, err := newGateway(ctx, connect(t, fakeToolbox))
	if err != nil {
		t.Fatal(err)
	}
	agent := connect(t, gateway)

	tools, err := agent.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "echo" {
		t.Fatalf("agent sees %v, want just the toolbox's echo tool", tools.Tools)
	}

	result, err := agent.CallTool(ctx, &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.StructuredContent; got == nil || got.(map[string]any)["echo"] != "hi" {
		t.Errorf("result = %v, want the toolbox's answer {echo: hi}", got)
	}
}
