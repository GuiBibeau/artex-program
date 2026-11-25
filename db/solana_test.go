package db

import (
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
)

// Well-known mainnet program ids, used as fixed valid addresses.
const (
	solTestSystemProgram = "11111111111111111111111111111111"
	solTestTokenProgram  = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	solTestATAProgram    = "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"
)

// solTestAddr returns a valid address nobody else can hold: the assets table
// is shared by every test run, so a test must not reuse a fixed address.
func solTestAddr(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return solTestEncode(b)
}

// solTestEncode is a base58 encoder, so the tests can build addresses without
// importing a library the product does not use.
func solTestEncode(in []byte) string {
	zeros := 0
	for zeros < len(in) && in[zeros] == 0 {
		zeros++
	}
	digits := make([]byte, 0, len(in)*138/100+1) // little-endian, base 58
	for _, b := range in {
		carry := int(b)
		for j := range digits {
			x := int(digits[j])*256 + carry
			digits[j] = byte(x % 58)
			carry = x / 58
		}
		for carry > 0 {
			digits = append(digits, byte(carry%58))
			carry /= 58
		}
	}
	var sb strings.Builder
	for i := 0; i < zeros; i++ {
		sb.WriteByte('1')
	}
	for i := len(digits) - 1; i >= 0; i-- {
		sb.WriteByte(base58Alphabet[digits[i]])
	}
	return sb.String()
}

func TestDecodeBase58(t *testing.T) {
	if got := solTestEncode(make([]byte, 32)); got != solTestSystemProgram {
		t.Fatalf("encoder: 32 zero bytes = %q, want %q", got, solTestSystemProgram)
	}
	for _, addr := range []string{solTestSystemProgram, solTestTokenProgram, solTestATAProgram} {
		b, ok := DecodeBase58(addr)
		if !ok || len(b) != 32 {
			t.Errorf("DecodeBase58(%s) = %d bytes, ok=%v; want 32 bytes", addr, len(b), ok)
			continue
		}
		if !IsSolanaAddress(addr) {
			t.Errorf("IsSolanaAddress(%s) = false", addr)
		}
		if solTestEncode(b) != addr {
			t.Errorf("round trip changed %s into %s", addr, solTestEncode(b))
		}
	}
	for _, bad := range []string{"", "0OIl", " " + solTestTokenProgram, solTestEncode(make([]byte, 65))} {
		if _, ok := DecodeBase58(bad); ok {
			t.Errorf("DecodeBase58(%q) accepted", bad)
		}
	}
	// A key is exactly 32 bytes.
	if IsSolanaAddress(solTestEncode(make([]byte, 31))) || IsSolanaAddress(solTestEncode(make([]byte, 33))) {
		t.Error("IsSolanaAddress accepted a value that is not 32 bytes")
	}
	if err := ValidateSolanaAddress(solTestTokenProgram); err != nil {
		t.Errorf("ValidateSolanaAddress(%s): %v", solTestTokenProgram, err)
	}
	if err := ValidateSolanaAddress("not-an-address"); err == nil {
		t.Error("ValidateSolanaAddress accepted an invalid key")
	}
}

func TestNormalizeSolanaCluster(t *testing.T) {
	for in, want := range map[string]string{
		"devnet":                               "devnet",
		" Devnet ":                             "devnet",
		"Testnet":                              "testnet",
		"mainnet":                              "mainnet-beta",
		"mainnet-beta":                         "mainnet-beta",
		"localnet":                             "localnet",
		"https://rpc.example.io/key?token=abc": "https://rpc.example.io/key?token=abc",
		"http://127.0.0.1:8899":                "http://127.0.0.1:8899",
	} {
		got, err := NormalizeSolanaCluster(in)
		if err != nil || got != want {
			t.Errorf("NormalizeSolanaCluster(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://x", "api.devnet.solana.com", "devnet-beta", "wss://x.io"} {
		if _, err := NormalizeSolanaCluster(bad); err == nil {
			t.Errorf("NormalizeSolanaCluster(%q) accepted", bad)
		}
	}
	if got := SolanaClusterRPC("devnet"); got != "https://api.devnet.solana.com" {
		t.Errorf("SolanaClusterRPC(devnet) = %q", got)
	}
	if got := SolanaClusterRPC("https://rpc.example.io"); got != "https://rpc.example.io" {
		t.Errorf("SolanaClusterRPC(url) = %q", got)
	}
}

func TestParseScopeInputSolana(t *testing.T) {
	rule, err := ParseScopeInput(ScopeInput{Kind: ScopeKindProgram, Value: solTestTokenProgram})
	if err != nil || rule.Kind != ScopeKindProgram || rule.Value != solTestTokenProgram {
		t.Errorf("program rule = %+v, %v", rule, err)
	}
	if _, err := ParseScopeInput(ScopeInput{Kind: ScopeKindProgram, Value: "not-a-key"}); err == nil {
		t.Error("an invalid program id was accepted")
	}
	rule, err = ParseScopeInput(ScopeInput{Kind: ScopeKindCluster, Value: "Mainnet"})
	if err != nil || rule.Kind != ScopeKindCluster || rule.Value != "mainnet-beta" {
		t.Errorf("cluster rule = %+v, %v", rule, err)
	}
	if _, err := ParseScopeInput(ScopeInput{Kind: ScopeKindCluster, Value: ""}); err == nil {
		t.Error("an empty cluster was accepted")
	}

	// An untyped line that is a program id or a cluster label is recognised as
	// such; an RPC URL keeps its domain meaning.
	if rule, err = ParseAutoScopeLine(solTestTokenProgram); err != nil || rule.Kind != ScopeKindProgram {
		t.Errorf("auto program = %+v, %v", rule, err)
	}
	if rule, err = ParseAutoScopeLine("devnet"); err != nil || rule.Kind != ScopeKindCluster || rule.Value != "devnet" {
		t.Errorf("auto cluster = %+v, %v", rule, err)
	}
	if rule, err = ParseAutoScopeLine("api.devnet.solana.com"); err != nil || rule.Kind != "domain" {
		t.Errorf("auto domain = %+v, %v", rule, err)
	}
}

func TestSolanaAssetReqNormalize(t *testing.T) {
	program, account := solTestAddr(t), solTestAddr(t)
	valid := []SolanaAssetReq{
		{Type: SolanaProgram, Address: program},
		{Type: " Instruction ", Parent: program, Name: "swapBaseIn"},
		{Type: SolanaAccount, Address: account, Parent: program},
		{Type: SolanaAuthority, Address: account, Parent: program},
		{Type: SolanaMint, Address: account, Parent: program},
		{Type: SolanaTokenAccount, Address: account, Parent: program},
	}
	for _, r := range valid {
		if _, err := r.normalize(); err != nil {
			t.Errorf("%s: %v", r.Type, err)
		}
	}
	invalid := map[string]SolanaAssetReq{
		"no type":               {Address: program},
		"unknown type":          {Type: "pda", Address: account, Parent: program},
		"program no address":    {Type: SolanaProgram},
		"program bad key":       {Type: SolanaProgram, Address: "not-a-key"},
		"instruction no name":   {Type: SolanaInstruction, Parent: program},
		"instruction bad name":  {Type: SolanaInstruction, Parent: program, Name: "1 step"},
		"instruction no parent": {Type: SolanaInstruction, Name: "withdraw"},
		"account no parent":     {Type: SolanaAccount, Address: account},
		"account bad parent":    {Type: SolanaAccount, Address: account, Parent: "nope"},
		"account no address":    {Type: SolanaAccount, Parent: program},
		"label too long":        {Type: SolanaAccount, Address: account, Parent: program, Name: strings.Repeat("x", maxSolanaNameRunes+1)},
	}
	for name, r := range invalid {
		if _, err := r.normalize(); err == nil {
			t.Errorf("%s: accepted %+v", name, r)
		}
	}
	// A program is the root: it ignores a parent.
	if r, err := (SolanaAssetReq{Type: SolanaProgram, Address: program, Parent: account}).normalize(); err != nil || r.Parent != "" {
		t.Errorf("program parent = %q, %v; want empty", r.Parent, err)
	}
	// An instruction has no address of its own.
	if r, err := (SolanaAssetReq{Type: SolanaInstruction, Parent: program, Name: "withdraw", Address: account}).normalize(); err != nil || r.Address != "" {
		t.Errorf("instruction address = %q, %v; want empty", r.Address, err)
	}
}

func TestMergeSolanaProps(t *testing.T) {
	stored := map[string]any{
		"cluster":     "devnet",
		"upgradeable": true,
		"tags":        []any{"defi"},
		"controls":    []any{map[string]any{"asset": "p1", "role": "admin"}},
	}
	incoming := map[string]any{
		"cluster":     "mainnet-beta",
		"tags":        []any{"defi", "perps"},
		"controls":    []any{map[string]any{"asset": "p1", "role": "admin"}, map[string]any{"asset": "p2", "role": "admin"}},
		"source_repo": "https://github.com/x/y",
	}
	merged, changed := mergeSolanaProps(stored, incoming)
	if !changed {
		t.Fatal("a merge with new values reported no change")
	}
	if merged["cluster"] != "mainnet-beta" || merged["upgradeable"] != true || merged["source_repo"] != "https://github.com/x/y" {
		t.Errorf("scalars merged wrong: %v", merged)
	}
	if tags, _ := merged["tags"].([]any); len(tags) != 2 || tags[0] != "defi" || tags[1] != "perps" {
		t.Errorf("tags = %v, want [defi perps]", tags)
	}
	if controls, _ := merged["controls"].([]any); len(controls) != 2 {
		t.Errorf("controls = %v, want 2 entries", controls)
	}
	// The stored maps are not modified.
	if len(stored["tags"].([]any)) != 1 {
		t.Error("merge modified the stored attributes")
	}
	// Re-applying the same attributes changes nothing.
	if again, changed := mergeSolanaProps(merged, incoming); changed || !equalJSONValue(again, merged) {
		t.Errorf("an identical merge changed the result: %v", again)
	}
}

func TestUpsertSolanaAsset(t *testing.T) {
	d, as, _ := testSetup(t)
	defer d.Close()

	program, other := solTestAddr(t), solTestAddr(t)
	var ids []int64
	defer func() {
		_, err := as.DeleteByIDs(ids)
		cleanupErr(t, "assets", err)
	}()
	register := func(want string, r SolanaAssetReq) int64 {
		t.Helper()
		id, err := as.UpsertSolanaAsset(r)
		if err != nil {
			t.Fatalf("%s: %v", want, err)
		}
		ids = append(ids, id)
		return id
	}

	// The program: first name kept, attributes merged, tasks united.
	pid := register("program", SolanaAssetReq{Type: SolanaProgram, Address: program, Name: "Test program", TaskID: 7001,
		Props: map[string]any{"cluster": "devnet", "upgradeable": true, "tags": []any{"defi"}}})
	again := register("program again", SolanaAssetReq{Type: SolanaProgram, Address: program, Name: "Other name", TaskID: 7002,
		Props: map[string]any{"cluster": "mainnet-beta", "tags": []any{"defi", "perps"}}})
	if again != pid {
		t.Fatalf("program dedup failed: %d != %d", again, pid)
	}
	var (
		name, chain, address, parent string
		taskJSON, extraRaw           string
	)
	if err := d.QueryRow(`SELECT COALESCE(name,''), COALESCE(chain,''), COALESCE(address,''), COALESCE(parent,''),
       array_to_json(task_ids)::text, extra::text FROM assets WHERE id=$1`, pid).
		Scan(&name, &chain, &address, &parent, &taskJSON, &extraRaw); err != nil {
		t.Fatal(err)
	}
	if name != "Test program" || chain != ChainSolana || address != program || parent != "" {
		t.Errorf("program row = name %q chain %q address %q parent %q", name, chain, address, parent)
	}
	var extra map[string]any
	if err := json.Unmarshal([]byte(extraRaw), &extra); err != nil {
		t.Fatal(err)
	}
	if extra["cluster"] != "mainnet-beta" || extra["upgradeable"] != true {
		t.Errorf("program attributes = %v", extra)
	}
	if tags, _ := extra["tags"].([]any); len(tags) != 2 {
		t.Errorf("program tags = %v, want 2", tags)
	}
	var taskIDs []int64
	if err := json.Unmarshal([]byte(taskJSON), &taskIDs); err != nil || len(taskIDs) != 2 {
		t.Errorf("task ids = %s (%v), want 2", taskJSON, err)
	}

	// An instruction is keyed by (program, name).
	iid := register("withdraw", SolanaAssetReq{Type: SolanaInstruction, Parent: program, Name: "withdraw", TaskID: 7001,
		Props: map[string]any{"discriminator": []any{1, 2, 3}}})
	if id := register("withdraw again", SolanaAssetReq{Type: SolanaInstruction, Parent: program, Name: "withdraw"}); id != iid {
		t.Errorf("instruction dedup failed: %d != %d", id, iid)
	}
	if id := register("deposit", SolanaAssetReq{Type: SolanaInstruction, Parent: program, Name: "deposit"}); id == iid {
		t.Error("a second instruction name reused the first row")
	}

	// A child whose program is not registered yet creates the program row.
	vault := solTestAddr(t)
	vid := register("vault", SolanaAssetReq{Type: SolanaAccount, Address: vault, Parent: other, TaskID: 7003,
		Props: map[string]any{"pda": true, "seeds": []any{"vault"}}})
	var connectorID int64
	if err := d.QueryRow(`SELECT id FROM assets WHERE chain=$1 AND type='program' AND address=$2`, ChainSolana, other).Scan(&connectorID); err != nil {
		t.Fatalf("the parent program row was not created: %v", err)
	}
	ids = append(ids, connectorID)

	// An authority accumulates the assets it controls and keeps its first parent.
	auth := solTestAddr(t)
	control := func(asset, role string) map[string]any { return map[string]any{"asset": asset, "role": role} }
	aid := register("authority", SolanaAssetReq{Type: SolanaAuthority, Address: auth, Parent: program, TaskID: 7001,
		Props: map[string]any{"controls": []any{control(program, "upgrade_authority")}}})
	if id := register("authority again", SolanaAssetReq{Type: SolanaAuthority, Address: auth, Parent: other, TaskID: 7004,
		Props: map[string]any{"controls": []any{control(program, "upgrade_authority"), control(other, "admin")}}}); id != aid {
		t.Errorf("authority dedup failed: %d != %d", id, aid)
	}

	// The rows read back through the shared column list.
	rows, err := as.GetByIDs([]int64{pid, iid, vid, aid})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]*Asset{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if len(rows) != 4 {
		t.Fatalf("GetByIDs returned %d rows", len(rows))
	}
	if r := byID[pid]; r.Chain != ChainSolana || r.Address != program || r.Name != "Test program" || r.Parent != "" {
		t.Errorf("program read back as chain=%q address=%q name=%q parent=%q", r.Chain, r.Address, r.Name, r.Parent)
	}
	if r := byID[iid]; r.Type != SolanaInstruction || r.Parent != program || r.Name != "withdraw" || r.Address != "" {
		t.Errorf("instruction read back as type=%q parent=%q name=%q address=%q", r.Type, r.Parent, r.Name, r.Address)
	}
	if r := byID[vid]; r.Address != vault || r.Parent != other || r.Extra["pda"] != true {
		t.Errorf("account read back as address=%q parent=%q extra=%v", r.Address, r.Parent, r.Extra)
	}
	if r := byID[aid]; r.Parent != program || len(r.Extra["controls"].([]any)) != 2 {
		t.Errorf("authority read back as parent=%q extra=%v", r.Parent, r.Extra)
	}
}

func TestSolanaTaskScopeAndCoverage(t *testing.T) {
	d, as, _ := testSetup(t)
	t.Cleanup(func() { d.Close() }) // registered first, so it runs after the row cleanups
	task, err := d.CreateTask("solana scope", "audit the program", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupErr(t, "task", d.DeleteTask(task.ID)) })

	program, child, outside := solTestAddr(t), solTestAddr(t), solTestAddr(t)
	// The rows are removed by address, which also takes the program rows the
	// upsert creates for a parent that was not registered.
	addrs := []string{program, child, outside}
	t.Cleanup(func() {
		cleanupExec(t, d, `DELETE FROM assets WHERE chain=$1 AND (address = ANY($2::text[]) OR parent = ANY($2::text[]))`,
			ChainSolana, addrs)
	})
	register := func(r SolanaAssetReq) int64 {
		t.Helper()
		r.TaskID = task.ID
		id, err := as.UpsertSolanaAsset(r)
		if err != nil {
			t.Fatal(err)
		}
		addrs = append(addrs, r.Address, r.Parent)
		return id
	}
	register(SolanaAssetReq{Type: SolanaProgram, Address: program, Name: "Target"})
	register(SolanaAssetReq{Type: SolanaInstruction, Parent: program, Name: "withdraw"})
	accountID := register(SolanaAssetReq{Type: SolanaAccount, Address: child, Parent: program, Props: map[string]any{"pda": true}})
	register(SolanaAssetReq{Type: SolanaMint, Address: solTestAddr(t), Parent: outside})

	// The goals agent declares the target program and the cluster.
	if _, err := as.AddAgentScope(task.ID, ScopeKindProgram, program, "goal target", "agent"); err != nil {
		t.Fatal(err)
	}
	if ts, err := as.AddAgentScope(task.ID, ScopeKindCluster, "Devnet", "cluster named in the goal", "agent"); err != nil || ts.Kind != ScopeKindCluster || ts.Value != "devnet" {
		t.Errorf("cluster scope = %+v, %v", ts, err)
	}
	if _, err := as.AddAgentScope(task.ID, ScopeKindProgram, "bad", "", "agent"); err == nil {
		t.Error("an invalid program id was accepted as scope")
	}
	if _, err := as.AddAgentScope(task.ID, ScopeKindCluster, "ftp://x", "", "agent"); err == nil {
		t.Error("an invalid cluster was accepted as scope")
	}

	// An asset the worker registers adds no scope beyond its program: one row
	// for the program, none for what is under it.
	if err := as.AddAutoSolanaScope(task.ID, SolanaAccount, child); err != nil {
		t.Fatal(err)
	}
	if err := as.AddAutoSolanaScope(task.ID, SolanaProgram, program); err != nil {
		t.Fatal(err)
	}
	scope, err := as.ListTaskScope(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	programs := 0
	for _, r := range scope {
		if r.Kind == ScopeKindProgram {
			programs++
		}
	}
	if len(scope) != 2 || programs != 1 {
		t.Errorf("scope rows = %+v, want one program row and one cluster row", scope)
	}

	// Coverage counts the program and what is under it, not another program's mint.
	cov, err := as.TaskCoverage(task.ID, task.ExplorationID)
	if err != nil {
		t.Fatal(err)
	}
	byType := map[string]CoverageByType{}
	for _, bt := range cov.ByType {
		byType[bt.Type] = bt
	}
	if byType[SolanaProgram].Total != 1 || byType[SolanaInstruction].Total != 1 || byType[SolanaAccount].Total != 1 {
		t.Errorf("coverage by type = %v", cov.ByType)
	}
	if _, has := byType[SolanaMint]; has || cov.Denominator != 3 {
		t.Errorf("an asset outside the program scope is covered: %+v", cov)
	}
	if _, total, err := as.ListUntestedAssets(task.ID, task.ExplorationID, "", 50, 0); err != nil || total != 3 {
		t.Errorf("untested total = %d, %v; want 3", total, err)
	}

	// The graph: the program and its children in scope, each child linked to it.
	g, err := as.BuildCoverageGraph(task.ID, task.ExplorationID)
	if err != nil {
		t.Fatal(err)
	}
	node := func(pred func(*CoverageGraphNode) bool) *CoverageGraphNode {
		for i := range g.Nodes {
			if pred(&g.Nodes[i]) {
				return &g.Nodes[i]
			}
		}
		return nil
	}
	target := node(func(n *CoverageGraphNode) bool { return n.Kind == SolanaProgram && n.Address == program })
	if target == nil || !target.InScope || target.Label != program {
		t.Fatalf("the target program node is missing or mislabelled: %+v", target)
	}
	account := node(func(n *CoverageGraphNode) bool { return n.AssetID == accountID })
	if account == nil || !account.InScope || account.Label != child {
		t.Fatalf("the account node is missing or mislabelled: %+v", account)
	}
	linked := false
	for _, e := range g.Edges {
		if e.Src == account.Key && e.Dst == target.Key {
			linked = true
		}
	}
	if !linked {
		t.Errorf("the account is not linked to its program: %v", g.Edges)
	}
	if node(func(n *CoverageGraphNode) bool { return n.Kind == SolanaMint }) != nil {
		t.Error("an asset outside the scope is drawn in the graph")
	}

	// A fact anchors the account: it counts as tested. The same fact anchors an
	// asset of another program: that one is drawn, with a gray connector for its
	// program, which has no row of its own.
	foreignProgram := solTestAddr(t)
	foreign := register(SolanaAssetReq{Type: SolanaAuthority, Address: solTestAddr(t), Parent: foreignProgram})
	if _, err := d.Exploration(task.ExplorationID).AddNode(KindFact, map[string]any{"summary": "withdraw lacks a signer check"},
		0, "confirmed", "worker", []int64{accountID, foreign}); err != nil {
		t.Fatal(err)
	}
	if cov, err = as.TaskCoverage(task.ID, task.ExplorationID); err != nil || cov.Tested != 1 {
		t.Errorf("tested = %d, %v; want 1", cov.Tested, err)
	}
	g, err = as.BuildCoverageGraph(task.ID, task.ExplorationID)
	if err != nil {
		t.Fatal(err)
	}
	anchored := node(func(n *CoverageGraphNode) bool { return n.AssetID == foreign })
	if anchored == nil || !anchored.Tested {
		t.Fatalf("the fact-anchored authority is missing or not tested: %+v", anchored)
	}
	connector := node(func(n *CoverageGraphNode) bool { return n.Kind == SolanaProgram && n.Address == foreignProgram })
	if connector == nil || connector.InScope || connector.Label != foreignProgram {
		t.Errorf("connector node = %+v", connector)
	}
	linked = false
	for _, e := range g.Edges {
		if e.Src == anchored.Key && e.Dst == connector.Key {
			linked = true
		}
	}
	if !linked {
		t.Errorf("the authority is not linked to its program: %v", g.Edges)
	}

	// The search language reaches the new fields, inside the task scope.
	rows, err := as.QueryDSLInScope("parent=="+program, "", task.ID, 50, 0)
	if err != nil || len(rows) != 2 {
		t.Errorf("parent query = %d rows, %v; want 2", len(rows), err)
	}
	if rows, err = as.QueryDSLInScope("program_id=="+program, SolanaInstruction, task.ID, 50, 0); err != nil || len(rows) != 1 || rows[0].Name != "withdraw" {
		t.Errorf("instruction query = %+v, %v", rows, err)
	}
	if rows, err = as.QueryDSL("address=="+program+" AND chain=="+ChainSolana, "", 0, 50, 0); err != nil || len(rows) != 1 {
		t.Errorf("address query = %d rows, %v; want 1", len(rows), err)
	}

	// The operator registers a program and a cluster from the task assets page.
	extra := solTestAddr(t)
	mut, err := as.RegisterTaskAssetScopes(task.ID, []ScopeInput{{Kind: ScopeKindProgram, Value: extra}, {Kind: ScopeKindCluster, Value: "testnet"}})
	if err != nil {
		t.Fatal(err)
	}
	if mut.AssetsLinked != 1 || mut.ScopesAdded != 2 {
		t.Errorf("mutation = %+v", mut)
	}
	var extraID int64
	if err := d.QueryRow(`SELECT id FROM assets WHERE chain=$1 AND type='program' AND address=$2 AND $3=ANY(task_ids)`,
		ChainSolana, extra, task.ID).Scan(&extraID); err != nil {
		t.Errorf("the registered program is not linked to the task: %v", err)
	}
	addrs = append(addrs, extra)
	if _, err := as.RegisterTaskAssetScopes(task.ID, []ScopeInput{{Kind: ScopeKindProgram, Value: "bad"}}); err == nil {
		t.Error("an invalid program id was registered")
	}
}
