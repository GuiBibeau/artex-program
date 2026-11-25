---
name: solana-recon
description: "Invoke this skill when an intent needs to map a Solana program on a cluster: the program account and its upgradeability, its IDL or bytecode, every instruction with its accounts, the accounts and PDAs the program owns, the authorities that control it, and the mints and token accounts it moves. Read-only RPC first; everything found is registered in the asset graph."
---

# Solana program recon (mapping the on-chain surface)

Under an **authorized** engagement, map the program the way an attacker builds a target list: what the program is, who can change it, what it exposes, what it owns, and where the value is. Mapping is read-only: RPC reads, dumps and IDL fetches change nothing and need no approval. The money rules in your system prompt apply to everything that comes after this skill.

## 0. Endpoint and tools

- Use the RPC endpoint of the task's cluster scope: devnet https://api.devnet.solana.com, testnet https://api.testnet.solana.com, mainnet-beta https://api.mainnet-beta.solana.com, localnet http://127.0.0.1:8899, or the URL the scope names. `solana config set --url <RPC>` sets it for the CLI; curl and scripts take it directly.
- CLI: `solana account`, `solana program dump`, `solana-keygen`, `spl-token display`, `anchor idl fetch`.
- JSON-RPC over HTTP is the common denominator; every CLI makes the same calls:

      curl -s <RPC> -H 'content-type: application/json' \
        -d '{"jsonrpc":"2.0","id":1,"method":"getAccountInfo","params":["<ADDRESS>",{"encoding":"base64"}]}'

- RPC calls made from the shell are recorded by the platform proxy. Keep them: they become the receipts of later findings.

## 1. The program account

- `getAccountInfo(program id)`: `owner` says how the program is loaded, `executable` confirms it is a program.
  - `BPFLoaderUpgradeab1e11111111111111111111111` → **upgradeable**. The program account (36 bytes on the current runtime: a 4-byte tag 2 and a 4-byte programdata index) points at a **programdata** account that holds the bytecode and the upgrade authority. Its address is the PDA of seeds `[program id]` under the loader.
  - `BPFLoader2111111111111111111111111111111111` → the last non-upgradeable loader: the bytecode sits in the program account itself and the program cannot be upgraded.
  - The deprecated loader (address starts with `BPFLoader1`) → same, legacy.
- **Programdata account layout** (upgradeable): a 4-byte tag 3, 8 bytes of the last deploy slot, a 4-byte option tag and, when the option is 1, the 32-byte **upgrade authority**; when the option is 0 the authority was revoked and the program is immutable. Record: `upgradeable`, `upgrade_authority` (address or `revoked`), `last_deploy_slot`.
- `solana program dump <PROGRAM_ID> program.so` writes the ELF bytecode to the work dir; it follows the programdata pointer for upgradeable programs.

## 2. Instructions: the IDL, else the bytecode

- `anchor idl fetch <PROGRAM_ID> --outfile idl.json` reads the on-chain IDL account (Anchor programs store it at seeds `[b"anchor:idl", program id]`). The IDL lists every instruction: its name, its discriminator (the first 8 bytes of sha256 of `global:<name>`), its arguments with types, and its accounts with the `signer` / `writable` / `pda` (with seeds) flags and docs.
- Other sources, in order: the source repository (for Anchor, the `#[program]` module under `programs/`), the project documentation, a published package.
- No IDL and no source → switch to the solana-bytecode skill.
- Register every instruction with `insert_assets`: `type=instruction`, `program_id`, `name`, `props` = discriminator, args, accounts (name, writable, signer, pda, seeds), docs. One instruction is one asset, however often it is called.

## 3. Accounts and PDAs

- Every PDA named in the IDL or the source: its seeds and bump. Derive addresses with a short script (`PublicKey.findProgramAddressSync(seeds, programId)` in @solana/web3.js, `getProgramDerivedAddress` in @solana/kit, `Pubkey::find_program_address` in Rust). Anchor seeds end with the `[bump]` byte: the canonical bump of `find_program_address`, never a caller-supplied one.
- `getProgramAccounts` lists the accounts the program owns right now. Filter before you fetch — public RPC nodes rate-limit this call:

      {"jsonrpc":"2.0","id":1,"method":"getProgramAccounts","params":["<PROGRAM_ID>",
       {"encoding":"base64","filters":[{"dataSize":<N>},
        {"memcmp":{"offset":0,"bytes":"<8-byte discriminator, base58>"}}]}]}

  One `dataSize` per account type (Anchor accounts start with the 8-byte account discriminator at offset 0).
- For each account record: lamports, owner (it must be the program for state accounts), data size, the parsed state (decode it with the IDL's type), and whether it is a PDA. Register with `insert_assets`: `type=account`, `address`, `program_id`, `props` = owner, lamports, space, data_sha256, pda, seeds, bump, parsed.

## 4. Authorities

- The upgrade authority, from the programdata account (section 1).
- Admin keys stored in state accounts (the IDL names them: authority, admin, owner, manager).
- Token authorities: `mint_authority` and `freeze_authority` of every mint (`spl-token display <MINT>` or `getParsedAccountInfo` on the mint); the owner and the delegate of every token account.
- For each authority ask: is it a PDA (then the program itself signs — which instruction makes it sign, and who can call that instruction?) or an external key (then a key compromise or a shared key is the risk)? Is the same key an authority over several assets?
- Register with `insert_assets`: `type=authority`, `address`, `program_id`, `props` = role (upgrade_authority, admin, mint_authority, freeze_authority, vault_authority...), controls `[{asset, role}]`. One asset per key; re-registering the key adds to its controls.

## 5. Mints and token accounts (follow the value)

- Every mint the program touches: decimals, supply, mint_authority, freeze_authority, the token program (`TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA` for SPL Token, `TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb` for Token-2022), and its extensions (transfer fee, transfer hook, permanent delegate, confidential transfers).
- Every vault, escrow or fee token account: its mint, its owner (a PDA of the program, or an external key?), its amount, its delegate. `getParsedAccountInfo` returns the parsed JSON; `getTokenAccountsByOwner` lists the accounts of an owner.
- Register: `type=mint` and `type=token_account`, with the fields above in `props`.

## 6. Dependencies and the CPI surface

- Which programs it calls (from the IDL's `Program` account types, the source, or the bytecode): the two token programs, the associated token program `ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL`, the sysvars (`SysvarC1ock11111111111111111111111111111111` for the clock, `Sysvar1nstructions1111111111111111111111111` for the instructions), `ComputeBudget111111111111111111111111111111`, and the other DeFi programs.
- A program account that the **caller** passes in, with no owner or address constraint, is the arbitrary-CPI surface. Note each one in the instruction's `props.accounts` (the account name, and why it is unchecked).

## 7. Record and hand over

- Register the assets (`insert_assets`) before the summary fact. One fact per mapping intent: the counts (instructions, accounts, PDAs, authorities, mints, token accounts), the upgradeability, and the three most interesting surfaces with the reason.
- A candidate is a hypothesis ("withdraw takes an authority account with no signer check"): hand it to the planner in the fact's summary; the solana-exploit skill tests it.
- A program with no IDL, no source and unknown bytecode is a result too: say so, and point at the solana-bytecode skill.
