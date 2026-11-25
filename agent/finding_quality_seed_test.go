package agent

import "strings"
import "testing"

// The finding quality gates (attacker-goal proof standard, measured impact,
// proof basis, bounded testing) must stay in the executor agents' default
// seeds. Methodology adapted from VulnHunter (Apache-2.0) and
// bug-bounty-hunting-prompts (MIT).
func TestPromptSeedsCarryFindingQualityGates(t *testing.T) {
	for name, tmpl := range BuiltinPromptSeeds() {
		switch name {
		case "worker", "pentest":
			for _, marker := range []string{
				"attacker's goal achieved end to end",
				"a number with its denominator",
				"State the proof basis",
				"at most three non-owned accounts",
				// The money rules: cluster choice, the approval gate on live
				// transactions, mainnet, and the operator-funded wallet flow.
				"never mainnet-beta",
				"the platform's approval gate has let it through",
				"Never change mainnet-beta state",
				"never print or paste the secret key",
				"ask_operator with the exact funding command",
			} {
				if !strings.Contains(tmpl, marker) {
					t.Errorf("%s seed missing quality-gate marker %q", name, marker)
				}
			}
		}
	}
	for _, marker := range []string{
		"## Measured impact",
		"denominator",
		"claim, not a measurement",
	} {
		if !strings.Contains(ReporterDefaultPrompt, marker) {
			t.Errorf("reporter prompt missing measured-impact marker %q", marker)
		}
	}
}
