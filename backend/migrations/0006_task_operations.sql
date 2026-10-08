CREATE TABLE task_operations (
 project_id UUID NOT NULL,op_id TEXT NOT NULL,actor_id TEXT NOT NULL,
 body_hash TEXT NOT NULL,body JSONB NOT NULL,auth_enc TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending',task_id UUID,result JSONB,error TEXT NOT NULL DEFAULT '',
 attempts INTEGER NOT NULL DEFAULT 0,next_attempt TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 lease_until TIMESTAMPTZ,created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(project_id,op_id)
);
CREATE INDEX task_operations_pending ON task_operations(state,next_attempt);
CREATE TABLE task_time_views (
 project_id UUID NOT NULL,task_id UUID NOT NULL,version TEXT NOT NULL,times JSONB NOT NULL,
 frozen BOOLEAN,error TEXT NOT NULL DEFAULT '',updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(project_id,task_id)
);
UPDATE plugin_metadata SET version=6 WHERE id=1;

CREATE TABLE IF NOT EXISTS task_time_requests(project_id UUID NOT NULL,task_id UUID NOT NULL,requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),PRIMARY KEY(project_id,task_id));
