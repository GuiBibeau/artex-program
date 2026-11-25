---
name: anchor-audit
description: "Invoke this skill when the source of an Anchor (or native Rust) Solana program is available and the task is a white-box security review of it: reading every instruction's account constraints for missing signer, owner, PDA and mint checks, unchecked CPI targets, stale account data, arithmetic and authority misconfiguration, mapping each candidate to its instruction and accounts, and handing the reproducible ones to the solana-exploit skill."
---

# Anchor program audit (white-box review of program source)

Under an **authorized** engagement, read the program the way an attacker chooses an instruction: for each instruction, what does a caller control, and what does the program trust? The standing finding quality gates and the adversarial gate in your system prompt apply. Reading source produces candidates; a finding needs the solana-exploit skill to trigger it.

## 0. Get and build the source

- Clone the repository into the work dir (`git clone --depth 1 <url>`), and read the version that matches the deployed program: compare the on-chain programdata slot with the repository history, and the commit in the IDL's `metadata` when it has one.
- `anchor build` produces the IDL (`target/idl/<name>.json`) and the deploy keypair; `NO_DNA=1 anchor test` runs the test suite on a local network (Surfpool by default in Anchor 1.x). Compare the built IDL with the on-chain one (`anchor idl fetch`): a difference means the deployed program is not this source.
- Read `Anchor.toml`, `Cargo.toml` (the anchor-lang and spl-token versions: older anchor-lang lacks some checks), and the `declare_id!` values.

## 1. The constraint vocabulary

In `#[derive(Accounts)]`, the wrapper of each field states what the framework checks for you:

- `Signer<'info, T>`: the account signed the transaction. `AccountInfo` / `UncheckedAccount`: nothing is checked.
- `Account<'info, T>`: owned by the program and deserialized as T (with the 8-byte account discriminator). `AccountLoader<'info, T>`: owned by the program, loaded lazily. `UncheckedAccount` / `AccountInfo`: no owner check, so the caller may pass an account of another program → **account confusion**.
- `Program<'info, T>` (or `Interface<'info, T>`): the account is the expected program. An `UncheckedAccount` used as the program of an `invoke` → **arbitrary CPI**.
- `#[account(signer)]`, `#[account(has_one = authority)]`, `#[account(constraint = a.key() == b.key())]`, `#[account(seeds = [...], bump)]`, `#[account(owner = token_program::ID)]`, `#[account(mint::authority = ...)]`, `#[account(mint::decimals = 9)]`.

## 2. Per instruction: the questions

Walk every handler in `#[program]` and answer, for every account:

1. **Who must sign?** An authority / owner / admin field that is `UncheckedAccount`, or that has no `signer`, `has_one` or `constraint` → `missing_signer_check`. Check program-owned signers too: an authority PDA signs only through `invoke_signed`; a field declared `signer` that is meant to be a PDA can never be satisfied.
2. **Who owns it?** Data the handler deserializes by hand (`try_deserialize`, `bytemuck::from_bytes`, `AnchorDeserialize::try_from_slice`) with no check of `.owner` → `missing_owner_check` / account confusion (type cosplay).
3. **Is it the right one?** A token account not checked against the expected mint, a state account not bound to its user or pool (`has_one = user`, seeds that include the user key), a vault not bound to the pool → `account_confusion`.
4. **PDA derivation**: the `seeds` of the constraint against the `find_program_address` call; a bump read from instruction data instead of the stored canonical bump; `create_program_address` with a caller-supplied bump; two seed tuples that can produce the same address → `pda_derivation`.
5. **CPI targets**: every `invoke` / `invoke_signed` must use a `Program` / `Interface` account or compare `.key()` with the expected program id. Seeds built from caller-controlled data let the program sign as a PDA the caller chose → `arbitrary_cpi`.
6. **Order of reads and CPI**: account data loaded before an `invoke` that can change that account (a token transfer changes balances; a CPI into another program can change any writable account) and used again after it → `stale_account_data`.
7. **Arithmetic**: `+`, `-`, `*`, `/` on u64 / i64 / u128 without `checked_*`, `saturating_*` or `wrapping_*` (a release build wraps silently); the order of multiply and divide in decimal scaling; rounding in the protocol's favor or the user's; Token-2022 transfer fees not accounted for → `arithmetic_error`.
8. **Authority lifecycle**: `set_authority` with an authority PDA that a caller can drive; mint and freeze authorities not renounced after setup; an upgrade authority on chain that is not revoked or is shared (see solana-recon) → `authority_misconfiguration`.
9. **Close and rent**: `#[account(close = recipient)]` in an instruction that can also `init` an account at the same address, or no discriminator check before a closed account is reused → `rent_or_close` (close-account revival); lamports returned to an account the caller controls.
10. **Access control**: an instruction that changes configuration (fees, oracles, authorities) and is callable by anyone, or by a role key that any user can create → `access_control`.
11. **Oracles and prices**: a price taken from pool reserves or from a single swap, a Pyth price without a confidence interval or staleness check, and usable in the same transaction as the swap → `oracle_manipulation`.
12. **Instruction introspection**: where a transfer must happen in the same transaction, `sysvar::instructions::load_instruction_at_checked` must be used; without it a caller can skip the payment.

## 3. Register and record

- Register the instructions and accounts you confirm from the source with `insert_assets` (`props.source: "source"`, with file:line).
- One fact per group of instructions reviewed: what is checked, what is missing, with file:line and the exact constraint that is absent. Confidence `observed` for what the code says, `inferred` for the impact.
- Candidates go to the planner as facts and, when you can build them, to solana-exploit: "handler X, account field Y is UncheckedAccount and is used as the authority at line N: a transaction signed by any key passes".

## 4. Quick reproductions (local, free)

- `NO_DNA=1 anchor test`: add a test in `tests/` that builds the attack transaction with a keypair that is not the authority and asserts that it succeeds. A passing attack test is an executed-lab proof.
- Trident for arithmetic and state-machine properties: `trident fuzz add <name>`, then `trident fuzz run <name>`.
