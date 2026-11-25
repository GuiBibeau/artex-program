package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/artex/egress"
)

// noiseProfileHeader starts the code-owned noise block. The block is appended to
// every agent prompt that can launch Bash; with no profile it is empty, so the
// prompts stay byte-for-byte unchanged by default.
const noiseProfileHeader = "[Noise profile: "

// noiseProfileBlock returns the low-and-slow guidance for the current noise
// profile (the noise_profile setting, else ARTEX_NOISE_PROFILE), or "" when the
// profile is off. It is read at each prompt build, so a changed setting shows in
// the next prompt. The text is code-owned and carries no target data.
func noiseProfileBlock() string { return renderNoiseProfile(egress.CurrentProfile()) }

// renderNoiseProfile renders the block for one profile. It is pure: tests cover
// every profile without touching the environment.
func renderNoiseProfile(p egress.Profile) string {
	if p.Name == "" || p.Name == "off" || p.RPS <= 0 {
		return ""
	}
	rps := int(p.RPS + 0.5)
	if rps < 1 {
		rps = 1
	}
	conc := p.MaxPerHost
	if conc < 1 {
		conc = 1
	}
	// The profile's delay between requests is 1/RPS (500 ms, 200 ms, 50 ms).
	delayMS := int((time.Second / time.Duration(rps)) / time.Millisecond)
	inFlight := fmt.Sprintf("%d requests", conc)
	if conc == 1 {
		inFlight = "1 request"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s. Operator setting, not a target fact.]\n", noiseProfileHeader, p.Name)
	b.WriteString("Work at a reduced rate. RPC nodes rate-limit by IP, and a 429 from the cluster's endpoint costs the task its evidence:\n")
	fmt.Fprintf(&b, "- RPC reads and simulations (curl, solana, anchor, spl-token, your own scripts): at most %d requests per second per RPC host, at most %s in flight, one getProgramAccounts page or simulation batch at a time, at least %d ms between requests\n", rps, inFlight, delayMS)
	fmt.Fprintf(&b, "- a local validator or fork (Surfpool, solana-test-validator, LiteSVM) is yours: it has no rate limit, so run bulk work (account walks, fuzzing, simulation sweeps) there and read the cluster only for what must come from it\n")
	b.WriteString("Prefer one read that answers the question over many that guess; on 429 or 503 stop and back off; never raise the rate to meet a deadline.\n")
	b.WriteString("ARTEX paces its own platform HTTP traffic (http custom tools, the enrich probe) with this profile. With traffic capture on, Bash commands that use the capture proxy (curl, wget, python-requests, node, the Solana CLIs) are paced too, by rate and jitter. These limits remain the brake: do not rely on the proxy.\n")
	return b.String()
}
