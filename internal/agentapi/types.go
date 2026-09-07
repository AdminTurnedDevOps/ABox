// Package agentapi contains provider-neutral agent stream types shared by the
// guest loop and host provider implementation.
package agentapi

import "github.com/AdminTurnedDevOps/ABox/protocol"

type Event struct {
	Type       string
	Text       string
	ToolName   string
	ToolID     string
	ToolArgs   string
	Err        error
	Usage      *protocol.UsageInfo
	StopReason string
}

type Message struct {
	Role       string
	Content    string
	ToolID     string
	ToolName   string
	ToolArgs   string
	ToolResult string
}

type ToolSchema struct {
	Name        string
	Description string
	Parameters  map[string]any
}
