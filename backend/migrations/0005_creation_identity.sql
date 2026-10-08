-- Cutover prevents replaying old creations, including tasks with no former plan.
CREATE TABLE creation_cutover (id INTEGER PRIMARY KEY CHECK(id=1), installed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp());
INSERT INTO creation_cutover(id) VALUES(1);
CREATE TABLE creation_notices (
 project_id UUID NOT NULL, task_id UUID NOT NULL, logical_key TEXT NOT NULL,
 op_id TEXT, state TEXT NOT NULL, first_seen TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(project_id,task_id), UNIQUE(project_id,logical_key)
);
INSERT INTO creation_notices(project_id,task_id,logical_key,op_id,state)
 SELECT DISTINCT ON(project_id,task_id) project_id,task_id,'task:'||task_id::text,op_id,'historical'
 FROM jobs WHERE kind='created' ORDER BY project_id,task_id,created_at;
UPDATE plugin_metadata SET version=5 WHERE id=1;
