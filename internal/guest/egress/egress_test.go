package egress

import (
	"context"
	"testing"
)

func TestAllowedHosts(t *testing.T) {
	if !Allowed("api.x.ai") || !Allowed("api.openai.com") || !Allowed("api.anthropic.com") {
		t.Fatal("expected LLM hosts allowed")
	}
	if Allowed("example.com") || Allowed("169.254.169.254") {
		t.Fatal("expected arbitrary hosts denied")
	}
}

func TestDialDeniesOtherHosts(t *testing.T) {
	tr := Transport()
	_, err := tr.DialContext(context.Background(), "tcp", "example.com:443")
	if err == nil {
		t.Fatal("expected deny")
	}
}

func TestAllowAddsMCPHost(t *testing.T) {
	t.Cleanup(ResetForTest)
	ResetForTest()
	if Allowed("mcp.example.com") {
		t.Fatal("expected mcp host denied before Allow")
	}
	Allow("mcp.example.com")
	if !Allowed("mcp.example.com") {
		t.Fatal("expected mcp host allowed after Allow")
	}
	if Allowed("example.com") {
		t.Fatal("expected unrelated host still denied")
	}
}
