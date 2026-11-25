package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Solana program assets: the on-chain half of the asset graph.
//
// A program is a root asset, as a root domain is for network assets. Everything
// found under it — instructions, accounts, authorities, mints, token accounts —
// is stored with parent = program id. One scope row of kind 'program' therefore
// covers the whole subtree, and the coverage graph stays one level deep.

// ChainSolana is the assets.chain value of a Solana program asset.
const ChainSolana = "solana"

// Solana asset types (assets.type with chain = ChainSolana).
const (
	SolanaProgram      = "program"       // an on-chain program; address = program id
	SolanaInstruction  = "instruction"   // one entrypoint; name = instruction name
	SolanaAccount      = "account"       // an account the program owns or uses, PDAs included
	SolanaAuthority    = "authority"     // a key that holds a privilege over an asset
	SolanaMint         = "mint"          // an SPL Token or Token-2022 mint
	SolanaTokenAccount = "token_account" // an SPL token account
)

// Task scope kinds of a Solana engagement.
const (
	ScopeKindProgram = "program" // value = program id
	ScopeKindCluster = "cluster" // value = devnet | testnet | mainnet-beta | localnet | RPC URL
)

// maxSolanaNameRunes bounds the display label of an asset.
const maxSolanaNameRunes = 200

// solanaAddressTypes are the types identified by an on-chain key.
var solanaAddressTypes = map[string]bool{
	SolanaProgram: true, SolanaAccount: true, SolanaAuthority: true,
	SolanaMint: true, SolanaTokenAccount: true,
}

// The unique indexes of schema.sql, written as ON CONFLICT infers them.
const (
	solanaAddressConflict     = `(type, address) WHERE chain = 'solana' AND type IN ('program','account','authority','mint','token_account')`
	solanaInstructionConflict = `(parent, name) WHERE chain = 'solana' AND type = 'instruction'`
)

// IsSolanaAssetType reports whether typ is a Solana asset type.
func IsSolanaAssetType(typ string) bool {
	switch typ {
	case SolanaProgram, SolanaInstruction, SolanaAccount, SolanaAuthority, SolanaMint, SolanaTokenAccount:
		return true
	}
	return false
}

// IsSolanaScopeKind reports whether kind is a task scope kind of a Solana engagement.
func IsSolanaScopeKind(kind string) bool {
	return kind == ScopeKindProgram || kind == ScopeKindCluster
}

// ── addresses ────────────────────────────────────────────────────────────────

// base58Alphabet is the Bitcoin base58 alphabet Solana keys use.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58Value maps a byte to its value in the alphabet, -1 when it is not in it.
var base58Value = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	for i := 0; i < len(base58Alphabet); i++ {
		t[base58Alphabet[i]] = int8(i)
	}
	return t
}()

// DecodeBase58 decodes s into bytes. ok is false for an empty string, a
// character outside the base58 alphabet, or a value over 64 bytes.
func DecodeBase58(s string) ([]byte, bool) {
	if s == "" || len(s) > 88 {
		return nil, false
	}
	// A leading '1' is a leading zero byte; skip them, count them.
	zeros := 0
	for zeros < len(s) && s[zeros] == '1' {
		zeros++
	}
	acc := make([]byte, 0, len(s)*733/1000+1) // little-endian
	for i := zeros; i < len(s); i++ {
		v := base58Value[s[i]]
		if v < 0 {
			return nil, false
		}
		carry := int(v)
		for j, b := range acc { // acc = acc*58 + v
			x := int(b)*58 + carry
			acc[j] = byte(x)
			carry = x >> 8
		}
		for carry > 0 {
			acc = append(acc, byte(carry))
			carry >>= 8
		}
	}
	if len(acc)+zeros > 64 {
		return nil, false
	}
	out := make([]byte, zeros+len(acc))
	for i, b := range acc {
		out[zeros+len(acc)-1-i] = b
	}
	return out, true
}

// IsSolanaAddress reports whether value is a base58-encoded 32-byte key.
func IsSolanaAddress(value string) bool {
	b, ok := DecodeBase58(strings.TrimSpace(value))
	return ok && len(b) == 32
}

// ValidateSolanaAddress checks that value is a base58-encoded 32-byte Solana
// key: a program id, an account address or a signer key.
func ValidateSolanaAddress(value string) error {
	if !IsSolanaAddress(value) {
		return fmt.Errorf("invalid Solana address %q: expected a base58 public key of 32 bytes", value)
	}
	return nil
}

// solanaInstructionName is an on-chain instruction name: the Anchor convention,
// which is also what IDLs expose.
var solanaInstructionName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// ── clusters ─────────────────────────────────────────────────────────────────

// SolanaClusters maps each cluster label to the public RPC endpoint used when
// the scope names the cluster and no endpoint of its own.
var SolanaClusters = map[string]string{
	"devnet":       "https://api.devnet.solana.com",
	"testnet":      "https://api.testnet.solana.com",
	"mainnet-beta": "https://api.mainnet-beta.solana.com",
	"localnet":     "http://127.0.0.1:8899",
}

// NormalizeSolanaCluster validates a cluster scope value: a known cluster label
// ("mainnet" is read as "mainnet-beta"), or the http(s) URL of an RPC endpoint.
// It returns the value to store.
func NormalizeSolanaCluster(value string) (string, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return "", errors.New("cluster must not be empty")
	}
	if strings.EqualFold(v, "mainnet") {
		v = "mainnet-beta"
	}
	if label := strings.ToLower(v); SolanaClusters[label] != "" {
		return label, nil
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid cluster %q: use devnet, testnet, mainnet-beta, localnet or an http(s) RPC URL", value)
	}
	if utf8.RuneCountInString(u.String()) > MaxCompanyScopeValueRunes {
		return "", fmt.Errorf("cluster RPC URL is too long: at most %d characters", MaxCompanyScopeValueRunes)
	}
	return u.String(), nil
}

// SolanaClusterRPC returns the RPC endpoint of a stored cluster scope value: the
// public endpoint of a cluster label, or the value itself when it is a URL.
func SolanaClusterRPC(cluster string) string {
	if rpc := SolanaClusters[strings.ToLower(strings.TrimSpace(cluster))]; rpc != "" {
		return rpc
	}
	return strings.TrimSpace(cluster)
}

// ── scope matching ───────────────────────────────────────────────────────────

// solanaScopePredicate matches the assets a program scope row covers: the
// program itself and every asset registered under it. It is written against the
// aliases task_scope ts and assets a.
const solanaScopePredicate = `(ts.kind='program' AND (a.address = ts.value OR a.parent = ts.value))`

// ── upsert ───────────────────────────────────────────────────────────────────

// SolanaAssetReq is the input of UpsertSolanaAsset.
type SolanaAssetReq struct {
	Type      string         // program | instruction | account | authority | mint | token_account
	Address   string         // the on-chain key; required unless Type is instruction
	Parent    string         // the program id the asset belongs to; required unless Type is program
	Name      string         // the instruction name (required for instruction); a display label otherwise
	Props     map[string]any // type-specific attributes, kept in extra
	CompanyID *int64         // explicit owner of a program
	TaskID    int64
}

// normalize validates the request and returns it in canonical form.
func (r SolanaAssetReq) normalize() (SolanaAssetReq, error) {
	r.Type = strings.ToLower(strings.TrimSpace(r.Type))
	r.Address = strings.TrimSpace(r.Address)
	r.Parent = strings.TrimSpace(r.Parent)
	r.Name = strings.TrimSpace(r.Name)
	if !IsSolanaAssetType(r.Type) {
		return r, fmt.Errorf("unknown Solana asset type %q", r.Type)
	}
	switch r.Type {
	case SolanaProgram:
		if r.Address == "" {
			return r, errors.New("address (the program id) is required")
		}
		if err := ValidateSolanaAddress(r.Address); err != nil {
			return r, err
		}
		r.Parent = ""
	case SolanaInstruction:
		if !solanaInstructionName.MatchString(r.Name) {
			return r, fmt.Errorf("invalid instruction name %q: use the on-chain name, for example withdraw or swapBaseIn", r.Name)
		}
		r.Address = ""
	default:
		if r.Address == "" {
			return r, fmt.Errorf("address is required for type %s", r.Type)
		}
		if err := ValidateSolanaAddress(r.Address); err != nil {
			return r, err
		}
	}
	if r.Type != SolanaProgram {
		if err := ValidateSolanaAddress(r.Parent); err != nil {
			return r, fmt.Errorf("program_id (the program this %s belongs to) is required: %w", r.Type, err)
		}
	}
	if r.Type != SolanaInstruction && utf8.RuneCountInString(r.Name) > maxSolanaNameRunes {
		return r, fmt.Errorf("name is too long: at most %d characters", maxSolanaNameRunes)
	}
	return r, nil
}

// UpsertSolanaAsset idempotently inserts or merges one Solana program asset and
// returns its id. Re-registering an asset keeps its first name and explicit
// owner, adds the task, replaces stored attributes with the new ones and unites
// the arrays in Props with the stored ones. When the parent program is not
// registered yet, a bare program row is created so the graph stays connected;
// that row does not widen the task scope.
func (s *AssetStore) UpsertSolanaAsset(req SolanaAssetReq) (int64, error) {
	req, err := req.normalize()
	if err != nil {
		return 0, err
	}
	if s.tx == nil {
		return s.withCompanyScopeMutation(func(scoped *AssetStore) (int64, error) {
			return scoped.UpsertSolanaAsset(req)
		})
	}
	taskIDs := "{}"
	if req.TaskID > 0 {
		taskIDs = fmt.Sprintf("{%d}", req.TaskID)
	}
	if req.Parent != "" {
		if _, err := s.tx.Exec(`INSERT INTO assets(type, chain, address, task_ids)
VALUES ('program', $1, $2, $3::bigint[])
ON CONFLICT `+solanaAddressConflict+` DO UPDATE SET
    task_ids = (SELECT ARRAY(SELECT DISTINCT unnest(assets.task_ids || EXCLUDED.task_ids)))`,
			ChainSolana, req.Parent, taskIDs); err != nil {
			return 0, err
		}
	}
	props, err := json.Marshal(nonNilProps(req.Props))
	if err != nil {
		return 0, err
	}
	var name, companyID any
	if req.Name != "" {
		name = req.Name
	}
	if req.Type == SolanaProgram && req.CompanyID != nil && *req.CompanyID > 0 {
		companyID = *req.CompanyID
	}

	// xmax = 0 on a returned row means the INSERT added it (no conflict update).
	const returning = `RETURNING id, (xmax = 0), extra::text`
	var query string
	var args []any
	if req.Type == SolanaInstruction {
		query = `INSERT INTO assets(type, chain, parent, name, extra, task_ids)
VALUES ('instruction', $1, $2, $3, $4::jsonb, $5::bigint[])
ON CONFLICT ` + solanaInstructionConflict + ` DO UPDATE SET
    task_ids  = (SELECT ARRAY(SELECT DISTINCT unnest(assets.task_ids || EXCLUDED.task_ids))),
    last_seen = now()
` + returning
		args = []any{ChainSolana, req.Parent, req.Name, string(props), taskIDs}
	} else {
		query = `INSERT INTO assets(type, chain, address, parent, name, extra, company_id, task_ids)
VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), $6::jsonb, $7, $8::bigint[])
ON CONFLICT ` + solanaAddressConflict + ` DO UPDATE SET
    name       = COALESCE(assets.name, EXCLUDED.name),
    company_id = COALESCE(assets.company_id, EXCLUDED.company_id),
    task_ids   = (SELECT ARRAY(SELECT DISTINCT unnest(assets.task_ids || EXCLUDED.task_ids))),
    last_seen  = now()
` + returning
		args = []any{req.Type, ChainSolana, req.Address, req.Parent, name, string(props), companyID, taskIDs}
	}
	var (
		id       int64
		inserted bool
		stored   string
	)
	if err := s.tx.QueryRow(query, args...).Scan(&id, &inserted, &stored); err != nil {
		return 0, err
	}
	if inserted || len(req.Props) == 0 {
		return id, nil
	}
	var existing map[string]any
	if err := json.Unmarshal([]byte(stored), &existing); err != nil {
		return 0, fmt.Errorf("decode stored attributes of asset %d: %w", id, err)
	}
	merged, changed := mergeSolanaProps(existing, req.Props)
	if !changed {
		return id, nil
	}
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return 0, err
	}
	if _, err := s.tx.Exec(`UPDATE assets SET extra = $1::jsonb WHERE id = $2`, string(mergedJSON), id); err != nil {
		return 0, err
	}
	return id, nil
}

func nonNilProps(props map[string]any) map[string]any {
	if props == nil {
		return map[string]any{}
	}
	return props
}

// mergeSolanaProps merges the incoming attributes of an asset over its stored
// ones: a new scalar or object replaces the stored value, an array is united
// with it (stored order first). changed reports whether the result differs.
func mergeSolanaProps(stored, incoming map[string]any) (merged map[string]any, changed bool) {
	merged = make(map[string]any, len(stored)+len(incoming))
	for k, v := range stored {
		merged[k] = v
	}
	for k, in := range incoming {
		old, has := merged[k]
		oldArr, oldIsArr := old.([]any)
		inArr, inIsArr := in.([]any)
		if has && oldIsArr && inIsArr {
			union, added := unionJSONValues(oldArr, inArr)
			merged[k] = union
			changed = changed || added
			continue
		}
		if !has || !equalJSONValue(old, in) {
			changed = true
		}
		merged[k] = in
	}
	return merged, changed
}

// unionJSONValues appends to a every value of b not already in it.
func unionJSONValues(a, b []any) (out []any, added bool) {
	out = append([]any{}, a...)
	seen := make(map[string]bool, len(a)+len(b))
	key := func(v any) string {
		raw, _ := json.Marshal(v)
		return string(raw)
	}
	for _, v := range a {
		seen[key(v)] = true
	}
	for _, v := range b {
		k := key(v)
		if !seen[k] {
			seen[k] = true
			out = append(out, v)
			added = true
		}
	}
	return out, added
}

// equalJSONValue compares two decoded JSON values by their encoding.
func equalJSONValue(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// AddAutoSolanaScope records the task scope implied by one Solana asset the
// worker registered at the top level of insert_assets: the program itself. The
// assets under a program add nothing — the program's row already covers them.
// taskID<=0 is a no-op.
func (s *AssetStore) AddAutoSolanaScope(taskID int64, assetType, address string) error {
	if taskID <= 0 || assetType != SolanaProgram || !IsSolanaAddress(address) {
		return nil
	}
	return s.upsertTaskScope(TaskScope{TaskID: taskID, Kind: ScopeKindProgram, Value: strings.TrimSpace(address)})
}

// taskClusterRPCHosts returns the hostnames of the RPC endpoints of the cluster
// scope rows of the task that owns exploration node nodeID. A Solana asset is
// reached through these endpoints, so they are the hosts its evidence must be
// bound to. A task without a cluster scope has none.
func (s *ExplorationStore) taskClusterRPCHosts(ctx context.Context, nodeID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT ts.value
FROM exploration_nodes n
JOIN tasks t ON t.exploration_id = n.exploration_id
JOIN task_scope ts ON ts.task_id = t.id AND ts.kind = 'cluster'
WHERE n.id = $1`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hosts []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		u, err := url.Parse(SolanaClusterRPC(value))
		if err != nil {
			continue
		}
		if h := strings.ToLower(u.Hostname()); h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts, rows.Err()
}
