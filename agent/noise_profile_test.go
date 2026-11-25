package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/egress"
)

func TestRenderNoiseProfileOff(t *testing.T) {
	for _, p := range []egress.Profile{{}, {Name: "off"}, {Name: "off", RPS: 5}} {
		if got := renderNoiseProfile(p); got != "" {
			t.Errorf("renderNoiseProfile(%+v) = %q, want empty", p, got)
		}
	}
}

func TestRenderNoiseProfileTable(t *testing.T) {
	for _, c := range []struct {
		name  string
		rps   int
		conc  int
		delay int
	}{
		{"quiet", 2, 1, 500},
		{"balanced", 5, 2, 200},
		{"aggressive", 20, 4, 50},
	} {
		got := renderNoiseProfile(egress.Profiles[c.name])
		for _, want := range []string{
			noiseProfileHeader + c.name,
			fmt.Sprintf("at most %d requests per second per RPC host", c.rps),
			fmt.Sprintf("at least %d ms between requests", c.delay),
			"one getProgramAccounts page or simulation batch at a time",
			"on 429 or 503 stop and back off",
			"ARTEX paces its own platform HTTP traffic (http custom tools, the enrich probe) with this profile. With traffic capture on, Bash commands that use the capture proxy (curl, wget, python-requests, node, the Solana CLIs) are paced too, by rate and jitter. These limits remain the brake: do not rely on the proxy.",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: missing %q in:\n%s", c.name, want, got)
			}
		}
	}
}

// The default must stay identical: with no profile the block is empty and none of
// the four prompts that can launch Bash gains any text.
func TestPromptBuildersNoiseBlock(t *testing.T) {
	builders := map[string]func() string{
		"planner":   func() string { return plannerSystem("goal", "/data", "/work") },
		"worker":    func() string { return workerSystem("", "", "/data", "/run") },
		"mainagent": func() string { return mainAgentSystem("goal", "/data", "/work") },
		"chat":      func() string { return chatSystem("auto", "/data", "/work") },
	}
	prev := egress.CurrentProfile()
	t.Cleanup(func() { egress.SetProfile(prev) })
	// The block follows the current profile, not the environment.
	t.Setenv("ARTEX_NOISE_PROFILE", "aggressive")
	egress.SetProfile(egress.Profile{Name: "off"})
	for name, build := range builders {
		if got := build(); strings.Contains(got, noiseProfileHeader) {
			t.Errorf("%s with profile off contains the noise block", name)
		}
	}
	for _, profile := range []string{"quiet", "balanced"} {
		egress.SetProfile(egress.Profiles[profile])
		for name, build := range builders {
			got := build()
			if !strings.Contains(got, noiseProfileHeader+profile) || strings.Contains(got, noiseProfileHeader+"aggressive") {
				t.Errorf("%s after SetProfile(%s) is missing its block or shows the env profile", name, profile)
			}
		}
	}
}
