# ADR-0018 — local completed-run history in SQLite

**Status:** proposed
**Date:** 2026-10-21

## Context

F2.9 must retain completed-run inspection across app and sidecar restarts,
including after the original repository disappears. The current runtime keeps a
bounded trace only in memory. It cannot resume an active graph after a crash.
F3.2 and F3.3 own an executing journal and recovery; this decision does not
implement either.

## Decision

The Go sidecar is the sole writer of a private SQLite store. Use
`modernc.org/sqlite` v1.55.0, a pure-Go runtime dependency compatible with the
repository's Go 1.25 and `golang.org/x/sys` v0.46.0 requirements. The standard
library has no SQLite engine. A separate Go 1.25 module proved local open,
insert and query on macOS arm64 and crosscompiled Windows/Linux amd64 test
binaries. Native Windows/Linux execution remains a release check.

Storage schema version starts at SQLite `user_version=1`, independently of
graph, event and sidecar protocol versions. One connection writes with foreign
keys enabled, WAL, `synchronous=FULL` and a bounded busy timeout. An unsupported
newer schema fails open with an actionable version error. Migrations execute
transactionally and may not destroy a prior readable version on failure.
Database, WAL, SHM and managed-evidence files and directories must be private
to the current user; the Windows ACL claim requires native verification. Open
the store only after acquiring and verifying the registry lifetime lock and
pinned private state-directory identity. Before SQLite opens, reject existing
`history.db`, `history.db-wal` and `history.db-shm` path components that are
symlinks/reparse points, nonregular, outside that directory or not private;
verify their identity and permissions again after SQLite opens and after WAL
creation. Encode the absolute database path as a URI path component rather than
concatenating it into a SQLite DSN. A replacement or failed verification makes
the store unavailable. The same-user race between the pre-open check and
SQLite's internal open is a remaining platform limitation; no claim of hostile
same-user process isolation is made without a no-follow VFS. Native tests must
cover static substitution and reopen, including WAL/SHM paths.

Version 1 has these tables. Timestamps are UTC RFC 3339 strings with exactly
nine fractional digits (`2006-01-02T15:04:05.000000000Z`), so SQLite text
ordering matches time ordering at whole-second boundaries. List filters and
cursors use the same representation. Canonical safe projections are bounded
JSON BLOBs with sorted object keys, no insignificant whitespace and preserved
JSON number lexemes; duplicate object keys are rejected before secret checks.
`digest` is SHA-256 of the stored canonical bytes and is verified with JSON
validity on every projection read. A mismatch makes history unavailable rather
than displaying the row as trusted. Foreign keys cascade only within the
private store:

- `runs`: `run_id TEXT PRIMARY KEY`, required `project_id`, `workspace_id`,
  `document_id`, `graph_id`, `graph_hash`, `started_at`, `storage_state` and
  nonnegative `revision`; nullable `ended_at` and `runtime_state`; `pinned`
  and `pending_approval` as checked booleans; required `safe_graph_json`,
  `graph_digest`, `safe_launch_json`, `launch_digest`, `completeness_json` and
  `completeness_digest`; nullable `safe_result_json` and `result_digest` must
  appear together. Stored states are `active`, `complete` or `interrupted`.
- `events`: required `run_id`, positive `sequence`, `event_json`, `digest`;
  primary key `(run_id, sequence)` and foreign key `run_id` to `runs`.
  Canonical JSON preserves schema and event identity.
- `event_omissions`: required `run_id`, positive `sequence`, fixed `reason`
  code (`sensitive_identity` or `not_retained`);
  primary key `(run_id, sequence)`, foreign key to `runs`, and mutually
  exclusive with an `events` row at the same sequence. Adjacent rows may be
  coalesced into ranges in page DTOs; the persisted key stays exact.
- `node_outcomes`: required `run_id`, `node_run_id`, `node_id`, `status`,
  `safe_json`, `digest`; primary key `(run_id, node_run_id)`, foreign key to
  `runs`. `attempt_outcomes` similarly requires `run_id`, `attempt_id`,
  `node_run_id`, `status`, `safe_json`, `digest`, with primary key
  `(run_id, attempt_id)` and foreign keys to `runs` and `node_outcomes`.
  These are indexes derived from runtime facts/results, never another state
  machine.
- `managed_evidence`: `evidence_id TEXT PRIMARY KEY`, required `run_id`,
  `attempt_id`, `kind`, `availability`, `created_at`; nullable `node_run_id`,
  `relative_path`, `byte_length`, `digest`, `omission_reason`. Available rows
  require a private relative path, nonnegative length and digest; unavailable
  rows require one fixed reason code (`preimage_missing`, `diff_unavailable`,
  `redaction_failed`, `size_limit`, `capture_failed` or `unsafe_path`) and no
  path. Callers never persist a raw error, path or secret in a reason. Reads
  reject unknown/corrupt codes. The foreign key references `runs`;
  final ingestion validates its attempt identity against derived
  `attempt_outcomes`, because active-run capture rows precede that index.
  Repository paths are never managed evidence paths.
- `store_meta`: one row with `id INTEGER PRIMARY KEY CHECK(id=1)` and
  nonnegative `list_generation`. A list-visible mutation increments it in the
  same transaction as the changed run rows.

Indexes support `(project_id, started_at DESC, run_id DESC)`,
`(graph_id, started_at DESC, run_id DESC)`,
`(storage_state, started_at DESC, run_id DESC)` and the unfiltered
`(started_at DESC, run_id DESC)` page order. Every insert or mutation that
changes a list row or its filter membership advances a persisted store-list
generation. Each page reads one snapshot transaction; its opaque cursor binds
the exact filters, `(started_at, run_id)` key and generation. A later insert,
pin or `active` to `complete` transition returns `stale_cursor` rather than
silently duplicating, omitting or reordering rows. Migration
SQL must enforce non-null, state/boolean and pair constraints. V1 migration
creates all tables and indexes inside one transaction before setting
`user_version=1`; a failed transaction leaves the prior version intact.

Before any execution event or node dispatch, the runtime invokes a narrowly
scoped `BeforeDispatch(runID) error` hook after normal preflight and run/node
identity allocation. Refactor machine construction so it does not append or
deliver `run_started`; commit the safe `active` launch marker through the hook
first, then append `run_started`, report scope facts, invoke `OnRunReady`, and
dispatch. A hook failure rejects Start with no accepted identity, no node
execution and an empty live event stream. The marker may remain `active` if a
later pre-dispatch step fails; restart classifies it as interrupted/incomplete.

After each Agent produce step, complete and fsync safe managed files and
commit their available/unavailable metadata in `managed_evidence` before
advancing to a Gate, retry or pending Human request. The active run also keeps
a bounded in-memory index of these immutable references and omissions, exposed
through the same typed evidence query so a pending Human request can inspect
them before final ingestion. A capture/database failure publishes explicit
unavailability to that live index; it cannot claim available content merely
because an unreferenced file exists. Final ingestion validates and reuses the
committed references without recapturing prior attempts.

On ordinary finish, the sidecar transactionally ingests retained safe events,
results, node/attempt indexes and the already committed evidence references
before advertising an archived run. Duplicate ingestion of the same safe bytes
is idempotent. The same `(run_id, sequence)` with different safe bytes is an
atomic conflict. A failed final write reports `history_unavailable` only in
live memory, blocks new starts until storage is writable and never claims that
state was committed. The durable marker remains `active`; on restart it becomes
`interrupted` storage status without synthetic runtime events or automatic
resume.

Final ingestion rejects any individual safe event, node outcome or attempt
outcome projection above 3 MiB with a typed page-size error before committing
the final transaction. The limit leaves room for one node plus one attempt in
a 7 MiB read page at `limit=1`, including bounded IDs and envelope metadata;
an archived item must never be permanently unpageable. The integrator reduces
oversized safe detail before finalization. An event may instead become an
explicit original-sequence omission; node and attempt outcomes require a
smaller safe projection because they have no omission slot. The general safe
JSON parser retains its 8 MiB cap for run-level projections, whose combined
Launch/Get fit is checked separately.

The live inspector, `runs.events` and history APIs share one bounded safe
projection. Redact full nested payload values before serialization and full
bounded evidence content before writing files. Event schema, sequence,
run/node/attempt IDs, typed reference identity, required enums and shape fields
remain unchanged. If a known secret occupies an identity-bearing field, omit
that event from the inspector projection and report the omission through
out-of-band page/run completeness metadata; do not fabricate an event at its
sequence. Cursors advance past omitted sequences. If `run_finished` is omitted,
the safe run snapshot still reports terminal status and incompleteness.
Runtime marks facts whose identity/path/reference fields changed during its
own redaction with a memory-only `Event.IdentityRedacted` hint excluded from
event JSON. A valid observed fact made unrepresentable by redaction reserves
its original sequence as a `sensitive_identity` omission at the scheduler's
normal append position. The projector must honor that hint; it cannot infer
provenance from the replacement marker text.
Idempotence hashes the canonical safe projection, so distinct raw events that
redact to identical bytes are identical for storage.
The event projector accepts only typed event payloads. It checks each identity
for byte-preserving UTF-8 and private-value collisions before serialization,
redacts content and Gate evidence bytes before their base64 encoding, and omits
an unrepresentable fact at its original sequence. The dedicated safe-event
constructor scans typed fields before encoding and decoded JSON **values**
after encoding; fixed event-schema key spellings are not user data. Graph,
launch and output projections scan all remaining dynamic keys and values.
Canonical safe event bytes and their SHA-256 are the sole live/history hash
authority; an event above the indexed 3 MiB bound is a `not_retained` omission.
For graph, launch and result projections, use versioned typed positional
arrays for allowlisted fixed structure before invoking the general safe JSON
parser. A redacted value or omitted member marks its projection incomplete.
Safe graph projection v1 is positional
`[1, graph_hash, schema_version, graph_id, name, metadata, units, inputs,
nodes, edges, graph_input_bindings, outputs]`. A node is
`[id, type, inputs, config, outputs, loop]`; loop is
`[max_attempts, gate_inputs, gate_config, bindings]`. A port/input declaration
is `[id, type_code, required]`, and a launch input is `[name,
[type_code, value]]` inside `[1, entries]`. Type codes 1–5 mean string,
number, boolean, JSON and artifact reference. Known node type codes 1–4 mean
Shell, Agent, Gate and Human. Other node types remain redacted strings in a
partial inspection view. Dynamic config/metadata maps are decoded and walked
under depth/item/byte bounds; nonstring-key or otherwise unsupported YAML
subtrees are omitted and reported as incomplete. A YAML timestamp is rendered
as a tagged, sanitized text value and marked incomplete because the view is
not executable YAML. Numeric config and loop values are checked against known
private strings using their exact JSON encoding before retention.
This avoids a configured private value colliding with an otherwise harmless
fixed DTO key such as `status`. Dynamic object keys and all decoded string
values remain subject to the full key-and-value scan; credential-shaped
dynamic members are omitted with explicit incompleteness. Fixed status and
identity already held in typed run/index columns are not repeated as string
values in the projection, avoiding fixed enum collisions. Do not broadly
exempt string values after serialization. The stored semantic graph hash is
the frozen launch version, separate from the safe projection digest.
Active replay selects one retained candidate at a time under the collector
lock, copying its payload only below a 4 MiB raw bound. Larger raw facts are
reported by original sequence without copying them. The first candidate fixes
the page watermark, delivery stats and finished flag; later immutable facts
are clipped to that watermark. The encoded safe event and omission arrays must
fit the 7 MiB result bound, with cursor progress preserved when a fact is
omitted. Readers lacking bounded selection make replay unavailable rather than
falling back to an unbounded raw copy.

The redaction set contains nonempty frozen environment values, authored Shell
`config.env` values and typed launch input strings, recursively including JSON
string leaves, subject to the existing 128-value/1 MiB limit. Exceeding it
fails preflight without a token. A distinct embedded-secret check rejects
credential-shaped authored Shell `config.env` keys and typed JSON input keys.
Normalize each key by ASCII case-folding and removing `_`/`-`, then check for
`password`, `secret`, `token`, `credential`, `apikey` or `privatekey`. It also
rejects a graph string containing an exact nonempty externally sourced frozen
environment or typed input value. A declared Shell `config.env` literal at
its own noncredential key is exempt from self-occurrence rejection only when
that value is absent from the external environment and typed-input value set.
Ordinary noncredential `config.env` literals are allowed for execution but
redacted in safe graph projections; the exemption never overrides an
external/input secret match.
Structured producer outputs omit credential-shaped members from live and
stored views and mark completeness. Unknown credentials in arbitrary strings
cannot be recognized reliably. No raw environment, credential value, private
reasoning or unredacted event is written to SQLite or managed files. A safe
graph projection carries its semantic hash but is not represented as the
byte-exact YAML source when fields were redacted or omitted.

Immediately before each Agent produce step, capture a separate attempt-start
Git observation and bounded no-follow preimages for regular tracked,
untracked and ignored files in declared scope, enumerated in stable path
order. Include ignored paths in the change comparison, as the existing
F1.7 safety baseline does. If an ignored or other in-scope changed path lacks
a captured preimage, its content diff is unavailable rather than empty. After
production and before Gate/retry, capture bounded postimages and immutable
attempt evidence. A newly created path may use an empty preimage only when
absent in the initial inventory; any changed path without captured preimage
has an unavailable content diff. Limits are 10 MiB per file and 100 MiB per
run, including pre- and postimages. Capture/storage errors surface explicit
unavailability while the run is live, including before a pending Human request.
They never turn a missing artifact into a successful empty diff or rerun an
accepted unit. External concurrent writes can contribute to observed diffs;
these are observations, not exclusive Agent authorship.

Evidence files are completed and fsynced before database references commit.
Every read pins the private directory, refuses symlink traversal and verifies
length and SHA-256 before delivering bounded chunks below the 8 MiB sidecar
frame. A later safe sweep removes files orphaned by a crash between file fsync
and reference commit. Retention defaults to 30 days, accepts a positive day
count or explicit keep-all, and prunes only finalized unpinned runs with no
pending approval. It deletes owned managed files only, never repository files.

The sidecar exposes typed paged `history.list`, `history.get`, `history.events`,
`history.evidence`, `history.pin` and `history.prune` methods. Renderer code
never opens SQLite or arbitrary evidence paths. A minimal read-only historical
detail view shows run facts, node/attempt outcomes, Check Results, Return
Envelopes, evidence availability and completeness after full restart, even
without the repository. Later execution-inspector features may change layout
without changing these safe storage contracts.

The v1 method payloads use the existing strict sidecar envelope, reject
unknown fields and keep each response below 8 MiB:

- `history.list` request: optional `project_id`, `graph_id`, `storage_state`,
  `started_from`, `started_before`, opaque `cursor`; required `limit` in 1–256.
  Response: `runs[]` of run ID, project/graph IDs, start/end, storage/runtime
  state, pinned, revision and completeness summary; `next_cursor` and
  `list_generation`. A cursor binds the exact filters, generation and
  `(started_at, run_id)` key; mutation returns `stale_cursor`.
- `history.get` request: `run_id`, required `outcome_limit` in 1–256 and
  optional opaque `node_cursor` and `attempt_cursor`. Response: the safe run
  graph/launch/result projections, digest/completeness metadata, separate
  `nodes[]` and `attempts[]` pages with `next_node_cursor` and
  `next_attempt_cursor`, typed Check Result/Return Envelope references and
  evidence descriptors from each attempt projection, and storage/runtime
  state. Cursors bind the run ID and stable outcome ID order; oversized
  detail returns a typed size error instead of silent truncation.
- `history.events` request: `run_id`, nonnegative `after_sequence`, `limit`
  in 1–256. Response: original-sequence ordered safe `events[]`, omission
  ranges with reason, `next_sequence`, `watermark`, `finished` and
  completeness. The cursor advances across omitted original sequences and a
  cursor above the watermark is rejected.
- `history.evidence` request: `evidence_id`, nonnegative `offset`, `limit`
  in 1–1,048,576 bytes. Response: availability/omission reason, total length,
  full-content SHA-256, `bytes_base64`, next offset and end flag. The server
  validates the full stored file before returning any chunk. This method also
  reads the active run's committed/in-memory capture index.
- `history.pin` request: `run_id`, `expected_revision`, `pinned` boolean.
  Response: new revision and pinned state; conflicts are typed and do not
  overwrite a concurrent change.
- `history.prune` request: optional positive `retention_days` override or
  explicit `keep_all`, never both; response: bounded removed-run and owned-file
  counts plus skipped-active/pinned/pending counts. The configured setting is
  the default; a caller cannot supply an arbitrary filesystem path.

Wire IDs and enum values are validated at the Go service boundary. A page or
detail that cannot fit one frame returns a typed size error with a smaller-page
instruction. The exact SQL migration and typed Go DTOs must match these fields
before either API is exposed.

## Alternatives

JSON files simplify initial writes but complicate indexed filtering,
transactional migration and cross-record atomicity. An Electron-owned database
would split authority. Persisting every executing transition and resuming work
would prematurely activate F3.2/F3.3. SQLite's larger pure-Go binary is
accepted to keep native packaging free of a C toolchain.

## Consequences and validation

An active trace can be lost after a crash despite its durable launch marker.
The UI must show `interrupted` and incomplete, never recovered success.
Storage or evidence corruption is an explicit availability error, not an empty
history result. Verify migrations/rollback/newer-version handling, write
failures, duplicate/conflicting ingestion, forced death, redaction in DB/WAL
and files, evidence tampering/orphan cleanup, retention confinement, and a
packaged app restart/inspector path after repository removal. Crosscompilation
does not replace native OS tests.

Feature: F2.9 Local Run History and SQLite. Parent design: Phase 2 Local
Desktop IDE.
