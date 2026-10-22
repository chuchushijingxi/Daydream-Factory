CREATE SCHEMA IF NOT EXISTS drama_catalog_v1;
-- Coalesced delivery work, not a second business ledger. Held until Redis acknowledges.
CREATE TABLE IF NOT EXISTS drama_catalog_v1.runtime_events (
 key text PRIMARY KEY, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION drama_catalog_v1.enqueue_runtime(k text) RETURNS void LANGUAGE sql AS $$
 INSERT INTO drama_catalog_v1.runtime_events(key) VALUES(k)
 ON CONFLICT(key) DO UPDATE SET created_at=EXCLUDED.created_at;
$$;
CREATE TABLE IF NOT EXISTS drama_catalog_v1.spaces (
 id text PRIMARY KEY CHECK(id<>''), kind text NOT NULL CHECK(kind IN ('personal','team')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.projects (
 id text PRIMARY KEY, revision bigint NOT NULL CHECK(revision>0), payload jsonb NOT NULL,
 space_id text NOT NULL REFERENCES drama_catalog_v1.spaces(id),
 owner_id text NOT NULL CHECK(owner_id<>''), creator_id text NOT NULL CHECK(creator_id<>''),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK(payload->>'id'=id), CHECK((payload->>'revision')::bigint=revision)
);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.model_config (
 id text PRIMARY KEY, revision bigint NOT NULL CHECK(revision>0), connections jsonb NOT NULL
);
-- One-time schema upgrade of this feature's previous singleton; stable model IDs,
-- encrypted credentials and project payloads are retained. No runtime dual reading.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='drama_catalog_v1' AND table_name='model_config' AND column_name='base_url') THEN
  ALTER TABLE drama_catalog_v1.model_config ADD COLUMN IF NOT EXISTS connections jsonb;
  UPDATE drama_catalog_v1.model_config SET connections=jsonb_build_array(jsonb_build_object(
   'id','deepseek','name','DeepSeek','kind',CASE WHEN base_url IN ('https://api.deepseek.com','https://api.deepseek.com/v1') THEN 'official' ELSE 'gateway' END,
   'protocol','deepseek','baseUrl',base_url,'secret',encode(credential,'base64'),
   'models',jsonb_build_array(
    jsonb_build_object('id','deepseek-flash','modelId','deepseek-flash','alias','','category','analysis','enabled',enabled,'contextWindow',CASE WHEN base_url IN ('https://api.deepseek.com','https://api.deepseek.com/v1') THEN 1000000 ELSE 0 END,'maxOutputTokens',CASE WHEN base_url IN ('https://api.deepseek.com','https://api.deepseek.com/v1') THEN 393216 ELSE 0 END,'pricing',CASE WHEN base_url IN ('https://api.deepseek.com','https://api.deepseek.com/v1') THEN jsonb_build_object('inputCnyPerMillion',2,'outputCnyPerMillion',8,'cacheHitHundredthsCnyPerMillion',4) ELSE 'null'::jsonb END),
    jsonb_build_object('id','deepseek-v4-pro','modelId','deepseek-v4-pro','alias','','category','analysis','enabled',pro_enabled,'contextWindow',CASE WHEN base_url IN ('https://api.deepseek.com','https://api.deepseek.com/v1') THEN 1000000 ELSE 0 END,'maxOutputTokens',CASE WHEN base_url IN ('https://api.deepseek.com','https://api.deepseek.com/v1') THEN 393216 ELSE 0 END,'pricing',CASE WHEN base_url IN ('https://api.deepseek.com','https://api.deepseek.com/v1') THEN jsonb_build_object('inputCnyPerMillion',9,'outputCnyPerMillion',27,'cacheHitHundredthsCnyPerMillion',30) ELSE 'null'::jsonb END)
   ))) WHERE connections IS NULL;
  ALTER TABLE drama_catalog_v1.model_config DROP COLUMN base_url,DROP COLUMN enabled,DROP COLUMN pro_enabled,DROP COLUMN credential;
  ALTER TABLE drama_catalog_v1.model_config ALTER COLUMN connections SET NOT NULL;
 END IF;
 ALTER TABLE drama_catalog_v1.model_config DROP CONSTRAINT IF EXISTS model_config_id_check;
 UPDATE drama_catalog_v1.model_config SET id='workspace' WHERE id='deepseek';
END $$;
INSERT INTO drama_catalog_v1.model_config(id,revision,connections) VALUES('workspace',1,
 '[{"id":"deepseek","name":"DeepSeek","kind":"official","protocol":"deepseek","baseUrl":"https://api.deepseek.com","secret":"","models":[{"id":"deepseek-flash","modelId":"deepseek-flash","alias":"","category":"analysis","enabled":false,"contextWindow":1000000,"maxOutputTokens":393216,"pricing":{"inputCnyPerMillion":2,"outputCnyPerMillion":8,"cacheHitHundredthsCnyPerMillion":4}},{"id":"deepseek-v4-pro","modelId":"deepseek-v4-pro","alias":"","category":"analysis","enabled":false,"contextWindow":1000000,"maxOutputTokens":393216,"pricing":{"inputCnyPerMillion":9,"outputCnyPerMillion":27,"cacheHitHundredthsCnyPerMillion":30}}]}]') ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS drama_catalog_v1.model_logos (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{64}$'),
 png bytea NOT NULL CHECK(octet_length(png)>0 AND octet_length(png)<=65536)
);

CREATE TABLE IF NOT EXISTS drama_catalog_v1.platform_logos (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{64}$'),
 png bytea NOT NULL CHECK(octet_length(png)>0 AND octet_length(png)<=65536)
);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.platform_settings (
 id boolean PRIMARY KEY CHECK(id),
 revision bigint NOT NULL DEFAULT 0 CHECK(revision>=0 AND revision<=9007199254740991),
 name text NOT NULL DEFAULT '短剧 Agent' CHECK(char_length(name) BETWEEN 1 AND 48),
 sidebar_description text NOT NULL DEFAULT '创作工作台' CHECK(char_length(sidebar_description)<=120),
 logo_id text REFERENCES drama_catalog_v1.platform_logos(id)
);
INSERT INTO drama_catalog_v1.platform_settings(id) VALUES(true) ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS drama_catalog_v1.uploaded_files (
 id text PRIMARY KEY, payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 space_id text NOT NULL REFERENCES drama_catalog_v1.spaces(id),
 owner_id text NOT NULL CHECK(owner_id<>''), creator_id text NOT NULL CHECK(creator_id<>''),
 CHECK(payload->>'id'=id)
);

-- Provider jobs are independent of projects; snapshots stay immutable during polling.
CREATE TABLE IF NOT EXISTS drama_catalog_v1.tasks (
 id text PRIMARY KEY, scope_id text NOT NULL, command_id text NOT NULL,
 space_id text NOT NULL REFERENCES drama_catalog_v1.spaces(id),
 owner_id text NOT NULL CHECK(owner_id<>''), creator_id text NOT NULL CHECK(creator_id<>''),
 status text NOT NULL CHECK(status IN ('queued','running','unknown','succeeded','failed','cancelled')),
 revision bigint NOT NULL CHECK(revision>0), next_run_at timestamptz,
 lease_token text NOT NULL DEFAULT '', lease_until timestamptz,
 input_snapshot jsonb NOT NULL, payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK((lease_token='')=(lease_until IS NULL))
);
CREATE INDEX IF NOT EXISTS tasks_due_idx ON drama_catalog_v1.tasks(next_run_at,id) WHERE next_run_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS tasks_scope_idx ON drama_catalog_v1.tasks(scope_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS tasks_image_fill_idx ON drama_catalog_v1.tasks(id) WHERE status='succeeded' AND input_snapshot#>>'{callerContext,fillMissingImage}'='true';


-- Resource ownership is explicit; no unowned content or legacy fallback.
CREATE INDEX IF NOT EXISTS projects_space_idx ON drama_catalog_v1.projects(space_id,updated_at DESC);
CREATE INDEX IF NOT EXISTS projects_space_owner_idx ON drama_catalog_v1.projects(space_id,owner_id,updated_at DESC);
CREATE INDEX IF NOT EXISTS uploads_space_created_idx ON drama_catalog_v1.uploaded_files(space_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS uploads_space_owner_idx ON drama_catalog_v1.uploaded_files(space_id,owner_id,id);
CREATE INDEX IF NOT EXISTS tasks_space_owner_idx ON drama_catalog_v1.tasks(space_id,owner_id,scope_id,created_at DESC,id DESC);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.users (
 id text PRIMARY KEY, email text UNIQUE NOT NULL, username text NOT NULL,
 role text NOT NULL CHECK(role IN ('user','operator')), enabled boolean NOT NULL DEFAULT true,
 password_hash bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
-- Team contract and membership facts are kept separate from content and credits.
CREATE TABLE IF NOT EXISTS drama_catalog_v1.teams (
 space_id text PRIMARY KEY REFERENCES drama_catalog_v1.spaces(id),
 name text NOT NULL CHECK(char_length(name) BETWEEN 1 AND 40),
 owner_id text NOT NULL REFERENCES drama_catalog_v1.users(id),
 member_limit integer NOT NULL CHECK(member_limit>=1),
 expires_at timestamptz NOT NULL, disabled boolean NOT NULL DEFAULT false,
 grant_amount bigint NOT NULL DEFAULT 0 CHECK(grant_amount BETWEEN 0 AND 9007199254740991),
 policy text NOT NULL DEFAULT 'read_all' CHECK(policy IN ('isolated','read_all','edit_all')),
 opened_at timestamptz NOT NULL DEFAULT now(), next_grant_at timestamptz NOT NULL,
 grant_sequence bigint NOT NULL DEFAULT 0 CHECK(grant_sequence>=0),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision BETWEEN 1 AND 9007199254740991)
);
CREATE INDEX IF NOT EXISTS teams_expiration_idx ON drama_catalog_v1.teams(expires_at,space_id) WHERE NOT disabled;
CREATE TABLE IF NOT EXISTS drama_catalog_v1.team_members (
 space_id text NOT NULL REFERENCES drama_catalog_v1.teams(space_id),
 user_id text NOT NULL REFERENCES drama_catalog_v1.users(id),
 role text NOT NULL CHECK(role IN ('owner','admin','member')), joined_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(space_id,user_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS team_single_owner_uidx ON drama_catalog_v1.team_members(space_id) WHERE role='owner';
CREATE INDEX IF NOT EXISTS team_members_user_idx ON drama_catalog_v1.team_members(user_id,space_id);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.team_model_discounts (
 space_id text NOT NULL REFERENCES drama_catalog_v1.teams(space_id), model_instance_id text NOT NULL CHECK(model_instance_id<>''),
 percent integer NOT NULL CHECK(percent BETWEEN 1 AND 100), PRIMARY KEY(space_id,model_instance_id)
);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.team_invites (
 id text PRIMARY KEY, space_id text NOT NULL REFERENCES drama_catalog_v1.teams(space_id),
 kind text NOT NULL CHECK(kind IN ('link','email')), token_digest text UNIQUE,
 invitee_user_id text REFERENCES drama_catalog_v1.users(id), created_by text NOT NULL REFERENCES drama_catalog_v1.users(id),
 expires_at timestamptz NOT NULL, revoked_at timestamptz,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','accepted','declined','expired')),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK((kind='link' AND token_digest IS NOT NULL AND length(token_digest)=64 AND invitee_user_id IS NULL) OR
 (kind='email' AND token_digest IS NULL AND invitee_user_id IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS team_invites_space_idx ON drama_catalog_v1.team_invites(space_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS team_invites_recipient_idx ON drama_catalog_v1.team_invites(invitee_user_id,created_at DESC,id DESC) WHERE status='pending' AND revoked_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS team_pending_email_invite_uidx ON drama_catalog_v1.team_invites(space_id,invitee_user_id) WHERE kind='email' AND status='pending' AND revoked_at IS NULL;
CREATE TABLE IF NOT EXISTS drama_catalog_v1.team_join_requests (
 id text PRIMARY KEY, space_id text NOT NULL REFERENCES drama_catalog_v1.teams(space_id),
 user_id text NOT NULL REFERENCES drama_catalog_v1.users(id), message text NOT NULL DEFAULT '' CHECK(char_length(message)<=200),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected')),
 decided_by text REFERENCES drama_catalog_v1.users(id), decided_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
 CHECK((status='pending')=(decided_at IS NULL)), CHECK((decided_at IS NULL)=(decided_by IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS team_pending_request_uidx ON drama_catalog_v1.team_join_requests(space_id,user_id) WHERE status='pending';
CREATE INDEX IF NOT EXISTS team_requests_space_idx ON drama_catalog_v1.team_join_requests(space_id,created_at DESC,id DESC);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.team_ownership_transfers (
 id text PRIMARY KEY, space_id text NOT NULL REFERENCES drama_catalog_v1.teams(space_id),
 from_user_id text NOT NULL REFERENCES drama_catalog_v1.users(id), to_user_id text NOT NULL REFERENCES drama_catalog_v1.users(id),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','accepted','cancelled')),
 created_at timestamptz NOT NULL DEFAULT now(), decided_at timestamptz,
 CHECK(from_user_id<>to_user_id), CHECK((status='pending')=(decided_at IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS team_pending_ownership_uidx ON drama_catalog_v1.team_ownership_transfers(space_id) WHERE status='pending';
-- Sharing is a container fact, never a permission on an uploaded file or output.
CREATE TABLE IF NOT EXISTS drama_catalog_v1.space_shares (
 space_id text NOT NULL REFERENCES drama_catalog_v1.teams(space_id),
 resource_kind text NOT NULL CHECK(resource_kind IN ('project','canvas')),
 resource_id text NOT NULL CHECK(resource_id<>''), grantee text NOT NULL CHECK(grantee<>''),
 permission smallint NOT NULL CHECK(permission IN (1,2)),
 created_by text NOT NULL REFERENCES drama_catalog_v1.users(id),
 PRIMARY KEY(space_id,resource_kind,resource_id,grantee)
);
CREATE INDEX IF NOT EXISTS shares_grantee_idx ON drama_catalog_v1.space_shares(space_id,grantee,resource_kind,resource_id);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.team_audit_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, space_id text NOT NULL REFERENCES drama_catalog_v1.teams(space_id),
 actor_id text NOT NULL, actor_kind text NOT NULL CHECK(actor_kind IN ('user','operator')),
 action text NOT NULL CHECK(action<>''), target text NOT NULL DEFAULT '', detail jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS team_audit_space_idx ON drama_catalog_v1.team_audit_events(space_id,id DESC);
-- 操作记录合并申请/邀请与其结果时按关联 ID 查找结果行。
CREATE INDEX IF NOT EXISTS team_audit_request_result_idx ON drama_catalog_v1.team_audit_events(space_id,(detail->>'requestId')) WHERE action IN ('join_request_approved','join_request_rejected');
CREATE INDEX IF NOT EXISTS team_audit_invite_result_idx ON drama_catalog_v1.team_audit_events(space_id,(detail->>'inviteId')) WHERE action IN ('member_joined','invite_declined');
CREATE TABLE IF NOT EXISTS drama_catalog_v1.team_reminders (
 space_id text NOT NULL REFERENCES drama_catalog_v1.teams(space_id), kind text NOT NULL CHECK(kind IN ('expires_7d','expires_1d','expires_today','read_only')),
 period_key text NOT NULL, PRIMARY KEY(space_id,kind,period_key)
);
-- Runtime sessions move once to Redis. Existing credentials expire on this upgrade.
-- User identity, password hashes and original security events remain intact.
ALTER TABLE drama_catalog_v1.users DROP COLUMN IF EXISTS session_hash,
 DROP COLUMN IF EXISTS session_expires_at, DROP COLUMN IF EXISTS operations_session_hash,
 DROP COLUMN IF EXISTS operations_session_expires_at;
-- Diagnostic events are not credentials or an authentication source.
CREATE TABLE IF NOT EXISTS drama_catalog_v1.session_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id text NOT NULL REFERENCES drama_catalog_v1.users(id),
 audience text NOT NULL CHECK(audience IN ('workspace','operations')),
 kind text NOT NULL CHECK(kind IN ('login','replaced','logout','password_changed','password_reset','disabled')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS session_events_user_idx ON drama_catalog_v1.session_events(user_id,id DESC);
-- Account-owned, re-encoded avatar only; business uploads remain separate.
ALTER TABLE drama_catalog_v1.users ADD COLUMN IF NOT EXISTS avatar_data bytea;
ALTER TABLE drama_catalog_v1.users ADD COLUMN IF NOT EXISTS avatar_digest text;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='drama_catalog_v1.users'::regclass AND conname='users_avatar_valid') THEN
  ALTER TABLE drama_catalog_v1.users ADD CONSTRAINT users_avatar_valid CHECK (
   (avatar_data IS NULL AND avatar_digest IS NULL) OR
   (avatar_data IS NOT NULL AND avatar_digest IS NOT NULL AND octet_length(avatar_data) BETWEEN 1 AND 524288 AND length(avatar_digest)=64));
 END IF;
END $$;
CREATE TABLE IF NOT EXISTS drama_catalog_v1.registration_settings (
 id boolean PRIMARY KEY DEFAULT true CHECK(id), requires_code boolean NOT NULL DEFAULT true
);
INSERT INTO drama_catalog_v1.registration_settings(id) VALUES(true) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS drama_catalog_v1.authorization_codes (
 id text PRIMARY KEY, digest text UNIQUE NOT NULL, expires_at timestamptz NOT NULL,
 disabled boolean NOT NULL DEFAULT false, used_by text REFERENCES drama_catalog_v1.users(id),
 used_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), CHECK((used_by IS NULL)=(used_at IS NULL))
);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.email_verifications (
 id text PRIMARY KEY, email text NOT NULL, purpose text NOT NULL CHECK(purpose IN ('register','login','reset')),
 digest text NOT NULL, expires_at timestamptz NOT NULL, active boolean NOT NULL DEFAULT false,
 attempts integer NOT NULL DEFAULT 0, consumed boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS email_verifications_email_idx ON drama_catalog_v1.email_verifications(email,purpose,created_at DESC);
-- Counters are transient Redis state, never restored from an obsolete table.
DROP TABLE IF EXISTS drama_catalog_v1.auth_throttles;
CREATE TABLE IF NOT EXISTS drama_catalog_v1.credit_accounts (
 account_id text PRIMARY KEY, balance bigint NOT NULL DEFAULT 0 CHECK(balance>=0 AND balance<=9007199254740991)
);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.credit_allocations (
 account_id text NOT NULL REFERENCES drama_catalog_v1.credit_accounts(account_id),
 holder_id text NOT NULL CHECK(holder_id<>''), balance bigint NOT NULL DEFAULT 0 CHECK(balance BETWEEN 0 AND 9007199254740991),
 open boolean NOT NULL DEFAULT true, PRIMARY KEY(account_id,holder_id), CHECK(open OR balance=0)
);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.credit_allocation_events (
 id text PRIMARY KEY, operation_id text UNIQUE NOT NULL, account_id text NOT NULL REFERENCES drama_catalog_v1.credit_accounts(account_id),
 holder_id text NOT NULL CHECK(holder_id<>''), kind text NOT NULL CHECK(kind IN ('allocate','reclaim','release')),
 amount bigint NOT NULL CHECK(amount BETWEEN 0 AND 9007199254740991), balance bigint NOT NULL CHECK(balance BETWEEN 0 AND 9007199254740991),
 actor_id text NOT NULL, requested_amount bigint NOT NULL CHECK(requested_amount BETWEEN 0 AND 9007199254740991), requested_all boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS credit_allocation_holder_idx ON drama_catalog_v1.credit_allocation_events(account_id,holder_id,created_at DESC,id DESC);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.credit_ledger (
 id text PRIMARY KEY, operation_id text UNIQUE NOT NULL, account_id text NOT NULL REFERENCES drama_catalog_v1.credit_accounts(account_id),
 holder_id text CHECK(holder_id IS NULL OR holder_id<>''),
 kind text NOT NULL CHECK(kind IN ('add','deduct','refund')), amount bigint NOT NULL CHECK(amount>0 AND amount<=9007199254740991),
 balance bigint NOT NULL CHECK(balance>=0 AND balance<=9007199254740991), source text NOT NULL, subject text NOT NULL, reason text NOT NULL,
 original_deduction_id text REFERENCES drama_catalog_v1.credit_ledger(id), created_at timestamptz NOT NULL DEFAULT now(),
 CHECK((kind='refund')=(original_deduction_id IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS credit_ledger_account_idx ON drama_catalog_v1.credit_ledger(account_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS credit_ledger_original_idx ON drama_catalog_v1.credit_ledger(original_deduction_id) WHERE original_deduction_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS tasks_space_owner_command_uidx ON drama_catalog_v1.tasks(space_id,owner_id,scope_id,command_id);
CREATE UNIQUE INDEX IF NOT EXISTS tasks_space_active_uidx ON drama_catalog_v1.tasks(space_id,scope_id,(payload->>'activeKey')) WHERE status IN ('queued','running','unknown') AND coalesce(payload->>'activeKey','')<>'';

CREATE TABLE IF NOT EXISTS drama_catalog_v1.mail_configuration (
 id boolean PRIMARY KEY DEFAULT true CHECK(id), revision bigint NOT NULL DEFAULT 0 CHECK(revision BETWEEN 0 AND 9007199254740991),
 enabled boolean NOT NULL DEFAULT false, address text NOT NULL DEFAULT '', username text NOT NULL DEFAULT '',
 from_address text NOT NULL DEFAULT '', tls text NOT NULL DEFAULT 'starttls' CHECK(tls IN ('starttls','tls')), password_cipher bytea,
 CHECK(NOT enabled OR (address<>'' AND username<>'' AND from_address<>'' AND password_cipher IS NOT NULL))
);
INSERT INTO drama_catalog_v1.mail_configuration(id) VALUES(true) ON CONFLICT DO NOTHING;

-- Append after the existing credit tables. One-time metadata extension only:
-- recorded old balances remain unclassified rather than fabricating a source.
ALTER TABLE drama_catalog_v1.credit_accounts ADD COLUMN IF NOT EXISTS recharge_balance bigint NOT NULL DEFAULT 0;
ALTER TABLE drama_catalog_v1.credit_accounts ADD COLUMN IF NOT EXISTS gift_balance bigint NOT NULL DEFAULT 0;
ALTER TABLE drama_catalog_v1.credit_accounts ADD COLUMN IF NOT EXISTS legacy_balance bigint;
UPDATE drama_catalog_v1.credit_accounts SET legacy_balance=balance WHERE legacy_balance IS NULL;
ALTER TABLE drama_catalog_v1.credit_accounts ALTER COLUMN legacy_balance SET DEFAULT 0;
ALTER TABLE drama_catalog_v1.credit_accounts ALTER COLUMN legacy_balance SET NOT NULL;
ALTER TABLE drama_catalog_v1.credit_ledger ADD COLUMN IF NOT EXISTS category text NOT NULL DEFAULT 'legacy';
ALTER TABLE drama_catalog_v1.credit_ledger ADD COLUMN IF NOT EXISTS recharge_amount bigint NOT NULL DEFAULT 0;
ALTER TABLE drama_catalog_v1.credit_ledger ADD COLUMN IF NOT EXISTS gift_amount bigint NOT NULL DEFAULT 0;
ALTER TABLE drama_catalog_v1.credit_ledger ADD COLUMN IF NOT EXISTS legacy_amount bigint;
UPDATE drama_catalog_v1.credit_ledger SET legacy_amount=amount WHERE legacy_amount IS NULL;
ALTER TABLE drama_catalog_v1.credit_ledger ALTER COLUMN legacy_amount SET DEFAULT 0;
ALTER TABLE drama_catalog_v1.credit_ledger ALTER COLUMN legacy_amount SET NOT NULL;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='drama_catalog_v1.credit_accounts'::regclass AND conname='credit_accounts_origins_check') THEN
  ALTER TABLE drama_catalog_v1.credit_accounts ADD CONSTRAINT credit_accounts_origins_check CHECK(recharge_balance>=0 AND gift_balance>=0 AND legacy_balance>=0 AND balance=recharge_balance+gift_balance+legacy_balance);
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='drama_catalog_v1.credit_ledger'::regclass AND conname='credit_ledger_origins_check') THEN
  ALTER TABLE drama_catalog_v1.credit_ledger ADD CONSTRAINT credit_ledger_origins_check CHECK(recharge_amount>=0 AND gift_amount>=0 AND legacy_amount>=0 AND amount=recharge_amount+gift_amount+legacy_amount AND CASE category WHEN 'recharge' THEN recharge_amount=amount WHEN 'gift' THEN gift_amount=amount WHEN 'legacy' THEN legacy_amount=amount WHEN 'mixed' THEN recharge_amount<amount AND gift_amount<amount AND legacy_amount<amount ELSE false END);
 END IF;
END $$;


-- Additive migration: shared authentication users; message text exists once.
CREATE TABLE IF NOT EXISTS drama_catalog_v1.notification_messages (
 id text PRIMARY KEY,
 author_id text REFERENCES drama_catalog_v1.users(id),
 kind text NOT NULL DEFAULT 'operator' CHECK(kind IN ('operator','system')), link jsonb,
 command_id text NOT NULL,
 creation_digest text NOT NULL,
 title text NOT NULL,
 body text NOT NULL,
 audience text NOT NULL CHECK(audience IN ('all','user')),
 recipient_user_id text REFERENCES drama_catalog_v1.users(id),
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published','withdrawn')),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0 AND revision<=9007199254740991),
 recipient_count bigint NOT NULL DEFAULT 0 CHECK(recipient_count>=0),
 created_at timestamptz NOT NULL DEFAULT now(),
 published_at timestamptz, withdrawn_at timestamptz,
 UNIQUE(author_id,command_id),
 CHECK((kind='operator' AND author_id IS NOT NULL AND link IS NULL) OR
 (kind='system' AND author_id IS NULL AND status='published' AND audience='user' AND recipient_user_id IS NOT NULL AND recipient_count=1 AND jsonb_typeof(link)='object' AND coalesce(link->>'type','')<>'')),
 CHECK((audience='all' AND recipient_user_id IS NULL) OR (audience='user' AND recipient_user_id IS NOT NULL)),
 CHECK((status='draft' AND published_at IS NULL AND withdrawn_at IS NULL) OR (status='published' AND published_at IS NOT NULL AND withdrawn_at IS NULL) OR (status='withdrawn' AND published_at IS NOT NULL AND withdrawn_at IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS notification_system_command_uidx ON drama_catalog_v1.notification_messages(command_id,recipient_user_id) WHERE kind='system';
CREATE INDEX IF NOT EXISTS notification_author_page ON drama_catalog_v1.notification_messages(author_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS notification_published_page ON drama_catalog_v1.notification_messages(published_at DESC,id DESC) WHERE status='published';
CREATE TABLE IF NOT EXISTS drama_catalog_v1.notification_receipts (
 message_id text NOT NULL REFERENCES drama_catalog_v1.notification_messages(id),
 user_id text NOT NULL REFERENCES drama_catalog_v1.users(id),
 read_at timestamptz,
 PRIMARY KEY(message_id,user_id)
);
CREATE INDEX IF NOT EXISTS notification_recipient_page ON drama_catalog_v1.notification_receipts(user_id,message_id);
CREATE INDEX IF NOT EXISTS notification_recipient_unread ON drama_catalog_v1.notification_receipts(user_id) WHERE read_at IS NULL;

CREATE TABLE IF NOT EXISTS drama_catalog_v1.notification_images (
 id text PRIMARY KEY,
 message_id text NOT NULL REFERENCES drama_catalog_v1.notification_messages(id),
 content_type text NOT NULL CHECK(content_type IN ('image/png','image/jpeg')),
 width integer NOT NULL CHECK(width BETWEEN 1 AND 4096),
 height integer NOT NULL CHECK(height BETWEEN 1 AND 4096),
 data bytea NOT NULL CHECK(octet_length(data) BETWEEN 1 AND 5242880),
 sha256 text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(message_id,sha256)
);
CREATE OR REPLACE FUNCTION drama_catalog_v1.notification_receipt_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.read_at IS NOT NULL AND (TG_OP='INSERT' OR OLD.read_at IS DISTINCT FROM NEW.read_at) THEN
  PERFORM drama_catalog_v1.enqueue_runtime('notifications:'||NEW.user_id);
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER notification_receipt_changed AFTER INSERT OR UPDATE OF read_at ON drama_catalog_v1.notification_receipts
 FOR EACH ROW EXECUTE FUNCTION drama_catalog_v1.notification_receipt_changed();
CREATE OR REPLACE FUNCTION drama_catalog_v1.notification_message_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.author_id IS NOT NULL THEN
  PERFORM drama_catalog_v1.enqueue_runtime('notifications:'||NEW.author_id);
 END IF;
 IF TG_OP='INSERT' AND NEW.kind='system' AND NEW.status='published' THEN
  PERFORM drama_catalog_v1.enqueue_runtime('notifications:'||NEW.recipient_user_id);
 ELSIF TG_OP='UPDATE' THEN
  IF NEW.audience='all' AND OLD.status IS DISTINCT FROM NEW.status AND NEW.status IN ('published','withdrawn') THEN
   PERFORM drama_catalog_v1.enqueue_runtime('notifications:*');
  ELSIF OLD.status IS DISTINCT FROM NEW.status AND NEW.status IN ('published','withdrawn') THEN
   PERFORM drama_catalog_v1.enqueue_runtime('notifications:'||NEW.recipient_user_id);
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER notification_message_changed AFTER INSERT OR UPDATE ON drama_catalog_v1.notification_messages
 FOR EACH ROW EXECUTE FUNCTION drama_catalog_v1.notification_message_changed();


-- Operational gift configuration and one eligibility fact per registration/day.
CREATE TABLE IF NOT EXISTS drama_catalog_v1.credit_gift_policy (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 revision bigint NOT NULL DEFAULT 0 CHECK(revision BETWEEN 0 AND 9007199254740991),
 registration_gift bigint NOT NULL DEFAULT 0 CHECK(registration_gift BETWEEN 0 AND 9007199254740991),
 daily_gift bigint NOT NULL DEFAULT 0 CHECK(daily_gift BETWEEN 0 AND 9007199254740991)
);
INSERT INTO drama_catalog_v1.credit_gift_policy(id) VALUES(true) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS drama_catalog_v1.credit_gift_claims (
 user_id text NOT NULL REFERENCES drama_catalog_v1.users(id),
 kind text NOT NULL CHECK(kind IN ('registration','daily')),
 period text NOT NULL,
 amount bigint NOT NULL CHECK(amount BETWEEN 0 AND 9007199254740991),
 policy_revision bigint NOT NULL CHECK(policy_revision BETWEEN 0 AND 9007199254740991),
 operation_id text UNIQUE REFERENCES drama_catalog_v1.credit_ledger(operation_id),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,kind,period),
 CHECK((amount=0)=(operation_id IS NULL)),
 CHECK((kind='registration' AND period='once') OR (kind='daily' AND period ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'))
);

-- Transactional invalidation work is visible only after COMMIT.
CREATE OR REPLACE FUNCTION drama_catalog_v1.credit_ledger_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM drama_catalog_v1.enqueue_runtime('credits:'||NEW.account_id);
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER credit_ledger_changed AFTER INSERT ON drama_catalog_v1.credit_ledger
 FOR EACH ROW EXECUTE FUNCTION drama_catalog_v1.credit_ledger_changed();
CREATE OR REPLACE TRIGGER credit_allocation_changed AFTER INSERT ON drama_catalog_v1.credit_allocation_events
 FOR EACH ROW EXECUTE FUNCTION drama_catalog_v1.credit_ledger_changed();

-- Completion settles an existing pre-deduction without another money entry.
CREATE OR REPLACE FUNCTION drama_catalog_v1.credit_settlement_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE account text;
BEGIN
 IF TG_TABLE_NAME='tasks' THEN
  account := NEW.input_snapshot#>>'{callerContext,credit,accountId}';
  IF account IS NULL OR NOT EXISTS (
   SELECT 1 FROM drama_catalog_v1.credit_ledger WHERE operation_id='generation:'||(NEW.input_snapshot#>>'{callerContext,credit,id}')||':deduct' AND account_id=account AND kind='deduct'
  ) THEN RETURN NEW; END IF;
  PERFORM drama_catalog_v1.enqueue_runtime('credits:'||account);
 ELSE
  FOR account IN SELECT DISTINCT r#>>'{credit,accountId}' FROM jsonb_array_elements(coalesce(nullif(NEW.payload->'runs','null'::jsonb),'[]'::jsonb)) r WHERE r#>>'{credit,accountId}' IS NOT NULL
  LOOP PERFORM drama_catalog_v1.enqueue_runtime('credits:'||account); END LOOP;
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER credit_project_settlement_changed AFTER UPDATE OF payload ON drama_catalog_v1.projects
 FOR EACH ROW WHEN (jsonb_path_query_array(OLD.payload,'$.runs[*] ? (@.credit.amount > 0).status') IS DISTINCT FROM jsonb_path_query_array(NEW.payload,'$.runs[*] ? (@.credit.amount > 0).status')) EXECUTE FUNCTION drama_catalog_v1.credit_settlement_changed();
CREATE OR REPLACE TRIGGER credit_task_settlement_changed AFTER UPDATE OF status ON drama_catalog_v1.tasks
 FOR EACH ROW WHEN (OLD.status IS DISTINCT FROM NEW.status) EXECUTE FUNCTION drama_catalog_v1.credit_settlement_changed();

-- Daily gifts are a subset of gift funds. Existing unallocated gifts retain
-- their original permanent lifetime; no historical source is guessed.
ALTER TABLE drama_catalog_v1.credit_accounts ADD COLUMN IF NOT EXISTS daily_balance bigint NOT NULL DEFAULT 0;
ALTER TABLE drama_catalog_v1.credit_accounts ADD COLUMN IF NOT EXISTS daily_expires_at timestamptz;
ALTER TABLE drama_catalog_v1.credit_ledger ADD COLUMN IF NOT EXISTS daily_amount bigint NOT NULL DEFAULT 0;
ALTER TABLE drama_catalog_v1.credit_ledger ADD COLUMN IF NOT EXISTS daily_expires_at timestamptz;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='drama_catalog_v1.credit_accounts'::regclass AND conname='credit_accounts_daily_check') THEN
  ALTER TABLE drama_catalog_v1.credit_accounts ADD CONSTRAINT credit_accounts_daily_check CHECK(daily_balance>=0 AND daily_balance<=gift_balance AND ((daily_balance=0)=(daily_expires_at IS NULL)));
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='drama_catalog_v1.credit_ledger'::regclass AND conname='credit_ledger_daily_check') THEN
  ALTER TABLE drama_catalog_v1.credit_ledger ADD CONSTRAINT credit_ledger_daily_check CHECK(daily_amount>=0 AND daily_amount<=gift_amount AND ((daily_amount=0)=(daily_expires_at IS NULL)) AND (kind<>'expire' OR (daily_amount=amount AND original_deduction_id IS NULL)));
 END IF;
END $$;
ALTER TABLE drama_catalog_v1.credit_ledger DROP CONSTRAINT IF EXISTS credit_ledger_kind_check;
ALTER TABLE drama_catalog_v1.credit_ledger ADD CONSTRAINT credit_ledger_kind_check CHECK(kind IN ('add','deduct','refund','expire','reclaim'));
CREATE INDEX IF NOT EXISTS credit_accounts_daily_due_idx ON drama_catalog_v1.credit_accounts(daily_expires_at,account_id) WHERE daily_balance>0;

-- Durable changes produce minimal work for Redis projections and cross-node hints.
CREATE OR REPLACE FUNCTION drama_catalog_v1.runtime_entity_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM drama_catalog_v1.enqueue_runtime(TG_ARGV[0]||':'||NEW.id);
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER runtime_task_changed AFTER INSERT OR UPDATE ON drama_catalog_v1.tasks
 FOR EACH ROW EXECUTE FUNCTION drama_catalog_v1.runtime_entity_changed('task');
CREATE OR REPLACE TRIGGER runtime_project_changed AFTER INSERT OR UPDATE ON drama_catalog_v1.projects
 FOR EACH ROW EXECUTE FUNCTION drama_catalog_v1.runtime_entity_changed('project');
CREATE OR REPLACE FUNCTION drama_catalog_v1.runtime_config_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM drama_catalog_v1.enqueue_runtime(TG_ARGV[0]);
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER runtime_model_changed AFTER UPDATE ON drama_catalog_v1.model_config
 FOR EACH ROW EXECUTE FUNCTION drama_catalog_v1.runtime_config_changed('models');
CREATE OR REPLACE TRIGGER runtime_platform_changed AFTER UPDATE ON drama_catalog_v1.platform_settings
 FOR EACH ROW EXECUTE FUNCTION drama_catalog_v1.runtime_config_changed('platform');
-- 团队成员、邀请与加入申请变化只发团队范围失效提示，页面收到后重读权威 GET。
CREATE OR REPLACE FUNCTION drama_catalog_v1.team_runtime_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  PERFORM drama_catalog_v1.enqueue_runtime('team:'||OLD.space_id);
 ELSE
  PERFORM drama_catalog_v1.enqueue_runtime('team:'||NEW.space_id);
 END IF;
 RETURN NULL;
END $$;
CREATE OR REPLACE TRIGGER runtime_team_members_changed AFTER INSERT OR UPDATE OR DELETE ON drama_catalog_v1.team_members
 FOR EACH ROW EXECUTE FUNCTION drama_catalog_v1.team_runtime_changed();
CREATE OR REPLACE TRIGGER runtime_team_invites_changed AFTER INSERT OR UPDATE ON drama_catalog_v1.team_invites
 FOR EACH ROW EXECUTE FUNCTION drama_catalog_v1.team_runtime_changed();
CREATE OR REPLACE TRIGGER runtime_team_join_requests_changed AFTER INSERT OR UPDATE ON drama_catalog_v1.team_join_requests
 FOR EACH ROW EXECUTE FUNCTION drama_catalog_v1.team_runtime_changed();

-- Cross-project account library, using original successful media facts.
CREATE INDEX IF NOT EXISTS tasks_media_library_idx ON drama_catalog_v1.tasks(space_id,created_at DESC,id DESC) WHERE status='succeeded' AND payload->>'operation' IN ('image.generate','video.generate');

-- 支付退款扣回保留原发放关联；旧流水不改写。
ALTER TABLE drama_catalog_v1.credit_ledger ADD COLUMN IF NOT EXISTS original_addition_id text REFERENCES drama_catalog_v1.credit_ledger(id);
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='drama_catalog_v1.credit_ledger'::regclass AND conname='credit_ledger_addition_check') THEN
  ALTER TABLE drama_catalog_v1.credit_ledger ADD CONSTRAINT credit_ledger_addition_check CHECK((kind='reclaim')=(original_addition_id IS NOT NULL) AND (kind<>'reclaim' OR (category IN ('recharge','gift') AND daily_amount=0)));
 END IF;
END $$;
CREATE INDEX IF NOT EXISTS credit_ledger_addition_idx ON drama_catalog_v1.credit_ledger(original_addition_id) WHERE original_addition_id IS NOT NULL;
CREATE OR REPLACE FUNCTION drama_catalog_v1.credit_reclaim_original_check() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind='reclaim' AND NOT EXISTS (
  SELECT 1 FROM drama_catalog_v1.credit_ledger WHERE id=NEW.original_addition_id AND kind='add' AND account_id=NEW.account_id AND category=NEW.category AND daily_amount=0
 ) THEN
  RAISE EXCEPTION 'reclaim requires an original permanent addition in the same account and category' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER credit_reclaim_original_check BEFORE INSERT OR UPDATE ON drama_catalog_v1.credit_ledger
 FOR EACH ROW EXECUTE FUNCTION drama_catalog_v1.credit_reclaim_original_check();

-- 支付订单仅保存规范化交易事实，不保存渠道原始报文或密钥。
CREATE TABLE IF NOT EXISTS drama_catalog_v1.payment_packages (
 id text PRIMARY KEY, revision bigint NOT NULL CHECK(revision BETWEEN 1 AND 9007199254740991),
 amount_fen bigint NOT NULL CHECK(amount_fen BETWEEN 1 AND 9007199254740991),
 recharge_credits bigint NOT NULL CHECK(recharge_credits BETWEEN 1 AND 9007199254740991),
 gift_credits bigint NOT NULL CHECK(gift_credits>=0 AND gift_credits<=9007199254740991-recharge_credits),
 enabled boolean NOT NULL, sort integer NOT NULL DEFAULT 0, test_only boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
-- 旧档位缺少名称时保留未命名；结构迁移不冒充运营保存或修改版本/业务时间。
ALTER TABLE drama_catalog_v1.payment_packages ADD COLUMN IF NOT EXISTS name text NOT NULL DEFAULT '';
ALTER TABLE drama_catalog_v1.payment_packages ADD COLUMN IF NOT EXISTS original_amount_fen bigint;
UPDATE drama_catalog_v1.payment_packages SET original_amount_fen=amount_fen WHERE original_amount_fen IS NULL;
ALTER TABLE drama_catalog_v1.payment_packages ALTER COLUMN original_amount_fen SET NOT NULL;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='drama_catalog_v1.payment_packages'::regclass AND conname='payment_packages_original_amount_check') THEN
  ALTER TABLE drama_catalog_v1.payment_packages ADD CONSTRAINT payment_packages_original_amount_check CHECK(original_amount_fen BETWEEN amount_fen AND 9007199254740991);
 END IF;
END $$;
CREATE TABLE IF NOT EXISTS drama_catalog_v1.payment_settings (
 id boolean PRIMARY KEY DEFAULT true CHECK(id), revision bigint NOT NULL DEFAULT 0 CHECK(revision BETWEEN 0 AND 9007199254740991),
 daily_limit_fen bigint NOT NULL DEFAULT 500000 CHECK(daily_limit_fen BETWEEN 0 AND 9007199254740991)
);
INSERT INTO drama_catalog_v1.payment_settings(id) VALUES(true) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS drama_catalog_v1.payment_orders (
 order_no text PRIMARY KEY, buyer_user_id text NOT NULL REFERENCES drama_catalog_v1.users(id), client_request_id text NOT NULL,
 target_kind text NOT NULL CHECK(target_kind IN ('personal','team')), target_id text NOT NULL, target_name text NOT NULL,
 credit_account_id text NOT NULL, package_id text NOT NULL REFERENCES drama_catalog_v1.payment_packages(id),
 amount_fen bigint NOT NULL CHECK(amount_fen BETWEEN 1 AND 9007199254740991),
 recharge_credits bigint NOT NULL CHECK(recharge_credits BETWEEN 1 AND 9007199254740991),
 gift_credits bigint NOT NULL CHECK(gift_credits>=0 AND gift_credits<=9007199254740991-recharge_credits),
 channel text NOT NULL CHECK(channel IN ('demo','wechatpay','alipay')), code_url text NOT NULL DEFAULT '', channel_trade_no text NOT NULL DEFAULT '',
 status text NOT NULL CHECK(status IN ('pending','credited','closed','refunded')), close_reason text NOT NULL DEFAULT '', agreement_version text NOT NULL,
 expires_at timestamptz NOT NULL, next_query_at timestamptz NOT NULL, query_attempts integer NOT NULL DEFAULT 0 CHECK(query_attempts>=0),
 created_at timestamptz NOT NULL DEFAULT now(), credited_at timestamptz, channel_paid_at timestamptz, closed_at timestamptz, refunded_at timestamptz,
 UNIQUE(buyer_user_id,client_request_id),
 CHECK((status IN ('credited','refunded'))=(credited_at IS NOT NULL)),
 CHECK((status='closed')=(closed_at IS NOT NULL)), CHECK((status='refunded')=(refunded_at IS NOT NULL))
);
-- 历史原价只按订单自身实付回填，不与后来改价的档位联表。
ALTER TABLE drama_catalog_v1.payment_orders ADD COLUMN IF NOT EXISTS original_amount_fen bigint;
UPDATE drama_catalog_v1.payment_orders SET original_amount_fen=amount_fen WHERE original_amount_fen IS NULL;
ALTER TABLE drama_catalog_v1.payment_orders ALTER COLUMN original_amount_fen SET NOT NULL;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='drama_catalog_v1.payment_orders'::regclass AND conname='payment_orders_original_amount_check') THEN
  ALTER TABLE drama_catalog_v1.payment_orders ADD CONSTRAINT payment_orders_original_amount_check CHECK(original_amount_fen BETWEEN amount_fen AND 9007199254740991);
 END IF;
END $$;
ALTER TABLE drama_catalog_v1.payment_orders ADD COLUMN IF NOT EXISTS channel_paid_at timestamptz;
CREATE UNIQUE INDEX IF NOT EXISTS payment_orders_buyer_pending_idx ON drama_catalog_v1.payment_orders(buyer_user_id) WHERE status='pending';
CREATE INDEX IF NOT EXISTS payment_orders_due_idx ON drama_catalog_v1.payment_orders(next_query_at,order_no) WHERE status='pending';
CREATE INDEX IF NOT EXISTS payment_orders_buyer_date_idx ON drama_catalog_v1.payment_orders(buyer_user_id,created_at DESC,order_no);
CREATE INDEX IF NOT EXISTS payment_orders_paid_idx ON drama_catalog_v1.payment_orders(channel,channel_paid_at) WHERE channel_paid_at IS NOT NULL;
CREATE TABLE IF NOT EXISTS drama_catalog_v1.payment_refunds (
 id text PRIMARY KEY, refund_no text UNIQUE NOT NULL, order_no text NOT NULL REFERENCES drama_catalog_v1.payment_orders(order_no),
 kind text NOT NULL CHECK(kind IN ('operator','late_payment')), amount_fen bigint NOT NULL CHECK(amount_fen BETWEEN 1 AND 9007199254740991),
 recharge_credits bigint NOT NULL CHECK(recharge_credits BETWEEN 0 AND 9007199254740991), gift_credits bigint NOT NULL CHECK(gift_credits BETWEEN 0 AND 9007199254740991),
 status text NOT NULL CHECK(status IN ('pending','succeeded','failed')), operator_id text REFERENCES drama_catalog_v1.users(id), reason text NOT NULL,
 channel_refund_id text NOT NULL DEFAULT '', attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0), next_query_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), finished_at timestamptz, channel_bill_at timestamptz,
 CHECK((status='pending')=(finished_at IS NULL)), CHECK((kind='operator')=(operator_id IS NOT NULL)),
 CHECK(kind<>'late_payment' OR (recharge_credits=0 AND gift_credits=0))
);
ALTER TABLE drama_catalog_v1.payment_refunds ADD COLUMN IF NOT EXISTS channel_bill_at timestamptz;
CREATE UNIQUE INDEX IF NOT EXISTS payment_refunds_operator_idx ON drama_catalog_v1.payment_refunds(order_no) WHERE kind='operator';
CREATE UNIQUE INDEX IF NOT EXISTS payment_refunds_late_idx ON drama_catalog_v1.payment_refunds(order_no) WHERE kind='late_payment';
CREATE INDEX IF NOT EXISTS payment_refunds_due_idx ON drama_catalog_v1.payment_refunds(next_query_at,id) WHERE status='pending';
CREATE TABLE IF NOT EXISTS drama_catalog_v1.payment_events (
 id text PRIMARY KEY, order_no text REFERENCES drama_catalog_v1.payment_orders(order_no), refund_id text REFERENCES drama_catalog_v1.payment_refunds(id),
 source text NOT NULL, outcome text NOT NULL, facts jsonb NOT NULL DEFAULT '{}', operator_id text REFERENCES drama_catalog_v1.users(id), reason text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS payment_events_order_idx ON drama_catalog_v1.payment_events(order_no,created_at,id);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.payment_anomalies (
 id text PRIMARY KEY, order_no text REFERENCES drama_catalog_v1.payment_orders(order_no), channel text NOT NULL,
 kind text NOT NULL, detail jsonb NOT NULL DEFAULT '{}', status text NOT NULL DEFAULT 'open' CHECK(status IN ('open','resolved')),
 resolved_by text REFERENCES drama_catalog_v1.users(id), note text NOT NULL DEFAULT '', resolved_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
 CHECK((status='resolved')=(resolved_at IS NOT NULL AND resolved_by IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS payment_anomalies_status_idx ON drama_catalog_v1.payment_anomalies(status,created_at DESC,id);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.payment_agreements (
 user_id text NOT NULL REFERENCES drama_catalog_v1.users(id), version text NOT NULL, agreed_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(user_id,version)
);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.payment_bills (
 channel text NOT NULL CHECK(channel IN ('wechatpay','alipay')), bill_date date NOT NULL,
 status text NOT NULL CHECK(status IN ('pending','compared','failed')), attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 24),
 next_attempt_at timestamptz NOT NULL, compared_at timestamptz, summary jsonb NOT NULL DEFAULT '{}', PRIMARY KEY(channel,bill_date)
);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.payment_demo_trades (
 order_no text PRIMARY KEY, state text NOT NULL CHECK(state IN ('notpay','paid','failed','closed')),
 amount_fen bigint NOT NULL CHECK(amount_fen BETWEEN 1 AND 9007199254740991), refunded_fen bigint NOT NULL DEFAULT 0 CHECK(refunded_fen>=0 AND refunded_fen<=amount_fen),
 paid_at timestamptz, updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE drama_catalog_v1.payment_demo_trades ADD COLUMN IF NOT EXISTS paid_at timestamptz;

-- 一单最多一次演示原路退款：必须保留原号和渠道账单时刻才能核对重试。
ALTER TABLE drama_catalog_v1.payment_demo_trades ADD COLUMN IF NOT EXISTS refund_no text NOT NULL DEFAULT '';
ALTER TABLE drama_catalog_v1.payment_demo_trades ADD COLUMN IF NOT EXISTS refund_bill_at timestamptz;
CREATE UNIQUE INDEX IF NOT EXISTS payment_demo_refund_no_idx ON drama_catalog_v1.payment_demo_trades(refund_no) WHERE refund_no<>'';
