package db

import "fmt"

// seedSolanaApprovalRules inserts the built-in approval rules for the commands
// that change chain state or move funds: solana and spl-token money and program
// commands, anchor deployments, and a command line that submits a transaction to
// an RPC node. Each is an ask: the operator approves it in the console. Reads and
// simulation (getAccountInfo, simulateTransaction, program dumps, IDL fetches,
// local validators) match none of these patterns and run without approval.
//
// Seeded once, behind its own settings flag, like the other built-in seeds, so a
// later disable by the operator is never undone on restart. No agent tool reads
// or edits intercept rules, so an agent cannot lower them. A transaction sent
// from inside a script is not visible to a command pattern: the script's own
// sendTransaction is gated by the operator's constraints and the fallback judge.
func (d *DB) seedSolanaApprovalRules() error {
	if v, _, _ := d.GetSetting("intercept_solana_rules_v1"); v == "done" {
		return nil
	}
	rules := []struct {
		name    string
		pattern string
		message string
	}{
		{
			name:    "[built-in] Solana: funds, staking, voting or program state",
			pattern: solanaCLIMoneyPattern,
			message: "Approval needed: this solana command moves funds, changes staking or voting state, or deploys, upgrades or changes the authority of a program. Check the cluster (--url) and the amount before you approve.",
		},
		{
			name:    "[built-in] Solana: spl-token state change",
			pattern: solanaSplTokenPattern,
			message: "Approval needed: this spl-token command moves, mints, burns, freezes or changes the authority of tokens. Check the cluster and the amount before you approve.",
		},
		{
			name:    "[built-in] Solana: anchor deploy or upgrade",
			pattern: anchorDeployPattern,
			message: "Approval needed: the agent asks to deploy or upgrade a program with anchor. Check the cluster of the anchor provider (Anchor.toml, --provider.cluster) before you approve.",
		},
		{
			name:    "[built-in] Solana: transaction submitted from the command line",
			pattern: solanaRawSubmitPattern,
			message: "Approval needed: this command line submits a transaction or a bundle to an RPC node. Check the RPC URL and what the transaction does before you approve.",
		},
	}
	// intercept_rules.name has no unique index, so ON CONFLICT cannot apply: the insert
	// is conditional on the name instead. A start that stopped between these inserts
	// and SetSetting re-runs them without duplicating a rule.
	for _, r := range rules {
		if _, err := d.Exec(`
INSERT INTO intercept_rules(name, enabled, priority, match_target, match_type, pattern, action, message, timeout_enabled, timeout_seconds, timeout_action)
SELECT $1, true, 70, 'tool_input', 'regex', $2, 'ask', $3, false, 60, 'deny'
WHERE NOT EXISTS (SELECT 1 FROM intercept_rules WHERE name = $1)`,
			r.name, r.pattern, r.message,
		); err != nil {
			return fmt.Errorf("rule %q: %w", r.name, err)
		}
	}
	return d.SetSetting("intercept_solana_rules_v1", "done")
}

// The solana CLI commands that change funds, staking, voting or program state.
// Read-only commands (account, balance, program dump / show, airdrop) are absent
// on purpose.
const solanaCLIMoneyPattern = `\bsolana\s+(?:transfer|pay|stake-create-account|stake-authorize|stake-set-lockup|delegate-stake|deactivate-stake|withdraw-stake|merge-stake|split-stake|vote-create-account|vote-authorize|vote-withdraw|vote-update-validator|vote-update-commission-rate|vote-init|program\s+(?:deploy|upgrade|set-buffer-authority|set-upgrade-authority|close|redeploy)|reward|lockup|transfer-with-seed)\b`

// The spl-token commands that change token state. display, supply, accounts and
// balance are read-only and absent on purpose.
const solanaSplTokenPattern = `\bspl-token\s+(?:transfer|transfer-checked|approve|approve-checked|revoke|mint|mint-checked|burn|burn-checked|freeze|thaw|set-authority|sync-native|close|withdraw-from-account)\b`

// anchor test and anchor build stay free: a local test run is the default way to
// reproduce a flaw. Only a deployment or an upgrade is gated.
const anchorDeployPattern = `\banchor\s+(?:deploy|upgrade)\b`

// A transaction submitted by a command line (curl to an RPC node, node -e, a
// one-liner). simulateTransaction is a read and is not matched.
const solanaRawSubmitPattern = `\b(?:sendTransaction|sendAndConfirmTransaction|sendAndConfirmRawTransaction|send_raw_transaction|sendBundle)\b`
