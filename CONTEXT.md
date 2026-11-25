# ARTEX Program: domain model

ARTEX Program tests **Solana programs**. It is derived from [ARTEX](https://github.com/BaaRai/ARTEX)
and keeps its engine (two linked graphs, planner and workers, approval gates, budgets). This file
records the terms the code, the prompts and the skills share. Change a term here first.

## The target

| Term | Meaning | Where it lives |
| --- | --- | --- |
| **program** | An on-chain program, identified by its **program id** (a base58 key of 32 bytes). The root of the asset tree. | `assets` row of type `program`, `chain = solana` |
| **instruction** | One entrypoint of a program: a name, an 8-byte discriminator, arguments, and the accounts it takes with their signer / writable / PDA constraints. | `assets` type `instruction`, unique per `(parent, name)` |
| **account** | An account the program owns or uses, PDAs included (`props.pda`, `props.seeds`, `props.bump`). | `assets` type `account`, unique per `(type, address)` |
| **authority** | A key that holds a privilege: the upgrade authority, an admin, a mint, freeze or vault authority. One asset per key; `props.controls` accumulates. | `assets` type `authority` |
| **mint**, **token_account** | SPL Token or Token-2022 state the program moves value through. | `assets` types `mint`, `token_account` |
| **parent** (`program_id` in the tools) | The program an asset belongs to. Every type but `program` carries one, so the tree is one level deep. | `assets.parent` |
| **cluster** | The network an engagement runs on: `devnet`, `testnet`, `mainnet-beta`, `localnet`, or an RPC URL. | `task_scope` row of kind `cluster` |
| **props** | The structured attributes of an asset (what other assets call `extra`). Arrays are united on re-registration; scalars are replaced. | `assets.extra` |

Network assets (`root_domain`, `subdomain`, `ip`, `service`, `app`, `endpoint`) stay in the model
for the web assets around a program (a frontend, a documentation portal).

## Scope and proof

- A task's **`program` scope** (value = program id) covers the program and every asset whose
  `parent` is that id (`db.solanaScopePredicate`). It is the coverage denominator.
- The **`cluster` scope** names the RPC endpoint the evidence must come from.
- A **receipt** is the recorded JSON-RPC exchange on that endpoint. A finding on a Solana asset
  closes a goal when it has a bound, verified exchange whose host is the RPC host of the task's
  cluster scope (`db.taskClusterRPCHosts`). A proof that ran on a local validator or fork has no
  exchange: it closes a goal through an observed `tool_output` fact.
- The **proof basis** of a finding is `executed-live` (landed on the in-scope cluster),
  `executed-lab` (simulation, fork, local validator) or `static-analysis-unexecuted`. Only the
  first two make a confirmed finding.

## The money rules

1. Reads and simulation are free: `getAccountInfo`, `getProgramAccounts`, `simulateTransaction`,
   dumps, IDL fetches, local validators and forks.
2. A live transaction spends funds and changes state. It needs the in-scope cluster, the
   operator's constraints, the smallest amount, and the approval gate.
3. Mainnet-beta state changes need an explicit operator approval of that exact transaction.
4. Workers own no funds. A worker creates a wallet in its work directory, seals the keypair in the
   task credentials, and asks the operator (`ask_operator`) for the funds. A key never reaches a
   fact, a finding or the model's context.
5. The public faucets rate-limit by IP: the fallbacks are the web faucet, a transfer, or a local
   fork when the goal allows executed-lab evidence.

The built-in approval rules (`db/intercept_solana_seed.go`) implement rule 2 for the commands the
platform can see: `solana` money, staking, voting and program commands, `spl-token` state changes,
`anchor deploy` and `upgrade`, and a command line that calls `sendTransaction`. A transaction sent
from inside a script is gated by the operator's constraints and the fallback judge, not by a
command pattern.

## Weakness classes

`missing_signer_check`, `missing_owner_check`, `account_confusion`, `pda_derivation`,
`arbitrary_cpi`, `arithmetic_error`, `oracle_manipulation`, `stale_account_data`,
`authority_misconfiguration`, `access_control`, `rent_or_close`, `logic_error`, `dos`,
`information_disclosure`.

## Method

The skills are the method: `solana-recon` (map), `anchor-audit` (read the source),
`solana-bytecode` (read the SBF when there is no source), `solana-exploit` (prove). The order of
proof is simulation, then a local fork or validator, then the smallest live transaction.
