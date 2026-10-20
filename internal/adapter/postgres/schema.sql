CREATE SCHEMA IF NOT EXISTS drama_catalog_v1;
-- Coalesced delivery work, not a second business ledger. Held until Redis acknowledges.
CREATE TABLE IF NOT EXISTS drama_catalog_v1.runtime_events (
 key text PRIMARY KEY, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION drama_catalog_v1.enqueue_runtime(k text) RETURNS void LANGUAGE sql AS $$
 INSERT INTO drama_catalog_v1.runtime_events(key) VALUES(k)
 ON CONFLICT(key) DO UPDATE SET created_at=EXCLUDED.created_at;
$$;
CREATE TABLE IF NOT EXISTS drama_catalog_v1.projects (
 id text PRIMARY KEY, revision bigint NOT NULL CHECK(revision>0), payload jsonb NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
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
 id text PRIMARY KEY, payload jsonb NOT NULL,
 CHECK(payload->>'id'=id)
);

-- Provider jobs are independent of projects; snapshots stay immutable during polling.
CREATE TABLE IF NOT EXISTS drama_catalog_v1.tasks (
 id text PRIMARY KEY, scope_id text NOT NULL, command_id text NOT NULL,
 status text NOT NULL CHECK(status IN ('queued','running','unknown','succeeded','failed','cancelled')),
 revision bigint NOT NULL CHECK(revision>0), next_run_at timestamptz,
 lease_token text NOT NULL DEFAULT '', lease_until timestamptz,
 input_snapshot jsonb NOT NULL, payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK((lease_token='')=(lease_until IS NULL)),
 UNIQUE(scope_id,command_id)
);
CREATE INDEX IF NOT EXISTS tasks_due_idx ON drama_catalog_v1.tasks(next_run_at,id) WHERE next_run_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS tasks_scope_idx ON drama_catalog_v1.tasks(scope_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS tasks_image_fill_idx ON drama_catalog_v1.tasks(id) WHERE status='succeeded' AND input_snapshot#>>'{callerContext,fillMissingImage}'='true';


-- Identity and independent credits. Existing content deliberately keeps NULL owner.
ALTER TABLE drama_catalog_v1.projects ADD COLUMN IF NOT EXISTS owner_id text;
ALTER TABLE drama_catalog_v1.uploaded_files ADD COLUMN IF NOT EXISTS owner_id text;
ALTER TABLE drama_catalog_v1.tasks ADD COLUMN IF NOT EXISTS owner_id text;
CREATE INDEX IF NOT EXISTS projects_owner_idx ON drama_catalog_v1.projects(owner_id,updated_at DESC);
CREATE INDEX IF NOT EXISTS uploads_owner_idx ON drama_catalog_v1.uploaded_files(owner_id,id);
CREATE INDEX IF NOT EXISTS tasks_owner_idx ON drama_catalog_v1.tasks(owner_id,scope_id,created_at DESC,id DESC);
CREATE TABLE IF NOT EXISTS drama_catalog_v1.users (
 id text PRIMARY KEY, email text UNIQUE NOT NULL, username text NOT NULL,
 role text NOT NULL CHECK(role IN ('user','operator')), enabled boolean NOT NULL DEFAULT true,
 password_hash bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
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
CREATE TABLE IF NOT EXISTS drama_catalog_v1.credit_ledger (
 id text PRIMARY KEY, operation_id text UNIQUE NOT NULL, account_id text NOT NULL REFERENCES drama_catalog_v1.credit_accounts(account_id),
 kind text NOT NULL CHECK(kind IN ('add','deduct','refund')), amount bigint NOT NULL CHECK(amount>0 AND amount<=9007199254740991),
 balance bigint NOT NULL CHECK(balance>=0 AND balance<=9007199254740991), source text NOT NULL, subject text NOT NULL, reason text NOT NULL,
 original_deduction_id text REFERENCES drama_catalog_v1.credit_ledger(id), created_at timestamptz NOT NULL DEFAULT now(),
 CHECK((kind='refund')=(original_deduction_id IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS credit_ledger_account_idx ON drama_catalog_v1.credit_ledger(account_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS credit_ledger_original_idx ON drama_catalog_v1.credit_ledger(original_deduction_id) WHERE original_deduction_id IS NOT NULL;

ALTER TABLE drama_catalog_v1.tasks DROP CONSTRAINT IF EXISTS tasks_scope_id_command_id_key;
DROP INDEX IF EXISTS drama_catalog_v1.tasks_active_uidx;
CREATE UNIQUE INDEX IF NOT EXISTS tasks_owner_command_uidx ON drama_catalog_v1.tasks(coalesce(owner_id,''),scope_id,command_id);
CREATE UNIQUE INDEX IF NOT EXISTS tasks_owner_active_uidx ON drama_catalog_v1.tasks(coalesce(owner_id,''),scope_id,(payload->>'activeKey')) WHERE status IN ('queued','running','unknown') AND coalesce(payload->>'activeKey','')<>'';

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
 author_id text NOT NULL REFERENCES drama_catalog_v1.users(id),
 command_id text NOT NULL,
 creation_digest text NOT NULL,
 title text NOT NULL,
 body text NOT NULL,
 audience text NOT NULL CHECK(audience IN ('all','user')),
 recipient_user_id text REFERENCES drama_catalog_v1.users(id),
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published')),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0 AND revision<=9007199254740991),
 recipient_count bigint NOT NULL DEFAULT 0 CHECK(recipient_count>=0),
 created_at timestamptz NOT NULL DEFAULT now(),
 published_at timestamptz,
 UNIQUE(author_id,command_id),
 CHECK((audience='all' AND recipient_user_id IS NULL) OR (audience='user' AND recipient_user_id IS NOT NULL)),
 CHECK((status='draft' AND published_at IS NULL) OR (status='published' AND published_at IS NOT NULL))
);
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

-- Preserve prior publications and receipt facts; withdrawal is a new terminal state.
ALTER TABLE drama_catalog_v1.notification_messages ADD COLUMN IF NOT EXISTS withdrawn_at timestamptz;
DO $$ DECLARE constraint_name text; BEGIN
 FOR constraint_name IN SELECT c.conname FROM pg_constraint c
 WHERE c.conrelid='drama_catalog_v1.notification_messages'::regclass AND c.contype='c'
 AND EXISTS(SELECT 1 FROM unnest(c.conkey) a(n) JOIN pg_attribute p ON p.attrelid=c.conrelid AND p.attnum=a.n WHERE p.attname='status')
 LOOP EXECUTE format('ALTER TABLE drama_catalog_v1.notification_messages DROP CONSTRAINT %I',constraint_name); END LOOP;
 ALTER TABLE drama_catalog_v1.notification_messages ADD CONSTRAINT notification_status_values CHECK(status IN ('draft','published','withdrawn'));
 ALTER TABLE drama_catalog_v1.notification_messages ADD CONSTRAINT notification_state_dates CHECK(
  (status='draft' AND published_at IS NULL AND withdrawn_at IS NULL) OR
  (status='published' AND published_at IS NOT NULL AND withdrawn_at IS NULL) OR
  (status='withdrawn' AND published_at IS NOT NULL AND withdrawn_at IS NOT NULL));
END $$;
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
 PERFORM drama_catalog_v1.enqueue_runtime('notifications:'||NEW.author_id);
 IF TG_OP='UPDATE' THEN
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

-- Completion settles an existing pre-deduction without another money entry.
CREATE OR REPLACE FUNCTION drama_catalog_v1.credit_settlement_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='tasks' THEN
  -- 媒体账务身份在受理时冻结；task ID 只用于原来按 task ID 记账的历史事实。
  IF NOT EXISTS (
   SELECT 1 FROM drama_catalog_v1.credit_ledger
   WHERE operation_id='generation:'||coalesce(NEW.input_snapshot#>>'{callerContext,credit,id}',NEW.id)||':deduct'
   AND account_id=NEW.owner_id AND kind='deduct'
  ) THEN RETURN NEW; END IF;
 END IF;
 PERFORM drama_catalog_v1.enqueue_runtime('credits:'||NEW.owner_id);
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER credit_project_settlement_changed AFTER UPDATE OF payload ON drama_catalog_v1.projects
 FOR EACH ROW WHEN (NEW.owner_id IS NOT NULL AND
 jsonb_path_query_array(OLD.payload,'$.runs[*] ? (@.credit.amount > 0).status') IS DISTINCT FROM
 jsonb_path_query_array(NEW.payload,'$.runs[*] ? (@.credit.amount > 0).status'))
 EXECUTE FUNCTION drama_catalog_v1.credit_settlement_changed();
CREATE OR REPLACE TRIGGER credit_task_settlement_changed AFTER UPDATE OF status ON drama_catalog_v1.tasks
 FOR EACH ROW WHEN (NEW.owner_id IS NOT NULL AND OLD.status IS DISTINCT FROM NEW.status)
 EXECUTE FUNCTION drama_catalog_v1.credit_settlement_changed();

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
ALTER TABLE drama_catalog_v1.credit_ledger ADD CONSTRAINT credit_ledger_kind_check CHECK(kind IN ('add','deduct','refund','expire'));
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

-- Cross-project account library, using original successful media facts.
CREATE INDEX IF NOT EXISTS tasks_media_library_idx ON drama_catalog_v1.tasks(owner_id,created_at DESC,id DESC) WHERE status='succeeded' AND payload->>'operation' IN ('image.generate','video.generate');
