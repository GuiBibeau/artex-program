package db

import (
	"regexp"
	"testing"
)

// A start that stopped after the rule inserts and before the settings flag runs the
// seed again on the next start: it must succeed and must not duplicate a rule.
func TestSeedSolanaApprovalRulesIsIdempotent(t *testing.T) {
	d := openTargetDB(t) // Open already ran the seed once
	names := []string{
		"[built-in] Solana: funds, staking, voting or program state",
		"[built-in] Solana: spl-token state change",
		"[built-in] Solana: anchor deploy or upgrade",
		"[built-in] Solana: transaction submitted from the command line",
	}
	for range 2 {
		if _, err := d.Exec(`DELETE FROM settings WHERE key = 'intercept_solana_rules_v1'`); err != nil {
			t.Fatal(err)
		}
		if err := d.seedSolanaApprovalRules(); err != nil {
			t.Fatalf("seed re-run failed: %v", err)
		}
	}
	for _, name := range names {
		var n int
		var action string
		if err := d.QueryRow(`SELECT count(*), min(action) FROM intercept_rules WHERE name = $1`, name).Scan(&n, &action); err != nil {
			t.Fatal(err)
		}
		if n != 1 || action != "ask" {
			t.Errorf("%q: %d rows of action %q, want 1 ask", name, n, action)
		}
	}
	if v, _, _ := d.GetSetting("intercept_solana_rules_v1"); v != "done" {
		t.Errorf("settings flag = %q, want done", v)
	}
}

// The patterns gate what changes chain state or moves funds, and nothing else:
// reads and simulation are the default way of working and must stay free.
func TestSolanaApprovalPatterns(t *testing.T) {
	cases := []struct {
		pattern string
		gated   []string
		free    []string
	}{
		{solanaCLIMoneyPattern,
			[]string{
				`solana transfer 9xQeWvG816bUx9EPjxoWYHqQqgVqT6p5K7oLm1N2b3C --url https://api.devnet.solana.com 0.1`,
				`solana program deploy target/deploy/poc.so --url devnet`,
				`solana program set-upgrade-authority EqWp5CbMnRgQeNgyA9aTKM6gnQ8vPku9xRgkhLFyVaCJ new.pubkey`,
				`solana withdraw-stake stake.json 0.5`,
				`SOLANA_URL=1 solana transfer --from kp.json 1 addr`,
			},
			[]string{
				`solana account EqWp5CbMnRgQeNgyA9aTKM6gnQ8vPku9xRgkhLFyVaCJ --url devnet`,
				`solana program dump EqWp5CbMnRgQeNgyA9aTKM6gnQ8vPku9xRgkhLFyVaCJ program.so`,
				`solana program show EqWp5CbMnRgQeNgyA9aTKM6gnQ8vPku9xRgkhLFyVaCJ`,
				`solana airdrop 1 9xQeWvG816bUx9EPjxoWYHqQqgVqT6p5K7oLm1N2b3C --url https://api.devnet.solana.com`,
				`solana balance`,
				`solana config set --url devnet`,
			}},
		{solanaSplTokenPattern,
			[]string{
				`spl-token transfer 7xKXt... 5 9xQe... --url devnet`,
				`spl-token set-authority mint.json mint-authority --disable`,
				`spl-token freeze 9xQe... addr`,
			},
			[]string{
				`spl-token display 7xKXtg2CW87d97TXJSDwxDUSM3p2VC9KCmU8v7F7GZ3p`,
				`spl-token supply 7xKXt...`,
				`spl-token accounts`,
			}},
		{anchorDeployPattern,
			[]string{`anchor deploy --provider.cluster devnet`, `anchor upgrade target/deploy/poc.so --program-id EqWp...`},
			[]string{`anchor build`, `NO_DNA=1 anchor test`, `anchor idl fetch EqWp5CbMnRgQeNgyA9aTKM6gnQ8vPku9xRgkhLFyVaCJ`, `anchor expand`}},
		{solanaRawSubmitPattern,
			[]string{
				`curl -s https://api.devnet.solana.com -d '{"jsonrpc":"2.0","id":1,"method":"sendTransaction","params":["0102"]}'`,
				`node -e "require('@solana/web3.js').sendAndConfirmTransaction(c, tx, [kp])"`,
			},
			[]string{
				`curl -s https://api.devnet.solana.com -d '{"jsonrpc":"2.0","id":1,"method":"simulateTransaction","params":["0102",{"sigVerify":false}]}'`,
				`curl -s https://api.devnet.solana.com -d '{"jsonrpc":"2.0","id":1,"method":"getAccountInfo","params":["EqWp...",{"encoding":"base64"}]}'`,
				`node exploit/simulate.js`,
			}},
	}
	for _, c := range cases {
		re, err := regexp.Compile(c.pattern)
		if err != nil {
			t.Fatalf("pattern does not compile: %v", err)
		}
		for _, cmd := range c.gated {
			if !re.MatchString(cmd) {
				t.Errorf("not gated: %s", cmd)
			}
		}
		for _, cmd := range c.free {
			if re.MatchString(cmd) {
				t.Errorf("gated but is a read: %s", cmd)
			}
		}
	}
}
