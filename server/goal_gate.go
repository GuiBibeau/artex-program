package server

import (
	"errors"
	"regexp"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// Ambiguity gate (ouroboros take, #99): a task goal must name a concrete target
// and be long enough to carry a testable objective before any token is spent.
// The live failure this guards against: a benchmark task whose goal named no
// target URL passed creation silently and the planner sent every worker to an
// unrelated asset. A goal that is deliberately target-less (pure analysis over
// provided material) can opt out with the literal marker [targetless].
var (
	goalMinLen = 80

	goalTargetlessRE = regexp.MustCompile(`\[targetless\]`)
	goalURLRE        = regexp.MustCompile(`(?i)\bhttps?://\S+`)
	goalDomainRE     = regexp.MustCompile(`(?i)\b[a-z0-9][a-z0-9-]{1,61}\.[a-z]{2,}(:[0-9]+)?(/\S*)?\b`)
	goalIPv4RE       = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(:\d+)?\b`)
	goalLocalhostRE  = regexp.MustCompile(`(?i)\blocalhost:[0-9]+\b`)
	goalPathRE       = regexp.MustCompile(`(?i)(^|\s)/[a-z0-9_-]+(/[a-z0-9_.-]+)+`)
)

// goalNamesSolanaProgram reports whether the goal names a Solana key: a token of
// the base58 alphabet that decodes to 32 bytes, such as a program id.
func goalNamesSolanaProgram(goal string) bool {
	for _, tok := range strings.FieldsFunc(goal, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if db.IsSolanaAddress(tok) {
			return true
		}
	}
	return false
}

// goalGateCheck returns a descriptive error when the goal is too ambiguous to
// dispatch workers against. Order: explicit opt-out, length floor, target
// presence (URL, domain, IPv4, localhost:port, absolute path, or a Solana
// program id).
func goalGateCheck(goal string) error {
	g := strings.TrimSpace(goal)
	if goalTargetlessRE.MatchString(g) {
		return nil
	}
	if len(g) < goalMinLen {
		return errors.New("goal is too short to carry a testable objective (min 80 chars): name the target and what counts as success; add the literal marker [targetless] only for genuinely target-less analysis tasks")
	}
	if goalURLRE.MatchString(g) || goalDomainRE.MatchString(g) ||
		goalIPv4RE.MatchString(g) || goalLocalhostRE.MatchString(g) || goalPathRE.MatchString(g) ||
		goalNamesSolanaProgram(g) {
		return nil
	}
	return errors.New("goal names no target: include the Solana program id or the URL/host/IP/path under test (or the literal marker [targetless] for target-less analysis tasks) so workers cannot be dispatched against an unrelated asset")
}
