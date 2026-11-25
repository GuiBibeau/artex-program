---
name: solana-bytecode
description: "Invoke this skill when a Solana program has no IDL and no source and must be understood from its SBF bytecode: dumping the ELF, recovering instruction names, account roles, error codes, seeds, CPI targets and arithmetic checks from symbols, strings and disassembly. Static results are labeled static-analysis-unexecuted until a simulation or a transaction reproduces them."
---

# Solana bytecode analysis (SBF, without source)

Use this when the IDL fetch and the source search came back empty. The goal is the same map the IDL would give: instructions, accounts, PDAs, authorities, CPI targets — and enough certainty to build a PoC that is then **executed**. Reading bytecode produces candidates; only a simulation or a transaction turns a candidate into a finding.

## 1. Get the binary

- `solana program dump <PROGRAM_ID> program.so` (it follows the programdata pointer for upgradeable programs). Keep the file in the work dir and record its sha256 in the program asset's `props` (`data_sha256`): after an upgrade, a different hash is the start of an upgrade review (see the patch-diff skill).
- `file program.so` → an ELF 64-bit executable for the SBF target. `llvm-readelf -h program.so` confirms the machine (eBPF) and the entry point.

## 2. Cheap wins first: symbols and strings

Anchor and most Rust programs keep their symbol table and their panic or error strings. They often give the instruction list and every error code in minutes:

- Disassembler: `llvm-objdump` from an `llvm` package (apt install llvm) or `rust-objdump` (`cargo install cargo-binutils`). The Solana platform-tools copy is at `<solana install>/bin/platform-tools-sdk/sbf/dependencies/platform-tools/llvm/bin/` where it exists: a release install has it after one `cargo-build-sbf --version` run, which downloads platform-tools into `~/.cache/solana/`. Any LLVM with BPF support works.
- `strings -n 6 program.so | grep -iE 'instruction|authority|signer|owner|bump|seed|insufficient|unauthorized|invalid|overflow|exceeded'`
- Anchor error codes look like `ErrorCode: 6000` style offsets with their message strings in order (custom errors start at 6000); the messages tell you what each check rejects, and a missing message for a check you expected is a lead.
- `llvm-nm program.so | grep -iE 'handler|instruction|withdraw|deposit|transfer|authority'` lists the functions of the dispatch; Anchor generates one handler per instruction with a readable name.

## 3. The entrypoint and the dispatch

- `llvm-objdump -d --no-show-raw-insn program.so > program.asm` in the work dir.
- The `entrypoint` function reads the input: the first bytes carry the number of accounts, then their metadata, then the instruction data. Anchor programs compare the first 8 bytes of the instruction data with each discriminator and jump to the handler: each comparison block names an instruction (match the discriminator against sha256 of `global:<name>` for the names you recovered from strings).
- In a handler, the account metadata flags (is_signer, is_writable) are read from the account info array: a handler that never reads the signer flag of an account is a missing-signer candidate; one that never compares the account owner is a missing-owner candidate.

## 4. What to look for, by weakness class

| Class | In the disassembly |
| --- | --- |
| missing_signer_check | a handler uses an account's key or data without a preceding load of its signer flag |
| missing_owner_check | an account's data is deserialized with no comparison of its owner field against the expected program id |
| pda_derivation | a `create_program_address` syscall fed with a bump byte taken from instruction data (instead of the canonical bump) or seeds that skip a key |
| arbitrary_cpi | an `invoke_signed` whose program account comes straight from the caller's account list |
| arithmetic_error | plain add / mul / sub on 64-bit or 128-bit values with no overflow branch afterwards (checked math has a branch to a panic) |
| stale_account_data | account data loaded into a register before an `invoke` and used again after it without a reload |
| authority_misconfiguration | an authority key compared against a constant baked into the binary, or read from an account anyone can create |

Syscalls appear by name in the disassembly (`sol_invoke_signed_c`, `sol_create_program_address`, `sol_try_find_program_address`...): grep for them, then read the call sites.

## 5. Cross-check on chain

Static reading is the hypothesis. Confirm what you can without a transaction:

- The discriminators you matched: send the instruction to `simulateTransaction` (the solana-exploit skill) and read the program log; an unknown instruction fails with `InstructionFallbackNotFound` or a custom error, a matched one gets further.
- The error strings: trigger the matching path in a simulation and compare the custom error number.
- The account layout: fetch the accounts with `getProgramAccounts` and check that the size matches what the deserialization code expects.

## 6. Record

- Register the instructions and accounts you recovered with `insert_assets`, and say in `props` that they come from bytecode (`source: "bytecode"`, with the symbol or address you matched).
- Facts from this skill are `source=tool_output`, confidence `observed` for what the binary shows and `inferred` for what you conclude from it.
- A candidate is labeled **static-analysis-unexecuted** until the solana-exploit skill reproduces it: it stays a fact for the planner, never a finding.
