CREATE TABLE IF NOT EXISTS valto_schema_versions (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE TABLE IF NOT EXISTS valto_batches (
 id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, request_key TEXT NOT NULL,
 request_hash TEXT NOT NULL, source TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','paused','canceled')),
 suppressed INTEGER NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(tenant_id, request_key)
);
CREATE TABLE IF NOT EXISTS valto_messages (
 id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, batch_id TEXT NOT NULL REFERENCES valto_batches(id),
 external_id TEXT NOT NULL DEFAULT '', dedup_key TEXT NOT NULL, recipient TEXT NOT NULL, body TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('queued','sending','accepted','rejected','unknown','canceled','expired')),
 scheduled_at TIMESTAMPTZ NOT NULL, expires_at TIMESTAMPTZ,
 error_code TEXT NOT NULL DEFAULT '', error_message TEXT NOT NULL DEFAULT '', provider_id TEXT NOT NULL DEFAULT '',
 retryable BOOLEAN NOT NULL DEFAULT FALSE, attempt_count INTEGER NOT NULL DEFAULT 0,
 current_attempt TEXT NOT NULL DEFAULT '', lease_until TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(tenant_id,dedup_key)
);
CREATE TABLE IF NOT EXISTS valto_attempts (
 id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, message_id TEXT NOT NULL REFERENCES valto_messages(id),
 status TEXT NOT NULL CHECK(status IN ('sending','accepted','rejected','unknown')),
 code TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '', provider_id TEXT NOT NULL DEFAULT '',
 started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), finished_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS valto_delivery_lanes (
 id TEXT PRIMARY KEY, next_send_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS valto_due_messages ON valto_messages(tenant_id,scheduled_at,id) WHERE status='queued';
CREATE INDEX IF NOT EXISTS valto_batch_messages ON valto_messages(tenant_id,batch_id,created_at,id);
CREATE INDEX IF NOT EXISTS valto_expired_leases ON valto_messages(lease_until) WHERE status='sending';
CREATE INDEX IF NOT EXISTS valto_message_attempts ON valto_attempts(tenant_id,message_id,started_at,id);
