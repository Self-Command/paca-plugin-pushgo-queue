CREATE TABLE project_settings (
 project_id UUID PRIMARY KEY, config JSONB NOT NULL, password_enc TEXT NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1, needs_reconcile BOOLEAN NOT NULL DEFAULT TRUE,
 last_reconciled TIMESTAMPTZ, last_error TEXT NOT NULL DEFAULT '', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE task_rules (
 project_id UUID NOT NULL REFERENCES project_settings(project_id), task_id UUID NOT NULL,
 config JSONB NOT NULL, revision INTEGER NOT NULL DEFAULT 1,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY(project_id,task_id)
);
CREATE TABLE plans (
 project_id UUID NOT NULL, task_id UUID NOT NULL, fingerprint TEXT NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1, state TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(project_id,task_id)
);
CREATE TABLE jobs (
 id BIGSERIAL PRIMARY KEY, project_id UUID NOT NULL, task_id UUID NOT NULL, kind TEXT NOT NULL,
 target_at TIMESTAMPTZ NOT NULL, fire_at TIMESTAMPTZ NOT NULL, expires_at TIMESTAMPTZ NOT NULL,
 plan_revision INTEGER NOT NULL, binding_key TEXT NOT NULL, op_id TEXT NOT NULL UNIQUE,
 payload JSONB NOT NULL, state TEXT NOT NULL DEFAULT 'scheduled', attempts INTEGER NOT NULL DEFAULT 0,
 next_attempt TIMESTAMPTZ NOT NULL, lease_owner TEXT, lease_until TIMESTAMPTZ, generation INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '', gateway_result JSONB, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX jobs_due ON jobs(state,next_attempt,fire_at);
CREATE INDEX jobs_task ON jobs(project_id,task_id);
CREATE TABLE audit_log (
 id BIGSERIAL PRIMARY KEY, project_id UUID NOT NULL, actor_id TEXT NOT NULL,
 action TEXT NOT NULL, subject TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
UPDATE plugin_metadata SET version=3 WHERE id=1;
