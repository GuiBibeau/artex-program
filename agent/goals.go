package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/llm"
	acperm "github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// goalsDefaultTmpl is the built-in EDITABLE body (section [A]) of the goals-decomposer
// prompt, seeded into agent_prompts. No template vars are used today.
const goalsDefaultTmpl = `You are a Solana program security goal decomposer. Your job is to identify the **final results to achieve** from the user's input, not to plan test steps.

**Step one (before decomposing goals): extract operation constraints**
From the "task goal / task description", identify the operator's explicit rules about [what may and may not be done], and register them one by one via set_constraints (if the description and goal involve no operation constraints, skip constraint extraction):
- type=deny: forbidden operations (e.g. "no transactions on mainnet", "simulate only, do not send transactions", "do not upgrade or deploy the program", "do not change any authority", "do not test any program other than <program id>", "spend at most 0.5 SOL").
- type=allow: explicitly allowed/bounded operation scope (e.g. "read-only RPC and simulation only", "live transactions on devnet only", "test on a local fork only", "only the program's own accounts").
- A constraint is neither a goal nor a test step: it is a rule about the boundaries of operational behavior.
- **Constraints must be [self-contained, with concrete targets written in]**: replace **deictic words** like "the current program / this program / the current cluster / this chain" with the **concrete values** from the task goal/description (the program id, the cluster label or RPC URL, the SOL or lamport amount). Constraints are injected separately into the execution-phase prompts; out of context, deictic words cannot be resolved.
  Example: if the goal names a program on devnet → write "live transactions only on devnet (https://api.devnet.solana.com), none on mainnet-beta", not "only on the current cluster".
- **Only register constraints [explicitly written or emphasized] in the goal/description — never invent any**; when unsure of the type, use deny (more conservative).
- If the goal/description truly contains no operation constraints, do **not** call set_constraints.
After registering constraints (if any), proceed to goal decomposition below.

**A goal = a final deliverable / verifiable result**

**What is NOT a goal (forbidden as sub-goals)**:
- Fetching the program account, IDL, bytecode or source; enumerating instructions, accounts, PDAs, authorities, mints or token accounts
- Vulnerability analysis and verification processes (simulation, fork tests, fuzzing, disassembly)
- Exploit steps: building, simulating or sending the attack transaction

**Decomposition principles**:
- The user's described final goal is single → output one
- Multiple **mutually independent** final deliverables → list them separately (e.g. "list every instruction that lets a non-authority move vault funds" and "prove the drain end to end")
- Annotate vulnclass with the Solana weakness class when it maps to one: missing_signer_check, missing_owner_check, account_confusion, pda_derivation, arbitrary_cpi, arithmetic_error, oracle_manipulation, stale_account_data, authority_misconfiguration, access_control, rent_or_close, logic_error, dos, information_disclosure. Leave it empty for inventory / mapping goals.
- Never invent goals the user did not mention

Call set_goals to submit the results.`

// goalsScopeTail is the code-owned tail appended after the editable goals body
// WHEN an asset store + task context are available. It teaches the decomposer to
// also lift the explicit asset scope out of the goal/description and register it
// via add_task_scope. Kept in code (not the DB-editable body) so it always applies
// on released DBs and can't be edited away — same pattern as the trafficTool tail.
const goalsScopeTail = `

**Extra duty: register the test asset scope**
Besides decomposing goals, you must also identify the **explicitly stated test asset scope** from the "task goal / task description" and register it via add_task_scope (this task's ` + taskScopeLabel + `: the denominator of asset test coverage, and the root under which auto-added assets count). **Minimal-scope principle: register only the targets the user explicitly named — never enlarge it on your own.**
- A **Solana program id** (a base58 key of 32 bytes, 32 to 44 characters, e.g. TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA) → one entry per program: kind=program, value=the program id exactly as written. Never add a program the user did not name (not a program it calls, not a dependency, not a "similar" program).
- A **cluster** the user names (devnet, testnet, mainnet, mainnet-beta, localnet) or an RPC URL → kind=cluster, value=the label or the URL; one entry per cluster. When the user names no cluster, register none.
- A **web host** the user names as a target (a docs site, a frontend, an API that fronts the program) → kind=subdomain with the **full hostname** of the URL or address (never shrink a subdomain-bearing host to its root domain: https://a1.lab.example.net → a1.lab.example.net, not example.net); only a bare root domain, or "the whole site / all subdomains", → kind=root_domain; a bare IP or range → kind=ip / cidr.
- A source repository, an IDL URL, a Git host, an explorer page or a documentation page is **information the workers fetch, not a target**: do not register its host.
- Do **not** register a company scope (company) — the task has just been created and the asset system usually has no such company yet; company-level scope is handled in the later plan phase.
Other rules:
- Only register scope **explicitly written in the goal/description**; never invent or infer unmentioned programs, clusters, domains or IPs.
- In reason, briefly state which sentence the entry is based on, for auditing.
- If the goal/description contains no explicit asset scope, do **not** call add_task_scope.
Register the scope via add_task_scope first (if any), then call set_goals to submit the goals.`

// GoalSpec is one decomposed objective.
type GoalSpec struct {
	Text      string `json:"text"`
	VulnClass string `json:"vulnclass,omitempty"`
}

// DecomposeGoals asks the LLM to break a pentest task goal into discrete,
// independently-verifiable objectives (each becomes a goal node). Returns nil if
// no provider is configured or the call yields nothing — the caller then falls
// back to a rule-based split so goal nodes always exist.
//
// prov is supplied by the caller (rather than built here from a Config) so goal
// decomposition rides the SAME provider instance as the rest of the engine — it
// shares the rate limiter, gets recorded by llmrec, and participates in LLM
// failover instead of quietly bypassing all three.
//
// desc is the task's free-text description (background: target scope / flag count / engagement notes, etc.).
// It is fed alongside the goal so the decomposer no longer splits blind — the
// prompt still forbids inventing anything the two texts don't state.
//
// emit, when non-nil, receives every LLM step (thinking/tool_use/result) with
// Worker="planner" so the round-0 goal-decomposition activity is visible in the UI.
//
// as + taskID, when non-nil/positive, wire the add_task_scope tool so the
// decomposer can register the explicit asset scope it extracts from the goal.
//
// ts is the task's exploration store: set_goals writes the decomposed goal nodes
// straight into it (the same managed tool the main agent uses to add goals at
// runtime). The returned specs are read back from the store so callers can emit
// per-goal activity and detect the "LLM produced nothing" case for their fallback.
func DecomposeGoals(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	return DecomposeGoalsWithProvider(ctx, prov, dataDir, goalText, desc, as, ts, taskID, false, 0, emit)
}

// DecomposeGoalsWithProvider is the task-runtime variant used when a task has an
// ordered provider chain. It preserves the same tools and write behavior while
// letting the caller own provider selection/failover. maxTokens is the profile's
// per-reply output cap (0 = send none).
func DecomposeGoalsWithProvider(ctx context.Context, prov llm.Provider, dataDir, goalText, desc string, as *db.AssetStore, ts *db.ExplorationStore, taskID int64, nonStreaming bool, maxTokens int, emit func(db.Activity)) []GoalSpec {
	if prov == nil {
		return nil
	}
	// Goal decomposition is a one-shot call: it does not attach a transcript store, so
	// agentcore does not put a session id on the ctx (it only does when there is a
	// writer, see agentcore.Prompt). Gateways that do prompt caching / sticky routing by
	// session-id header (opencode zen returns 400 MissingSessionID when
	// x-opencode-session is missing) read exactly this value on the ctx — without
	// attaching one you get "conversations fine, decomposition 400". Attach a stable id
	// explicitly: decomposition requests of the same exploration share it (good for
	// cache hits), the naming does not collide with planner/worker, and
	// llmrec.parseSession can attribute it correctly.
	if ts != nil {
		ctx = transcript.WithSessionID(ctx, fmt.Sprintf("exp%d-goals", ts.ID()))
	}
	// worker="goals" tags the goal nodes' provenance; ts/taskID let set_goals link
	// each goal under the task root. This is the catalog's real set_goals tool, so a
	// web-edited description/schema on it applies here too.
	tsx := &ToolSet{as: as, ts: ts, taskID: taskID, worker: "goals"}
	// Description rides in the user message (same channel as the goal), NOT via the
	// {{.EngagementDescription}} template var — else a prompt that references the var
	// would inject the description twice. System prompt stays pure static instructions.
	sys := renderSystem("goals", goalsDefaultTmpl, GoalsVars{DataDir: dataDir, Now: nowStr()})
	// set_constraints is always available (it does not depend on the asset store): the body
	// already contains the "extract operation constraints before decomposing goals" step
	// (wording editable on the agent edit page); here we only need to wire the tool.
	tools := []actool.CoreTool{tsx.setGoals(), tsx.setConstraints()}
	// Wire add_task_scope only when we have a real asset store + task to write to.
	// The scope-extraction tail is appended in lockstep so the prompt never asks for
	// a tool that isn't present.
	if as != nil && taskID > 0 {
		tools = append(tools, tsx.addTaskScope())
		sys += goalsScopeTail
	}
	userMsg := "Task goal:\n" + goalText
	if d := strings.TrimSpace(desc); d != "" {
		userMsg += "\n\nTask description (background information, may include target scope / flag count / engagement notes; for reference only — do not invent anything it does not mention):\n" + d
	}
	// Use captureRun so every LLM step is emitted as an activity record (visible in
	// the plan tab under the round-0 marker). Falls back gracefully when emit is nil.
	captureEmit := func(r db.Activity) {
		if emit != nil {
			r.Worker = "planner"
			emit(r)
		}
	}
	captureRun(ctx, agentcore.Options{
		Provider:               prov,
		SystemPrompt:           []string{sys},
		Tools:                  tools,
		PermissionMode:         acperm.ModeBypass,
		DisableBackgroundTasks: true,
		// 3 steps (extract constraints → register scope → decompose goals) each need one tool call; give enough turns so set_goals is not missed before wrap-up.
		MaxTurns:     8,
		NonStreaming: nonStreaming, // when this profile selects non-streaming, use Provider.Complete
		MaxTokens:    maxTokens,    // 0 = send no cap, the server's default decides
	}, userMsg, captureEmit)
	// set_goals persisted the goals directly; read them back so the caller sees what
	// was written (empty slice ⇒ the LLM produced nothing ⇒ caller falls back).
	if ts == nil {
		return nil
	}
	nodes, _ := ts.ListByKind(db.KindGoal, 10000)
	var out []GoalSpec
	for _, n := range nodes {
		var p struct {
			Text      string `json:"text"`
			VulnClass string `json:"vulnclass"`
		}
		_ = json.Unmarshal(n.Payload, &p)
		if strings.TrimSpace(p.Text) != "" {
			out = append(out, GoalSpec{Text: p.Text, VulnClass: p.VulnClass})
		}
	}
	return out
}
