package session

import (
	"os"
	"strings"
	"testing"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
)

func TestWriteGuestConfigIncludesMCP(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Create("/repo", "deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	err = s.WriteGuestConfig(config.Model{Name: "grok"}, map[string]string{"ABOX_MCP_GH_TOKEN": "tok"}, []config.MCPServer{
		{Name: "gh", URL: "https://api.githubcopilot.com/mcp/", CredentialEnv: "ABOX_MCP_GH_TOKEN"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.GuestConfigJSON())
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "api.githubcopilot.com") || !strings.Contains(body, "ABOX_MCP_GH_TOKEN") {
		t.Fatalf("guest config missing mcp: %s", body)
	}
}
