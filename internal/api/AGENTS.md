# Local API package guidance

The root `AGENTS.md` applies here. This guide covers `internal/api/` source
and tests. Read the active feature spec and ADR before changing an HTTP,
document or event contract.

## Find the owner

| Concern | Start here |
| --- | --- |
| Listener, authentication, routes, run start, validation and SSE | `server.go` |
| Live event replay and pending human decisions | `hub.go` |
| Graph grants, open/edit/stat/save and file checks | `docs.go` |
| Graph document response types | `docs_dto.go` |
| Read-only capability report routes (ADR-0025) | `capabilities.go`; detection in `internal/capability` |
| CLI composition of store, validators and executors | `cmd/agentplane/serve.go` |
| Renderer forwarding and sidecar token ownership | `desktop/electron/main.cjs`, `desktop/electron/allowlist.cjs` |
| Native graph choice and recent-file grant registration | `desktop/electron/graphs.cjs` |

## Preserve these contracts

- The API observes and steers runs; `runtime` owns scheduling, retries and
  state transitions. `StartFunc` and `ValidateDefinition` are composition
  seams. Keep execution decisions and concrete executor wiring out of this
  package. `POST /runs` accepts exactly one of `graph_path` and `graph_grant`;
  a grant resolves to the saved file, not unsaved editor text.
- Keep the listener on `127.0.0.1` with an ephemeral port, every route behind
  the bearer-token and exact `Host` checks, and responses without CORS headers.
  Keep request bodies bounded and reject unknown JSON fields. Do not log the
  token, graph text or unredacted input.
- In the production `serve` composition, the store append precedes the hub
  sink, so clients see only events accepted by the store. Preserve that
  ordering when changing the composition; `Hub` alone does not enforce
  persistence. Live SSE replays from sequence zero and follows without gaps
  or duplicates. It emits `end` on completion or `error` on persistence
  failure. Runs in this process's hub, including finished ones, replay from
  memory; other finished runs replay from the store. Preserve the distinction
  between a persistence failure and an ordinary run failure. Shutdown cancels
  active runs and admits no new runs.
- Human decisions are scoped to both run ID and node-run ID. The first
  accepted decision wins; a repeat for that run returns 409 with the original
  decision, while a different run gets 404. Cancellation must not discard a
  decision already accepted by the provider. The runtime emits the approval
  facts; the API must not invent state from the HTTP response.
- A graph-document grant is an opaque, bounded, process-local capability for
  a path Electron main registers after a native file choice or from its
  recent-graphs list. The sidecar checks paths, regular files and symlinks;
  Electron main keeps the API token and uses an exact renderer route
  allowlist that excludes grant registration and revocation. The API's bearer
  token alone does not distinguish Electron main from a renderer that somehow
  obtained it. Existing read/run endpoints still accept `path`/`graph_path`;
  write endpoints must stay grant-only.
- `POST /graphs/edit` applies `graph/edit` operations in memory. Only
  `PUT /graphs/save` writes the granted file. Preserve text/size limits,
  `base_hash` conflict responses, symlink and directory rechecks, atomic
  replacement, file mode and create-if-absent behavior. Do not treat the
  in-memory edit hash as the file's save precondition. Document validation
  composes graph rules with executor configuration checks when supplied;
  `graph.Validate` alone does not cover those configurations.

Read `docs/features/F2.5-live-map.md`, `docs/features/F2.9-run-store.md`,
`docs/features/F2.10-human-node.md`, `docs/features/F2.1-graph-canvas.md`,
`docs/adr/0017-human-node-and-local-approval.md`,
`docs/adr/0019-local-run-store-and-loopback-api.md`, and
`docs/adr/0021-local-graph-document-api-and-write-trust.md` for these
contracts. The desktop bridge is an additional trust boundary, not an API
authentication mechanism.

## Choose regression tests

- Auth, routes, lifecycle, store/SSE and decisions: `server_test.go`.
- Grants, edit/save conflicts, path safety, limits and DTOs: `docs_test.go`.
- CLI composition: `cmd/agentplane/serve_test.go`; CLI/API diagnostic parity:
  `cmd/agentplane/validation_test.go`.
- Capability routes, single flight and body rules: `capabilities_test.go`;
  detection itself: `internal/capability/capability_test.go`.
- Renderer route exposure: `desktop/test/allowlist.test.ts`.

SSE uses Electron main's separate `ap:subscribe` bridge in
`desktop/electron/main.cjs`, not the generic request allowlist.

Run `go test ./internal/api/...` before `make check`. For changes to hub
concurrency, streaming or cancellation, also run
`go test -race ./internal/api/...`. Run the relevant CLI or desktop tests
when changing a cross-component boundary. Update this guide when a named
owner or contract changes.
