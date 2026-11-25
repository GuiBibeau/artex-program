-- Solana program assets (ARTEX Program).
-- The idempotent schema supplies the DDL; these statements bring an existing
-- database to the same shape: the on-chain key columns, the widened asset type
-- list, and the program / cluster task scope kinds.
ALTER TABLE assets ADD COLUMN IF NOT EXISTS chain   TEXT;
ALTER TABLE assets ADD COLUMN IF NOT EXISTS address TEXT;
ALTER TABLE assets ADD COLUMN IF NOT EXISTS parent  TEXT;
ALTER TABLE assets ADD COLUMN IF NOT EXISTS name    TEXT;

ALTER TABLE assets DROP CONSTRAINT IF EXISTS assets_type_check;
ALTER TABLE assets ADD CONSTRAINT assets_type_check
    CHECK (type IN ('root_domain','ip','subdomain','app','service','endpoint',
                    'program','instruction','account','authority','mint','token_account'));

CREATE UNIQUE INDEX IF NOT EXISTS uq_sol_address
    ON assets(type, address)
    WHERE chain = 'solana' AND type IN ('program','account','authority','mint','token_account');
CREATE UNIQUE INDEX IF NOT EXISTS uq_sol_instruction
    ON assets(parent, name)
    WHERE chain = 'solana' AND type = 'instruction';
CREATE INDEX IF NOT EXISTS idx_av2_chain_address ON assets(chain, address) WHERE address IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_av2_parent       ON assets(parent)       WHERE parent IS NOT NULL;

ALTER TABLE task_scope DROP CONSTRAINT IF EXISTS task_scope_kind_check;
ALTER TABLE task_scope ADD CONSTRAINT task_scope_kind_check
    CHECK (kind IN ('company','root_domain','subdomain','ip','cidr','icp','keyword','program','cluster'));
