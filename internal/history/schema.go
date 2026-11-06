package history

var migrationV1 = []string{
	`CREATE TABLE store_meta (id INTEGER PRIMARY KEY CHECK(id=1), list_generation INTEGER NOT NULL CHECK(list_generation>=0))`,
	`INSERT INTO store_meta(id,list_generation) VALUES(1,0)`,
	`CREATE TABLE runs (
 run_id TEXT PRIMARY KEY, project_id TEXT NOT NULL, workspace_id TEXT NOT NULL, document_id TEXT NOT NULL,
 graph_id TEXT NOT NULL, graph_hash TEXT NOT NULL, started_at TEXT NOT NULL, ended_at TEXT,
 storage_state TEXT NOT NULL CHECK(storage_state IN ('active','complete','interrupted')),
 runtime_state TEXT, pinned INTEGER NOT NULL CHECK(pinned IN (0,1)),
 pending_approval INTEGER NOT NULL CHECK(pending_approval IN (0,1)), revision INTEGER NOT NULL CHECK(revision>=0),
 safe_graph_json BLOB NOT NULL, graph_digest TEXT NOT NULL, safe_launch_json BLOB NOT NULL, launch_digest TEXT NOT NULL,
 completeness_json BLOB NOT NULL, completeness_digest TEXT NOT NULL,
 safe_result_json BLOB, result_digest TEXT,
 CHECK((safe_result_json IS NULL)=(result_digest IS NULL)),
 CHECK(storage_state!='complete' OR
  (ended_at IS NOT NULL AND runtime_state IS NOT NULL AND length(runtime_state)>0
   AND safe_result_json IS NOT NULL AND result_digest IS NOT NULL)))`,
	`CREATE TABLE events (run_id TEXT NOT NULL REFERENCES runs(run_id) ON DELETE CASCADE,
 sequence INTEGER NOT NULL CHECK(sequence>0), event_json BLOB NOT NULL, digest TEXT NOT NULL,
 PRIMARY KEY(run_id,sequence))`,
	`CREATE TABLE event_omissions (run_id TEXT NOT NULL REFERENCES runs(run_id) ON DELETE CASCADE,
 sequence INTEGER NOT NULL CHECK(sequence>0), reason TEXT NOT NULL CHECK(reason IN ('sensitive_identity','not_retained')),
 PRIMARY KEY(run_id,sequence))`,
	`CREATE TRIGGER event_not_omitted BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'event sequence omitted')
 WHERE EXISTS(SELECT 1 FROM event_omissions WHERE run_id=NEW.run_id AND sequence=NEW.sequence); END`,
	`CREATE TRIGGER omission_not_event BEFORE INSERT ON event_omissions BEGIN SELECT RAISE(ABORT,'event sequence stored')
 WHERE EXISTS(SELECT 1 FROM events WHERE run_id=NEW.run_id AND sequence=NEW.sequence); END`,
	`CREATE TABLE node_outcomes (run_id TEXT NOT NULL REFERENCES runs(run_id) ON DELETE CASCADE,
 node_run_id TEXT NOT NULL, node_id TEXT NOT NULL, status TEXT NOT NULL, safe_json BLOB NOT NULL, digest TEXT NOT NULL,
 PRIMARY KEY(run_id,node_run_id))`,
	`CREATE TABLE attempt_outcomes (run_id TEXT NOT NULL, attempt_id TEXT NOT NULL, node_run_id TEXT NOT NULL,
 status TEXT NOT NULL, safe_json BLOB NOT NULL, digest TEXT NOT NULL,
 PRIMARY KEY(run_id,attempt_id), FOREIGN KEY(run_id,node_run_id) REFERENCES node_outcomes(run_id,node_run_id) ON DELETE CASCADE)`,
	`CREATE TABLE managed_evidence (evidence_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(run_id) ON DELETE CASCADE,
 attempt_id TEXT NOT NULL, node_run_id TEXT, kind TEXT NOT NULL, availability TEXT NOT NULL CHECK(availability IN ('available','unavailable')),
 created_at TEXT NOT NULL, relative_path TEXT, byte_length INTEGER, digest TEXT, omission_reason TEXT,
 CHECK((availability='available' AND relative_path IS NOT NULL AND byte_length>=0 AND digest IS NOT NULL AND omission_reason IS NULL)
 OR (availability='unavailable' AND relative_path IS NULL AND byte_length IS NULL AND digest IS NULL
  AND omission_reason IN ('preimage_missing','diff_unavailable','redaction_failed','size_limit','capture_failed','unsafe_path'))))`,
	`CREATE INDEX runs_project_page ON runs(project_id,started_at DESC,run_id DESC)`,
	`CREATE INDEX runs_graph_page ON runs(graph_id,started_at DESC,run_id DESC)`,
	`CREATE INDEX runs_state_page ON runs(storage_state,started_at DESC,run_id DESC)`,
	`CREATE INDEX runs_page ON runs(started_at DESC,run_id DESC)`,
}
