package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/evidence"
)

// A Solana finding keeps its receipts. A program asset has no host, so the
// proof of a finding on it is the JSON-RPC exchange bound to the finding,
// matched on the RPC host of the cluster the task declared in its scope.
func TestSolanaFindingProofBindsClusterRPCTraffic(t *testing.T) {
	d := testDB(t)
	t.Cleanup(func() { _ = d.Close() })
	ensureTestSecrets(t, d)
	task := p43BenchTask(t, d, "solana-proof")
	store := d.Exploration(task.ExplorationID)
	ctx := context.Background()

	const program = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	pid, err := d.Assets().UpsertSolanaAsset(db.SolanaAssetReq{Type: db.SolanaProgram, Address: program, TaskID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM assets WHERE id=$1`, pid) })

	tr := p43Capture(t)
	const rpcID, otherID = "ex-sol-rpc", "ex-sol-other"
	p43SeedCaptured(t, tr, rpcID, "https://api.devnet.solana.com/")
	p43SeedCaptured(t, tr, otherID, "https://unrelated.example.test/")
	ev := evidence.New(d, tr, filepath.Join(t.TempDir(), "evidence"))
	exists := func(context.Context, string) (bool, error) { return true, nil }
	record := func(ref string) *db.RecordedFinding {
		t.Helper()
		rec, err := ev.Record(ctx, db.RecordFindingInput{
			TaskID: task.ID, ExplorationID: task.ExplorationID, Worker: "worker-1",
			VulnClass: "missing_signer_check", Name: "withdraw has no signer check", Severity: "critical",
			Summary: "anyone signs as the vault authority", AssetIDs: []int64{pid},
		}, []db.TrafficRef{{TrafficID: ref, Role: "proof", Note: "simulateTransaction"}})
		if err != nil {
			t.Fatalf("record finding through the evidence store: %v", err)
		}
		return rec
	}

	// Without a cluster in scope there is no RPC host to match the exchange.
	rec := record(rpcID)
	check, err := store.VerifyFindingProofForNode(ctx, rec.NodeID, exists)
	if err != nil {
		t.Fatal(err)
	}
	if check.Proven || !strings.Contains(check.Reason, "cluster") {
		t.Errorf("no cluster scope: Proven=%v Reason=%q; want Proven=false and a reason naming the cluster", check.Proven, check.Reason)
	}

	// The cluster in scope makes the RPC exchange the proof.
	if _, err := d.Assets().AddAgentScope(task.ID, db.ScopeKindCluster, "devnet", "cluster named in the goal", "agent"); err != nil {
		t.Fatal(err)
	}
	check, err = store.VerifyFindingProofForNode(ctx, rec.NodeID, exists)
	if err != nil {
		t.Fatal(err)
	}
	if !check.Proven || len(check.MatchedTrafficIDs) != 1 || check.MatchedTrafficIDs[0] != rpcID {
		t.Errorf("RPC exchange on the cluster host: %+v; want Proven with %s matched", check, rpcID)
	}

	// Traffic to an unrelated host proves nothing.
	check, err = store.VerifyFindingProofForNode(ctx, record(otherID).NodeID, exists)
	if err != nil {
		t.Fatal(err)
	}
	if check.Proven || !strings.Contains(check.Reason, "does not match") {
		t.Errorf("unrelated host: Proven=%v Reason=%q; want Proven=false with a host mismatch", check.Proven, check.Reason)
	}
}
