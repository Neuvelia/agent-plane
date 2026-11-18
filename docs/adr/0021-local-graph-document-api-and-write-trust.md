# ADR-0021 - Local graph document API and write trust

**Status:** accepted
**Date:** 2026-11-14

## Context

The editor must open, validate, edit and save graph files through the local sidecar. Today the loopback API
has `POST /graphs/validate {path}` and run start with `graph_path`; neither writes. ADR-0019 states that
`graph_path` is read with the user's own permissions and that the desktop may send a typed path, because the
person at the keyboard chooses which graph to execute.

Writing changes the threat model. The renderer is sandboxed but is the component that renders untrusted
text (graph prompts, log output, diagnostics). If it is compromised, a write endpoint that accepts a
renderer-supplied path lets it overwrite any `*.yml` the user can write, for example a CI workflow, which
is code execution elsewhere. AGENTS.md security rules: treat paths as untrusted, do not silently widen
filesystem permissions.

Also verified: `desktop/electron/main.cjs` forwards any renderer request matching
`^/(runs|approvals|graphs)(/|$)`, so any route added under `/graphs/` would be reachable from the renderer.

Validation is also incomplete today: `serve` and `agentplane graph validate` call only `graph.Validate`; shell,
gate and agent config errors surface only when a run starts (executors' `ValidateNodeConfig`).

## Decision

1. **Writes are capabilities, not paths.** The main process owns file choice. New preload methods
   `openGraph()` and `newGraph()` show native dialogs in main. Main canonicalises the chosen path, registers
   it with the sidecar as a **grant** (`POST /graphs/grants`) and returns to the renderer only an opaque
   `grant` id (128 random bits), a display label (the file name) and, for open, the document. The renderer
   addresses a graph by grant and never learns or supplies its path.
2. **The forwarding rule becomes an exact allowlist.** Main forwards, from the renderer, only
   `/runs`, `/runs/{id}`, `/runs/{id}/events`, `/runs/{id}/decisions/{id}`, `/approvals`,
   `/graphs/validate`, `/graphs/open`, `/graphs/edit`, `/graphs/save` and `/graphs/stat`. `/graphs/grants`
   and any later administrative route are **never** forwarded. A test asserts the renderer cannot reach
   `/graphs/grants`.
3. **Grants are session-scoped and bounded.** They live in sidecar memory, die with the process, are limited
   in number (256), and are revoked when the document is closed. A recent-graphs list is kept by main in user
   data (paths and labels); selecting a recent entry makes main issue a fresh grant from its own stored
   path. The renderer receives labels and recent ids, not paths.
4. **Endpoints** (loopback, bearer token, Host check and no CORS as before; no new listener):

   | Endpoint | Purpose |
   | --- | --- |
   | `POST /graphs/grants` | main only: register a canonical path; returns the grant |
   | `POST /graphs/open {grant}` | source text, source hash (sha256 of the bytes), typed definition DTO, semantic version, diagnostics (graph and node config), field descriptors for the node types in use |
   | `POST /graphs/stat {grant}` | current hash and size, cheap, for change detection |
   | `POST /graphs/edit {grant, base_hash, text?, ops_version, ops}` | apply ADR-0020 operations; returns the new text, definition, version, diagnostics and a result per operation; **does not write**. `base_hash` identifies the text the client holds: the text the server holds for the grant (else 409 `stale_base` with `current_hash`), or `text` sent by the client after typing in the YAML view (then `base_hash` must be that text's hash). The server's held text advances to each successful result |
   | `POST /graphs/validate {text}` or `{grant}` | validate unsaved text or the granted file as it is on disk; exactly one of `path`, `text`, `grant`; `{path}` keeps its original behaviour. Text that does not parse answers 200 with `status: invalid` and `parse_error` |
   | `PUT /graphs/save {grant, base_hash, text}` | atomic write; here `base_hash` is the hash of the **file's** bytes the client last saw (`null` creates a file that must not exist); 409 `conflict` (with `current_hash`), `exists`, `gone`, `not_regular` or `path_changed` otherwise |

5. **Save semantics.** The sidecar requires the grant to exist, the extension to be `.yaml` or `.yml`, the
   target to be a regular file or absent in an existing directory, and the final path component not to be a
   symlink; it re-resolves the directory on every save and refuses if it changed. It writes a temporary file
   in the same directory, fsyncs it, renames it over the target and fsyncs the directory. File mode is
   preserved; a new file gets 0644 (graph files are meant to be committed). `base_hash` is required to
   overwrite; `null` means create-if-absent and fails with 409 if the file exists. On any failure the old
   file is untouched. One save at a time per grant.
6. **Limits.** Graph documents up to 1 MiB (the 64 KiB cap stays for every other body), at most 500
   operations per edit request, text must be valid UTF-8 without NUL. Limits are explicit errors.
7. **One validation path.** The API's validation runs `graph.Validate` **and** the executors'
   side-effect-free `ValidateNodeConfig` for shell, gate and agent nodes (human nodes are already checked by
   `graph.Validate`). `agentplane graph validate` and `serve` share that function, so every CLI diagnostic has an
   API counterpart and the UI shows what `agentplane run` would report at start. Diagnostics keep their
   shape: `severity`, `code`, `message`, `location` (node id, edge index, binding index, path),
   `suggestion`.
8. **Typed document model, not a UI invention.** The API returns an explicit DTO with JSON tags (separate
   from runtime types) and **field descriptors** per node type, generated from the same Go definitions the
   executors validate against (key, type, required, allowed values, nesting for gate kinds and the loop).
   Forms render descriptors; unknown config keys are preserved and shown in an "Advanced" view. Exact shapes
   are fixed in the F2.2 spec.
9. **The renderer starts runs and validates graph files by grant only; `graph_path` stays on the API for other
   clients.** This ADR first left `POST /runs {graph_path}` and `POST /graphs/validate {path}` unchanged, as
   ADR-0019 decided, and stated the exposure: a compromised renderer could ask the sidecar to run any graph
   file the user can read. Issue #56 closed it. New run chooses the file with `openGraph()`, validates with
   `POST /graphs/validate {grant}` and starts with `POST /runs {graph_grant, inputs}`; the renderer holds a
   grant and a file name, never a path, and revokes the grant when it leaves the screen or chooses another
   file. Main enforces this and does not rely on the renderer's code: for those two routes it forwards only
   bodies whose keys are exactly `graph_grant`, `inputs` (runs) or `grant`, `text` (validate), and answers
   400 `body not allowed` otherwise. The check is a key allow-list because the Go decoder matches JSON keys
   case-insensitively, so refusing `graph_path` alone would let `GRAPH_PATH` through. The legacy `pickGraph`
   bridge call, which returned a path, is removed. What remains of `graph_path`: the Go API still accepts
   `graph_path` on `POST /runs` and `path` on `POST /graphs/validate` (exactly one of path, grant or text), read
   with the user's own permissions as ADR-0019 states, for clients other than the renderer: API tests and
   scripts that call a local `agentplane serve` with the bearer token. The CLI reads its graph path directly and
   does not use the API. A compromised renderer can now run only a file the person chose in a native dialog;
   a graph chosen on New run also joins the recent-graphs list, like one opened in the editor.
10. **Run uses saved bytes.** The editor offers "Save and run"; a run never reads unsaved editor state.
    So the renderer can start a run of a graph it holds only as a grant, `POST /runs` additionally accepts
    `graph_grant` in place of `graph_path` (exactly one of the two); the sidecar resolves the grant to the
    saved file. Run start keeps `graph.Validate` as `agentplane run` does, so a node-configuration error
    fails the node when it executes; the editor gates its Run button on the full diagnostics (F2.4).

## Alternatives considered

### A. Keep renderer-supplied paths and rely on sidecar checks

Pros:

- No preload change; consistent with ADR-0019's reading.

Cons:

- The extension and symlink checks do not stop overwriting an arbitrary `.yml` (CI workflows, other tools'
  config). A compromised renderer becomes an arbitrary-file-write primitive. Rejected for writes.

### B. The sidecar shows the file dialog

Pros:

- Grants would not need a main-to-sidecar registration step.

Cons:

- The sidecar is a headless Go process with no UI; it would need a GUI dependency. Rejected.

### C. Rely on an OS-level sandbox for the renderer and sidecar

Pros:

- Defence at a lower layer.

Cons:

- Not portable across the supported desktops and not available as a deliverable of this feature. A useful
  later hardening, not a substitute.

## Consequences

### Positive

- A compromised renderer cannot write to a path it chooses; it can write only to files the user opened or
  created through a main-process dialog, with conflict detection and atomic replacement.
- One validation path for CLI, API and UI.
- The forwarding rule is explicit and testable instead of prefix-based.

### Negative / trade-offs

- The preload surface frozen by F2.5/F2.5a grows by `openGraph`, `newGraph` and the recent-graphs calls;
  `desktop/README.md` and F2.5 must be updated.
- Main and sidecar must agree on canonical paths; grants add state to both.
- New run has no typed-path box any more (issue #56): a graph must be chosen in the native dialog, so a path
  cannot be pasted or scripted into the renderer.
- Descriptors add a Go-side surface that must stay in step with the executors (mitigated by generating them
  from the same definitions).

## Compatibility and migration

Existing endpoints and CLI behaviour are unchanged; `POST /graphs/validate {path}` and `POST /runs {graph_path}`
keep working for API clients other than the desktop renderer (decision 9). The desktop
forwarding rule changes from a prefix match to an exact allowlist; routes outside it, if any renderer code
used them, stop working (only `/graphs/validate` was used). Event schema, run store and graph schema are
untouched. Making `agentplane graph validate` report node-config errors is a user-visible improvement: graphs that
validated cleanly before may now report config errors they would have hit at run start.

## Validation

- A renderer-originated request to `/graphs/grants` is refused; routes outside the allowlist return 400.
- Save refuses an unknown grant, a non-YAML name, a directory, a symlink final component, an oversize body,
  invalid UTF-8, a stale `base_hash` (409) and a create over an existing file (409); after each failure the
  previous file is byte-identical. A killed save leaves either the old or the new file, never a partial one.
- API diagnostics equal `agentplane graph validate` for the same text, including node-config errors.
- `/graphs/edit` does not touch the file system; `/graphs/save` is the only writer.
- An end-to-end run shows a graph created in the UI runs from the CLI unchanged.
