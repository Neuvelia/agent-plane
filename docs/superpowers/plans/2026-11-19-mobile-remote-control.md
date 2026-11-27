# Mobile remote control for Tarmind - plan

**Status:** proposal for the product owner (not an active feature spec; no ADR accepted)
**Date:** 2026-11-19
**Roadmap touchpoints:** F4.1-F4.10 (Phase 4), F5.3 (alerts), F6.1/F6.2 (managed runners, artifacts)
**Depends on (already on main):** F2.10 human node (ADR-0017), F3.2 durable run state (ADR-0027),
F2.1-F2.4 graph grants (ADR-0021), loopback API (ADR-0019), F3.4 cancel/timeouts (spec drafted,
ADR-0028 proposed), F3.8/F3.9 secrets and policy (ADR-0029 proposed)

Product name: Tarmind (in code today: agentplane). Identifiers, paths and event names below keep
the code name.

## 0. Honest scope note

`AGENTS.md` puts the cloud control plane, accounts and any remote service out of scope until a
feature spec explicitly activates them, and requires that core execution never needs them. Phase 4
is on the roadmap but no F4.x spec exists. This document is therefore a **proposal**: it does not
authorise implementation. Slice M0 (below) is the only part that needs no activation, and it is
docs and tests of a pure projection. Everything that opens a network path needs the owner to
activate the matching F4.x spec and accept the ADRs listed in section 5. Nothing here changes the
local engine; the runner remains fully useful with the phone feature absent.

## 1. Goal and non-goals

### Goal

A person who started work on their desktop can leave it, and from a phone:

- see what is running, what is waiting for them, and what failed and why;
- decide (approve or reject) a human gate with enough context to decide safely;
- stop a run that is going wrong;
- start another run of a graph they already saved and marked as startable;
- be told, without content leaking to third parties, when attention is needed.

The phone is a **supervisor of the runner**, driven by the same graph/run/event model as the
desktop. It consumes a bounded projection of the existing event stream and issues a small closed
set of commands that the runner validates exactly as it validates local ones.

### Non-goals

- Repository contents stay on the runner by default. No source, diffs, file paths beyond what
  a bounded gate reason already contains, or full logs cross the boundary in v1.
- The phone is not a code editor, not a graph editor, not a terminal, not a diff reviewer.
  Graph authoring stays on the desktop (F2.1-F2.4).
- No inbound port on the user's machine in the production design. (Section 3 lists the one
  opt-in exception, a LAN prototype, and why it is labelled a prototype.)
- No new execution semantics: no remote shell, no remote arbitrary graph upload, no changing a
  graph's commands, policy or secrets from the phone.
- No model-judged approval and no "auto-approve from phone": approval stays based on consequence
  (AGENTS.md rule 9), not on how convenient the phone is.
- Not a general remote-desktop or agent-chat client. Not copying any other product's surface.
- No team features, RBAC or billing (Phase 5). One person, one account, N devices.
- No cloud runners in v1 (Section 2, "Later"). The protocol is designed so they fit, nothing more.

## 2. User stories

### v1 (single user, own desktop runner)

| ID | Story | Notes |
| --- | --- | --- |
| U1 | As a developer I pair my phone to my desktop runner once by scanning a code on the desktop screen. | Desktop must confirm; works with one runner, then several. |
| U2 | I see the runner's presence (online, last seen, what version) and a list of recent runs with status. | Mirrors `GET /runs` summaries. |
| U3 | I open a run and see node states, current attempt number, durations and the live status line. | Reduced from projected events; same reducer as desktop. |
| U4 | I see a Gate verdict (pass/fail), the check name, the bounded reason, and an evidence summary (counts and typed references, not file bodies). | F1.5 Check Result summary. |
| U5 | I see every pending human gate across runs ("Needs you" list), with the prompt/context the runner bound to it. | Mirrors `GET /approvals`. |
| U6 | I approve or reject a human gate. The decision is bound to the exact request I saw, and the result (recorded or already decided) is shown. | Section 3.6. Consequence class may restrict approve. |
| U7 | I cancel a run (or a single run, never a single accepted node). | Needs F3.4 cancel route. |
| U8 | I start a run of a graph I earlier marked "allow remote start" on the desktop, with its declared inputs only. | Needs F4.6-equivalent safeguards. |
| U9 | I get a notification when a gate needs me, a run finishes, or a run fails, and it does not show graph names, prompts or paths on the lock screen unless I opt in on the device. | Section 4.3. |
| U10 | I see a clear "stale / runner offline" state instead of a spinner, and I am never shown an action that cannot currently succeed. | Offline rule 3.5. |
| U11 | I list paired devices on desktop and phone, and revoke one; revoked devices stop working within seconds. | Section 3.4. |
| U12 | The app works in English and Polish with screen-reader labels for every control and status. | Same practice as `desktop/src/i18n`. |

### Later

| ID | Story | Depends on |
| --- | --- | --- |
| L1 | The same app supervises a managed cloud runner. The UI labels that the provider hosts the code. | F6.1, F4.9 |
| L2 | I fetch a bounded artifact or a log excerpt on demand, per project opt-in, decrypted on the phone. | F3.5, F4.10 |
| L3 | Scheduled runs and alert rules (what notifies me and when). | F5.2, F5.3 |
| L4 | Team approval (who may approve what). | F5.5, F5.6, F6.4 |

## 3. Architecture

### 3.1 What exists today (facts the design builds on)

- The engine emits ordered, redacted events with `run_id`, per-run `sequence`, node run and attempt
  IDs, graph hash and event schema (currently 13). The store persists them as
  `<store>/<run_id>/events.jsonl` (ADR-0019, ADR-0027); the desktop UI is a reducer over them
  (`desktop/src/reducer.ts`) and the stored log and live stream reduce to the same state.
- `agentplane serve` binds `127.0.0.1` on an ephemeral port, requires a 32-byte bearer token,
  checks the Host header, sends no CORS headers and has no remote-bind option. Routes today:
  `GET /runs`, `POST /runs`, `GET /runs/{id}`, `GET /runs/{id}/events` (SSE replay then follow),
  `POST /runs/{id}/decisions/{nodeRun}` (idempotent, scoped to the run), `GET /approvals`,
  `POST /graphs/validate`, `POST /graphs/grants`, `DELETE /graphs/grants/{id}`,
  `POST /graphs/open`, `POST /graphs/stat`, `POST /graphs/edit`, `PUT /graphs/save`,
  `GET /capabilities` and `POST /capabilities/check`. None of the graph or capability routes
  may be reachable by a phone. There is **no HTTP cancel route** yet; it is a F3.4
  deliverable (ADR-0028 proposed). The cancel *events* (`cancel_requested`,
  `process_terminated`) already exist at schema 13.
- Human gates are events, not scheduler states: `approval_requested` without `approval_decided`
  means waiting. A decision carries no approver identity (ADR-0017 states this explicitly).
- `POST /runs` accepts a `graph_grant` (a saved graph, by id) from the desktop renderer and, for
  other clients, `graph_path`. Grants are the existing "the person at the keyboard chose this
  graph" mechanism (ADR-0021).
- Redaction happens before an event is emitted or stored; F3.8 extends the redaction set to every
  resolved secret value (ADR-0029). The runner never has an unredacted event in its retained
  stream, so a mobile projection starts from the already-redacted fact.
- Architecture: cloud concerns must not enter `internal/runtime`; the runner should dial out;
  a user need not expose an inbound port (`docs/architecture.md`, "Trust boundaries").

### 3.2 Requirements derived from the rules

1. Phone control is a **client of the existing run/approval contracts** through a new narrow
   adapter, never a second engine path and never a new way to start arbitrary commands.
2. Every remote command and its outcome is an observable, structured fact (rule 8), including
   which device issued it. This needs additive events (section 5, ADR-C).
3. A command that arrives stale, replayed, out of scope or from a revoked device fails closed
   with a typed error and changes nothing.
4. Content leaves the machine only after redaction and only the allowlisted fields of section 3.7.
5. Offline or hostile network never causes an approval to be recorded that the human did not
   make about that exact request.
6. Local-first: with the feature disabled (the default) no listener, no outbound connection and
   no code path is active.

### 3.3 Options compared

Three ways to connect a phone to the runner.

**(a) Direct LAN pairing, no relay.** Runner opens a second, mobile-scoped listener on the LAN
(TLS with a key pinned from the QR code). Phone talks to it directly.

**(b) Outbound runner connection to a relay/control plane (F4.4) with end-to-end encryption
between phone and runner.** The runner dials out to a relay and holds the connection. The phone
dials the same relay. Messages between the two are encrypted by device keys; the relay forwards
ciphertext and routes by opaque IDs. This is the roadmap's own shape (Phase 4: outbound secure
connection, no inbound port).

**(c) Third-party tunnel** (a mesh VPN or a quick-tunnel product that publishes the loopback API
at a URL). The existing token-protected API is reachable from anywhere.

| Dimension | (a) LAN direct | (b) outbound + relay, E2E | (c) third-party tunnel |
| --- | --- | --- | --- |
| Trust boundary | None added beyond the LAN; the LAN and Wi-Fi become the perimeter. | Relay is untrusted for content (ciphertext only), trusted for availability and routing metadata. | Tunnel vendor terminates TLS: it can read and modify everything, including approvals and the bearer token. Content boundary is the vendor's. |
| Inbound port on user machine | Yes, a LAN listener (breaks the architecture's "no inbound" default; opt-in only). | No. | No on the machine, but a public URL maps to the full admin API (graph write, run start). |
| NAT / roaming | Fails off the LAN, on captive/guest Wi-Fi (client isolation), on mobile data. | Works anywhere with outbound HTTPS/WebSocket. | Works anywhere; depends on the vendor. |
| Offline behaviour | Phone only works when both are on the same network and the desktop is awake. No push. | Relay can queue a wake-up and the last snapshot ciphertext; commands never queue past a short TTL. Push possible. | Same as (b) for reachability, but no push without more work; vendor outage = no access. |
| Battery | Discovery (mDNS) and keep-alive on the phone are costly; no push, so the app must poll while open. | Push for attention events; one WebSocket only while the app is in the foreground. | Same as (a) polling or app-held socket. |
| Security | Smallest remote attack surface but a new LAN-reachable listener on a machine that executes code; local-network attackers, rogue Wi-Fi. Pinned key helps. | One extra outbound connection; compromise of the relay does not yield content or the ability to forge commands (device-signed, AEAD, replay counter). Needs a real crypto protocol (ADR cost). | The loopback token becomes an internet-reachable secret; the Host check is bypassed by design; weakest. Violates AGENTS.md ("do not expose a network listener beyond loopback by default") in spirit even if the process still listens only on loopback. |
| Cost | None. | Relay hosting and ops (small: ciphertext fan-out, no storage of content), push providers, store accounts. | Vendor pricing or the user's own setup; no cost for us, no control either. |
| Fit with Phase 4 order | Not on the roadmap; useful only as a prototype of the protocol and UI. | Is Phase 4: F4.3 enrollment, F4.4 connection, F4.5 presence, F4.9 baseline, F4.6/F4.7/F4.8 features. | Not on the roadmap; cannot satisfy F4.9. |
| Product/ops risk | Support burden of "why can't my phone find my PC". | Needs a service we operate (or self-host option). | Outsources the trust decision; hard to explain to users. |

### 3.4 Recommendation

**Target architecture: (b).** Outbound runner connection to a relay, end-to-end encrypted between
the paired phone and the runner, content-blind relay, with push as a content-free wake signal.
Reasons: it is the only option consistent with "no inbound ports", it survives NAT and mobile data,
it makes push possible, and its trust model (relay sees metadata, never content or approval
authority) is defensible and explainable.

**Prototype path: (a) behind an explicit opt-in flag, as a protocol rehearsal only.** Slice M2
builds the pairing, device keys, command envelope, projection and mobile UI against a LAN
listener so the hard parts (crypto, replay, approval binding, UI) are proven with no cloud and
no account. The LAN listener is:

- off by default, enabled only by an explicit flag/setting (for example
  `agentplane serve --mobile-lan`, name to be fixed in the spec), with a visible indicator in
  the desktop UI while on;
- a **separate listener** from the loopback admin API, serving only the mobile command and
  projection allowlist (never graph edit/save/grant/open routes, never `graph_path`);
- bound to a configured interface, TLS with the runner's device key, authenticated by device
  keys after pairing, gone when `serve` exits;
- documented as not a supported production mode, because it contradicts the default in
  `docs/architecture.md` and `docs/security-model.md` ("bind future local HTTP APIs to loopback
  unless explicitly configured otherwise" - explicit configuration is allowed, but the ADR must
  say so and bound it).

**Rejected: (c) as a product path.** It makes the full admin token internet-reachable and puts a
vendor on the approval path. Users may do it themselves; Tarmind does not document or ship it.

The relay in (b) should be self-hostable (single binary, no database for content) so a
local-first user can run their own, and so the product never *requires* our service (rule 1).

### 3.5 Offline and degraded behaviour (design rule)

- Runner offline or asleep: the phone shows the last snapshot with its age and "runner offline";
  all command buttons are disabled with a reason. Runs on a sleeping desktop are the desktop's
  concern (F3.3 resume is out of this plan).
- **No store-and-forward of commands** beyond a short freshness TTL (default 60 s, runner-enforced
  against a signed timestamp and counter). An approval typed while the runner was unreachable is
  not delivered later; the user must re-confirm against the current request. This prevents "ghost
  approve" after the situation changed.
- Phone offline: events are pulled by cursor on reconnect (`after_sequence`), no loss, no
  duplicates (same guarantee the desktop stream already tests: no-gap/no-duplicate).
- Relay offline: runner keeps running; connector reconnects with bounded exponential backoff and
  jitter; presence goes stale; push cannot be sent (an honest "no updates" banner, not silence).

### 3.6 Pairing, auth, revocation, replay, rate limits

**Identities.** Each runner install has a long-term key pair (signing, plus a key-agreement key).
Each phone install has its own key pair generated on the device in the hardware-backed store
(Keychain/Secure Enclave, Android Keystore/StrongBox where available); the private key is
non-exportable. Keys are per device, not per account.

**Pairing (QR, one-time, confirmed on the desktop).**

1. Desktop (Settings > Mobile) generates a pairing offer: runner public key, a 128-bit one-time
   pairing secret, expiry (2 minutes), relay address (empty for LAN prototype), runner label.
   Shown as QR; the secret is never logged or stored beyond the offer. The offer also carries
   the runner's **fingerprint** (hash of its public key) and label; the phone displays both
   and the desktop shows the same fingerprint, so the user compares them. The relay address in
   a QR is a hint only: the phone accepts a relay address solely from the signed runner
   record obtained through the handshake (or the app's built-in default), and never connects
   to or sends credentials to an address that appears only in a scanned code. A malicious QR
   (attacker's runner key and relay) is caught by the fingerprint/label check and by the
   rule that pairing is a handshake with the runner, not a URL to follow.
2. Phone scans, generates/loads its device key, opens an authenticated key exchange to the
   runner that mixes in the pairing secret (a PSK-style handshake, so a bystander who saw only
   the runner's public key cannot pair).
3. Both screens show a short confirmation code derived from the handshake transcript. The
   **desktop requires a click** on "Pair this device (name, code)". A photographed QR alone is
   not enough; the offer is single-use and dies at first use or expiry.
4. The runner stores the device record (device id, public key, label, created, last seen,
   scope) in the local config area; the phone stores the runner record. No account is needed
   for the LAN prototype; with a relay, device ids are registered to the account (F4.1/F4.3).

**Session auth.** After pairing, each connection is a fresh mutually authenticated key exchange
using the stored static keys (forward secrecy by ephemeral keys); then an AEAD channel. Only
then are any projection frames or commands exchanged. **Transport layering (M2 and M3):** the
Noise-style handshake is the only authentication and confidentiality layer; it authenticates
both device and runner by static key and binds the session. In the LAN prototype the TCP/TLS
layer is transport only: TLS uses a self-signed certificate whose key is the runner key from the
QR, pinned by the phone, so it adds no trust of its own and a failure of either layer closes the
connection; the Noise channel runs inside it. Over a relay there is no TLS-level
authentication of the runner (the relay's TLS only protects the hop). **Version negotiation is
authenticated:** the highest protocol version each side supports is sent as handshake prologue
data, so it is part of the transcript hash; both sides abort if the negotiated version is not
the highest common one (downgrade attempts by the relay or a LAN attacker fail), and there is no
unauthenticated fallback or "legacy" mode. The recommended primitive is a standard
framework handshake (Noise IK/XX with PSK for pairing) rather than a bespoke scheme; the
choice and the dependency question (Go standard library has X25519/Ed25519/AES-GCM but the
project is on Go 1.23 and ChaCha20-Poly1305/HKDF live in `golang.org/x/crypto`) belong to
ADR-B. AGENTS.md's dependency policy applies: this is a justified, runtime, security-sensitive
dependency and must be argued in that ADR, not assumed.

**Replay protection (defence in depth).**

- AEAD session nonces are strictly increasing; a repeated or reordered frame closes the session.
- Every command carries `device_id`, a per-device monotonic `counter` (persisted by the runner;
  a command with counter <= last accepted is rejected), a `sent_at` within +/- 30 s of runner
  time (skew reported to the phone), and an `idempotency_key`.
- State-changing commands are **bound to the object they act on**: a decision carries
  `run_id`, `node_run_id`, `approval_requested` event sequence, the graph hash, and an
  **approval digest**. The runner recomputes the digest from its own event and rejects a
  mismatch (what you see is what you approve). The digest is defined over a **canonical,
  versioned byte form**, never over what a screen rendered:
  `digest = SHA-256("tarmind.approval.v1\0" || canonical)` where `canonical` is, in this
  order, length-prefixed (uint32 big-endian) UTF-8 NFC strings or fixed-width integers:
  `run_id`, `node_run_id`, `approval_requested.sequence` (uint64), `graph_hash`, `prompt`,
  `context`, `consequence_class` (if the gate declares one, else empty), then the
  `decision_kind` the signature is for (`approve` or `reject`). `prompt` and `context` are
  the exact bounded strings stored in the runner's `approval_requested` event after
  redaction and the runner's own truncation, **before** any localization, markdown or display
  truncation on the phone. The phone may render a localized or shortened view, but it hashes
  and signs the canonical form it received in the frame; if the received frame's own digest
  field does not equal its recomputation the app refuses to enable the buttons. A new canonical
  version gets a new prefix and the runner accepts only versions it can still recompute.
  Test vectors (fixed inputs -> fixed digest, including NFC/NFD, a multi-byte string, empty
  context and a maximum-length field) live in `docs/validation/` and are checked by both the
  Go tests and the mobile tests. Cancel carries run id and
  `last_seen_sequence`; start carries the grant id and a hash of the declared input values.
- Repeats are idempotent exactly as ADR-0019 decisions are today (first decision wins; the
  repeat returns the original and is not a second fact).

**Authorization scopes per device.** `observe` (read projection), `decide` (approve/reject),
`cancel`, `start`. Default for a new device: `observe` + `decide(reject)`; **every `decide`, reject included, requires device user presence**
(biometric or passcode, bound to the key use) so a phone stolen unlocked cannot decide
anything; the desktop owner
grants `approve`, `cancel`, `start` explicitly at pairing or later, per device. Scope is checked
by the runner, never by the relay or phone.

**Revocation.**

- Authoritative on the runner: removing a device on the desktop deletes its record, closes its
  sessions immediately and rejects any future handshake. This works even with a hostile or down
  relay (the relay can only drop, never authorise).
- Also from the phone ("unpair this runner") and, with accounts, from the account page, which
  tells the relay to refuse the device and tells the runner on next contact.
- Lost phone: revoke from the desktop or account; compromise window = time until the runner
  next sees the revocation (for LAN: immediate; for relay: the next connection, seconds).
- User presence is mandatory, not optional, for every decision (approve and reject) and for
  `cancel` and `start`; a device that cannot provide it cannot be granted those scopes.
- **Desktop lost or reinstalled.** The runner key and device list lived on that machine. A new
  install is a new runner identity: phones show "runner key changed" and refuse to talk to it
  until re-paired by QR (never silently trust a new key under the old name). The old runner's
  records are revoked in the account (F4.1) so the relay stops routing to them. With no
  account (LAN or self-hosted), a lost desktop needs nothing: the phone just forgets the
  runner ("unpair"). A phone can always unpair locally.
- **Account compromise vs runner-authoritative revocation.** The account can only tell the
  relay to *refuse* or *add routing for* devices; it cannot authorise a device. A device
  becomes valid only by a runner-confirmed pairing, so an attacker with the account can
  deny service and read relay metadata but cannot add a device that the runner accepts or
  issue commands. Conversely an account-side "revoke" is advisory to the runner and applied
  on next contact; the runner's own list always wins. Enrolling an additional runner or phone
  from the account is possible but pairing still needs the on-desktop confirmation. Account
  recovery (lost password or second factor) must not reset device keys: it re-establishes
  login only.
- Key rotation: runner key rotation re-pairs all devices (explicit); device keys can be
  rotated without re-pairing by a runner-confirmed rekey message.

**Rate limits (runner-enforced, relay as defence in depth).**

| Limit | Default |
| --- | --- |
| Handshakes per device | 5/min, then backoff; unknown device 10/min per source |
| Commands per device | 30/min total; `cancel`: 10/min; `start`: 5/min (error `rate_limited`, HTTP-equivalent 429) |
| Concurrent remote-started runs | 2 (config, below any local cap); a start over the cap is `concurrency_limit` (HTTP-equivalent 409), not 429 |
| Frame size | 64 KiB; projection batches <= 200 events; excerpt fields bounded |
| Subscriptions per device | 20 runs; one live per run |
| Open sessions per runner | 5 devices |

Every rejection is a typed result (what failed, which device/command, how to correct) and a
structured fact, not only a log.

### 3.7 What data crosses the boundary

Data classes, from least to most sensitive:

| Class | Content | v1 default |
| --- | --- | --- |
| M0 identity/status | run id, node ids and types, node/attempt states, counts, timings, graph hash, event schema, outcome kinds/codes | sent |
| M1 bounded excerpts | the approval prompt/context, Gate reason (bounded), check name, evidence **summary** (counts, typed references by id) | sent, bounded, redacted |
| M2 progress and logs | `log_chunk`, `text_delta`, command output, agent progress text | **not sent in v1**; later per-project opt-in (L2) |
| M3 repository content | diffs, file bodies, artifacts, recorded values | **never in v1**; F4.10-style explicit opt-in later |

Rules:

- Redaction first. The projection reads the already redacted retained event (F0.6/ADR-0019) and
  F3.8's resolved-value redaction applies unchanged. In addition the projector applies a **field
  allowlist per event type**, truncates by byte bound, and refuses to forward a field whose value
  matches a known-secret canary (see tests). If redaction cannot be established (event not from
  the redacted stream) the frame is dropped and a typed gap marker sent, never the raw text.
- Names are sensitive. Node ids and graph names may reveal project internals; with (b) they are
  inside the E2E channel, so the relay never sees them. Only the **relay-visible envelope**
  exists in clear: opaque route ids, device ids, frame size bucket and timing.
- **Metadata leakage is real even when content is encrypted.** The relay and the network see
  when a runner is active, frame sizes and timing (a gate firing, a run finishing), which
  device talks to which runner, connection IPs, and push timing/volume to the provider.
  Mitigations: frames padded to size buckets (e.g. 1, 4, 16, 64 KiB), a low-rate constant
  heartbeat while a session is open so idle and busy look alike, events batched into at most
  one frame per 500 ms, pushes coalesced and rate-limited (no per-event push, at most one
  per approval id plus a digest push per run end), randomized small delay on non-urgent
  pushes. This reduces but does not remove inference (an observer can still learn that a
  user is active); the privacy statement and ADR-D must say so and must not promise
  traffic-analysis resistance.
- Least data per surface: lists carry summaries only; full excerpts are fetched on open.
- Never forwarded: environment values, secret references' resolved values, absolute local paths,
  process argv/env, raw agent reasoning (AGENTS.md: do not expose private model reasoning).
  F3.8's `policy_evaluated` carries only assignment names; the projection forwards counts, not
  names, unless the user opts in.

### 3.8 Mapping the event stream to a remote sync protocol

The existing event envelope is the wire shape. Phone and desktop share one reducer, so the
phone receives **the same envelope with an allowlisted payload**, not a new status blob.

Per event type (current schema 13; names from `internal/domain/events`):

| Event type | Sent? | Payload kept |
| --- | --- | --- |
| `run_started` | yes | run id, graph hash/name, node ids and types (structure for the map) |
| `node_ready`, `node_started`, `node_finished`, `node_terminated`, `run_finished` | yes | ids, state, kind/code, durations |
| `attempt_started`, `attempt_finished`, `attempt_input` | yes (reduced) | attempt numbers, outcome; input values omitted |
| `gate_check` | yes | verdict, check name, bounded reason, evidence counts and typed refs |
| `approval_requested`, `approval_decided` | yes | bounded prompt/context, digest, decision, approver device id (new) |
| `cancel_requested`, `process_terminated` | yes | reason, source (new field), outcome |
| `workspace_*`, `source_workspace_changed` | yes (state only) | state and counts, no paths |
| progress (`status`) | yes | bounded status line |
| progress (`text_delta`, `file_changed`), `log_chunk` | no (M2) | replaced by a count marker |
| `attempt_diff`, `value_recorded`, `return_envelope`, `node_evidence` | no (M3) | omitted marker with kind and size class |
| `policy_evaluated`, `policy_violation` (F3.9) | yes (reduced) | violation kind and node id; no values |

**Messages (all inside the AEAD channel, versioned `mobile_protocol_version`):**

- `hello` / `hello_ok`: protocol version, runner id, event schema range supported, server time,
  device scope, limits.
- `snapshot`: run summaries (the shape of `GET /runs`), pending approvals (the shape of
  `GET /approvals`), runner presence and capability summary.
- `subscribe{run_id, after_sequence}` / `events{run_id, from, to, events[]}` with an `omitted`
  list for gaps; the phone acks `to`. Replay then follow, no gap, no duplicate: the runner
  streams from its durable store by sequence, so a reconnect resumes exactly.
- `command{type, device_id, counter, sent_at, idempotency_key, target, payload}` and
  `command_result{status, code, message, original?}` with the same typed codes the API uses
  (404 not pending, 409 already decided with the original, 422 stale digest, 403 scope, 429).
- `ping`/`pong` with presence; `bye` with reason (revoked, protocol too old).

Properties inherited rather than rebuilt: sequence is the cursor; a run ends in exactly one
terminal event; a log truncated at the tail is `interrupted` (F3.2); events from a schema newer
than the app are rendered as "update the app", never as a crash (same rule as `unsupported` in
F3.2). The phone never invents state: the reducer is the single source of derived status.

## 4. Mobile client

### 4.1 Options compared

| Dimension | React Native + Expo | Native Swift + Kotlin | PWA (installed web app) |
| --- | --- | --- | --- |
| Reuse of existing code | High: `types.ts`, `reducer.ts`, event decoding, i18n tables, tokens JSON, fixtures can be shared as a small TS package. | None; two ports of the reducer and protocol. | Highest: same React code, but no shared secure storage. |
| i18n / a11y practice | Same string tables (`en.ts`/`pl.ts` parity test); RN accessibility roles/labels; screen readers on both OSes. | Best platform fidelity; double the work. | Web a11y tooling (axe) works; weaker platform integration. |
| Secure key storage | Keychain/Keystore via maintained modules; hardware-backed, biometric-bound keys. | Best. | WebCrypto non-extractable keys in IndexedDB: no hardware/biometric binding guarantee; weaker. |
| QR scanning, biometric gate | Camera and local-auth modules. | Native. | Camera via getUserMedia works; no biometric-bound key. |
| Push without content leak | Provider push with a content-free wake plus on-device fetch/decrypt (needs iOS notification service extension and Android data handling; Expo dev build, not Expo Go). | Natural. | Web Push on iOS only for installed PWAs, limited and not wake-and-decrypt reliably; weakest. |
| Background limits | OS-limited like all; relies on push. | Same. | Tightest. |
| Store policy | Fine; apps are native binaries. | Fine. | No store presence; install friction. |
| Cost to maintain | One codebase, TypeScript, same team skills as `desktop/`. | Two codebases, two skill sets. | Cheapest, but cannot meet the key-storage and push requirements. |
| Crypto module risk | Needs a native crypto module (JS-only crypto is not acceptable for the key and AEAD). | Platform crypto libraries. | WebCrypto lacks X25519/ChaCha20-Poly1305 in some engines; protocol constraints. |

**Recommendation: React Native with Expo (development builds with a small native module set),
in a new top-level `mobile/` component.** It maximises reuse of the TypeScript reducer, event
types, string tables and design tokens, keeps one codebase for two stores, and still gives
hardware-backed keys, QR scanning, biometrics and the push extension points the privacy design
needs. Native Swift/Kotlin is the fallback if the crypto/push modules prove unreliable (a spike
in M2 decides this with evidence). PWA is rejected for v1 for key storage and push reasons; it
may later serve a read-only status view.

Required hygiene (mirroring `desktop/`): UI strings only through `i18n/en.ts` + `i18n/pl.ts` with
the parity test; every control has an accessible name/role/state; touch targets >= 44 pt;
dynamic type; reduced motion; contrast from the shared tokens (`tokens.json` sync check);
no remote URLs or fonts at runtime; a bundle scan like `desktop/scripts/check-bundle.mjs`;
Storybook-style component stories or equivalent (decision in M2) and an e2e flow runner.

Shared code: extract **after** M1 proves the projection, not before (no opportunistic refactor of
`desktop/`). The first version may vendor a copy with a parity test against the same golden event
logs; a shared `packages/` module is a follow-up decision (open question O6).

### 4.2 Component map

```text
mobile/                   Expo app (new top-level, not under internal/ or desktop/)
internal/remote/          runner-side connector: projection, command port, pairing, device store
                          (NEW package; must not be imported by internal/runtime; archtest-enforced)
cloud/relay/              relay (separate top-level, later; content-blind, self-hostable)
docs/adr/                 ADR-A..D (section 5)
```

`internal/remote` depends on a small consumer-owned port set (the same seams the HTTP API uses:
run store reader, approval hub, run starter, and a new canceller), so the runtime stays unaware
of phones. The architecture test gains a rule: `internal/runtime` and `internal/domain` import
nothing from `internal/remote`; the connector is wired only by the composition root.

### 4.3 Push notifications without leaking content

- The push provider and relay see only: an opaque device push token, an opaque `wake_id`, a
  coarse category code (`attention`, `finished`), and a localized **generic** body
  ("Tarmind needs your attention"). No graph name, prompt, path or run id in the payload.
- Content-bearing text (which gate, which run) is fetched after wake: iOS uses a notification
  service extension to fetch the ciphertext for `wake_id` from the relay and decrypt it with the
  device key, and replaces the body only if the user enabled "show details" for that device;
  Android handles the data message in the app process and posts a local notification the same
  way. If fetch or decrypt fails, the generic text stays.
- Tapping opens the app to the "Needs you" list and the app reconnects and fetches by cursor;
  the notification never carries an approve button in v1 (approval needs the full context and
  biometric confirmation).
- Lock screen detail setting: default hidden; per device, local only.
- Collapse/dedup: one notification per pending approval id; cleared when the decision event
  arrives from any device.
- With the LAN prototype there is no push; notifications are limited to the app's foreground
  events (explicitly a prototype limitation).
- Provider tokens are secrets on the relay; the relay stores no content; token rotation on
  revocation.

## 5. Contracts that need ADRs (to be written, not written here)

| ADR | Decides | Why it is an ADR (AGENTS.md criteria) |
| --- | --- | --- |
| **ADR-A Remote protocol** | Frame format, authenticated version negotiation (no downgrade), the canonical approval digest, messages in section 3.8, projection allowlist per event type, cursor semantics, size/rate limits, error codes, new additive events/fields (`remote_command_*`, approver device id on `approval_decided`, `source` on `cancel_requested`), event schema bump (next free number at merge; 14 is proposed for F3.9). | Changes event semantics and a public/durable contract. |
| **ADR-B Device identity and crypto** | Key types and storage, pairing handshake and PSK, confirmation code, session handshake, forward secrecy, replay counters, revocation model, scopes, the crypto dependency (`x/crypto` or a Noise implementation) and what stays in the standard library. | New dependency, new durable identity contract, security-critical. |
| **ADR-C Approval and command semantics over the network** | Binding of decisions to the exact request (digest), idempotency, consequence-class gating of approve (L3 stays desktop-only by default), freshness TTL, no offline queue, approver identity in the trace, remote start rules (grant opt-in, declared inputs only), remote cancel authority, interaction with F2.10/ADR-0017's "no approver identity" and "no timeout". | Changes approval semantics and the human-gate contract. |
| **ADR-D Cloud trust boundary** | What the relay/control plane may store (opaque routing, push tokens, account/device registry; no content), content-blind guarantee and how it is tested, relationship to F4.7 (server-readable metadata for web history) vs the E2E live channel, managed runners as a different trust class, data residency, self-host option, relay as availability-only, residual metadata leakage (frame
timing and size buckets, push timing) and its mitigations, account compromise/recovery versus
runner-authoritative revocation. | Affects local-vs-cloud trust boundary; expensive to reverse. |

ADR-D must resolve the tension with the roadmap: F4.7 (cloud run metadata sync) and F4.8 (web
approval) imply a server-readable store for history and the web UI, while this plan wants the
mobile live channel to be content-blind. Recommended resolution (open question O2): two
tiers. The **live E2E channel** carries M0/M1 to paired devices (phone, and a web client that
holds a device key). The **server-readable metadata** of F4.7 stays optional, coarser than the
phone projection (counts and statuses), and off by default; a user who chooses E2E only gets no
server-side history, which is acceptable for v1.

A fifth, smaller decision (a section in ADR-A, not its own ADR) covers the LAN prototype listener
and its exact bounds.

## 6. Phased delivery

Slices align with the roadmap's order. Each states the F-dependency, acceptance criteria that can
be measured, and the tests that prove them. "Activation" means the owner accepts the matching
spec and ADR first.

### M0 - Design, threat model and ADR drafts (docs only)

- **Depends:** nothing (it is this plan). Needs owner approval to proceed.
- **Deliver:** this plan reviewed; `docs/features/` specs for M1 and M2 from `_template.md`;
  ADR-A..D drafted as `proposed`; a threat-model section appended to `docs/security-model.md`.
- **Acceptance (measurable):**
  - [ ] the four ADRs exist, each with a Decision, Alternatives, Consequences and Validation
        section, and each lists the tests that will prove it;
  - [ ] `docs/security-model.md` names the relay-hostile, lost-phone, LAN-attacker,
        QR-photograph, malicious-QR, protocol-downgrade, account-compromise and
        account-recovery, desktop-loss, metadata-observer and stale-approval attackers, each
        with a mitigation and the test id that will cover it;
  - [ ] no code in the diff (`git diff --stat` shows only `docs/`).
- **Proof:** review of the diff; docs link check if the repository has one.

### M1 - Runner-side projection and command port, no network (local, no cloud)

- **Depends:** F3.2 (durable events), F2.10 (approvals), F3.4 cancel (for the cancel command; the
  slice can land with `observe`+`decide` and add `cancel` when F3.4's route exists), F3.8/F3.9
  redaction.
- **Deliver:** `internal/remote` with: the projection (event -> allowlisted frame), the command
  validator and port (decide/cancel/start against existing seams), the device record store,
  replay counters, rate limiter, the `remote_command_*` facts. In-process fake transport only.
  Nothing listens.
- **Acceptance:**
  - [ ] projecting every event type in a fixture covering schemas 1-13 yields only allowlisted
        fields; a table-driven test fails if a new event type has no projection rule;
  - [ ] with a canary secret planted in every free-text field of every fixture event (and F3.8
        resolved values), the byte stream of all projected frames contains the canary 0 times,
        including Base64/hex forms (fuzz target, 60 s in CI);
  - [ ] the approval digest matches the published test vectors (NFC/NFD, multi-byte,
        empty context, maximum-length fields, approve vs reject kind) byte for byte, the
        digest ignores any localized or truncated display rendering, and a runner running
        digest `v1` rejects an unknown version prefix;
  - [ ] a `decide` or `cancel` or `start` command without a valid user-presence assertion
        is rejected with a typed error, reject included;
  - [ ] a decision whose digest differs from the runner's own `approval_requested` is rejected
        with the stale code and records nothing; the same decision with the right digest is
        recorded once, a repeat returns the original, a different run's node run id is "not
        pending" (404);
  - [ ] a command with counter <= last accepted, a `sent_at` outside the window, or a revoked
        device id is rejected with a typed error and emits a `remote_command_rejected` fact;
  - [ ] the rate limiter returns the 429 code after the configured count and recovers after
        the window using a fake clock (no sleeps);
  - [ ] `internal/runtime` and `internal/domain` import nothing from `internal/remote`
        (architecture test); `go test -race ./internal/remote/...` passes;
  - [ ] the stored and projected sequences reduce to the same desktop reducer state for the
        existing golden logs (cross-check test through a shared fixture).
- **Tests:** unit and table tests, a fuzz test for the projector, an architecture test, a
  race run, fixture round trip.

### M2 - LAN pairing prototype behind an opt-in flag (no cloud, no account)

- **Depends:** M1; ADR-A, B, C accepted (LAN listener bounded in ADR-A). No F4.x needed.
- **Deliver:** the opt-in mobile listener (separate from the loopback admin API), device-key
  pairing with QR and desktop confirmation, the E2E session, Settings > Mobile on desktop
  (device list, revoke, indicator while on), and the Expo app v0: pair, runs list, run detail,
  "Needs you", approve/reject, cancel, English and Polish, accessibility labels. Start run is
  excluded here.
- **Acceptance:**
  - [ ] with the flag off (default) no extra listener exists: a test lists listening sockets of
        `serve` and finds only the loopback API;
  - [ ] with the flag on, the listener rejects any route outside the mobile allowlist (graph
        edit/save/grant/open and `graph_path` return a typed refusal), and unauthenticated
        connections are closed before any frame is processed;
  - [ ] a pairing offer is single-use and expires in 2 minutes: the second use and a use at
        121 s (fake clock) fail; pairing fails without the desktop confirmation within 60 s;
  - [ ] a handshake whose advertised version list was tampered with (downgrade) fails on
        both sides; a QR whose relay address or runner key differs from the signed runner
        record is refused, and the phone shows the runner fingerprint compared with the
        desktop's;
  - [ ] a recorded handshake transcript replayed against the runner fails; a replayed command
        frame in a new session fails the counter check;
  - [ ] revoking a device closes its live session within 2 s and a new handshake fails (e2e
        test with the real binary);
  - [ ] a phone-recorded `approval_decided` shows the approver device id in the stored trace
        and in `GET /runs/{id}` on the desktop; the desktop UI shows the same decision;
  - [ ] the app passes the a11y gate (every interactive element has an accessible name in en
        and pl; the string-table parity test passes; contrast from tokens) and the e2e flow
        pair -> observe a run -> approve a gate -> cancel a run passes on one iOS simulator
        and one Android emulator;
  - [ ] battery measurement protocol recorded (idle foreground 10 min, no background activity
        after the app is backgrounded for 5 min: 0 sockets open, 0 wake-ups) in
        `docs/validation/`.
- **Tests:** Go integration with the built binary and a Go test client speaking the protocol;
  RN unit tests (reducer parity on shared golden logs, i18n parity); e2e with Maestro or
  Detox on simulator/emulator (tool choice in the spec); manual screen-reader checklist for
  VoiceOver and TalkBack recorded as evidence.
- **Spike (inside M2, decides native vs RN):** one day-bounded spike recorded under
  `docs/validation/`: hardware-backed key, AEAD module, QR scan, push extension feasibility in
  an Expo dev build, and confirmation that the files to be shared or vendored
  (`desktop/src/reducer.ts`, `types.ts` and what they import) have no DOM, Electron or
  `window` imports, so they run under React Native's JS engine.

### M3 - Relay and outbound connection (Phase 4 core)

- **Depends:** F4.1 (account) or a documented accountless self-host mode, F4.3 (runner
  enrollment), F4.4 (outbound secure connection), F4.9 (cloud security baseline) as a gate for
  anything beyond read-only. This slice is F4.3 + F4.4 + the relay half of F4.5 (presence).
- **Deliver:** `cloud/relay` (content-blind, self-hostable), runner connector in
  `internal/remote` that dials out with backoff, device registry, presence, the same E2E
  channel carried over the relay, the desktop "runner status" and the phone "runner presence".
- **Acceptance:**
  - [ ] no inbound port: with the feature on and no LAN flag, `serve` opens no listener beyond
        loopback (socket test), and the connector works with all inbound traffic blocked;
  - [ ] **relay blindness:** a dump of the relay's entire storage and logs after a scripted
        session containing canary strings contains the canaries 0 times, and its decoded
        frames reveal only route ids, sizes and timing (test over a recording proxy);
  - [ ] a malicious relay (test double that drops, reorders, duplicates, replays frames or
        injects a forged command) causes only liveness loss: no forged command is accepted,
        no state differs from the runner's own events;
  - [ ] reconnect after a 60 s network drop resumes from `after_sequence` with 0 gaps and 0
        duplicates in a 10 000-event run;
  - [ ] presence turns stale within 2 heartbeat intervals of the connector dying (fake clock),
        and the phone shows runner offline with commands disabled;
  - [ ] relay compromise does not allow device enrollment: a registry insert by the relay
        without runner confirmation is rejected by the runner;
  - [ ] unauthenticated and over-limit connections are shed under a connection flood test
        with bounded memory (documented limits).
- **Tests:** relay integration tests with fault-injecting proxy, replay/forgery suite,
  load/limit tests, e2e with real runner + relay + test client; security review by a separate
  reviewer session per AGENTS.md role separation.

### M4 - Push notifications (content-free)

- **Depends:** M3, ADR-D. Partly overlaps F5.3 (alerts); this slice is only the minimum wake
  for attention/finish/fail; alert rules and scheduling remain F5.3 (see O5).
- **Deliver:** relay push gateway (opaque token, generic body), runner-side
  `notify_wake` frame, mobile service extension/handler, per-device "show details" setting.
- **Acceptance:**
  - [ ] captured provider payloads for every category contain no run id, graph name, prompt,
        path or node id (assertion over the recorded push request bodies);
  - [ ] with "show details" off the lock-screen text is the generic localized string in en
        and pl; with it on, details come from decrypted content and never from the push
        payload;
  - [ ] a decision from the desktop clears the phone's notification for that approval within
        5 s of the `approval_decided` event on a connected phone;
  - [ ] if the relay is unreachable at wake, the notification still shows the generic text
        (no crash, no empty banner).
- **Tests:** payload capture tests, simulator/emulator notification tests, manual device check
  documented, relay tests.

### M5 - Remote run start (F4.6 shape)

- **Depends:** M3, F4.9 baseline, F3.9 (policy mode `enforce` possible), F2.1-F2.4 grants.
- **Deliver:** desktop "allow remote start" per saved graph (an explicit opt-in recorded next
  to the grant), the `start` command (grant id + declared inputs only), concurrency cap,
  desktop-visible origin ("started from <device>"), `remote_command_*` facts.
- **Acceptance:**
  - [ ] a graph without the opt-in cannot be started remotely (typed refusal; no run created);
  - [ ] a start with an undeclared input name, a wrong type, or an oversized value is rejected
        before any run directory is created;
  - [ ] the started run records the originating device id; a run started remotely under
        `enforce` policy cannot weaken the runner's configured policy mode;
  - [ ] one rule, two codes: the sixth `start` from one device inside one minute is refused
        `rate_limited` (429); with 2 remote-started runs active a start (even the first in the
        window) is refused `concurrency_limit` (409); neither creates a run directory;
  - [ ] a graph edited after the grant (hash differs) is refused until re-granted on desktop.
- **Tests:** API/integration tests with the built binary, rate-limit test with fake clock,
  graph-hash drift test, policy interaction test.

### M6 - Hardening, store readiness and beta

- **Depends:** M3-M5; F4.9 completion.
- **Deliver:** abuse controls (account-level limits), audit-friendly facts, privacy review,
  store metadata (privacy disclosures, encryption export declarations), crash/ANR budget,
  localized store listing, beta channel (TestFlight / Play internal track).
- **Acceptance:**
  - [ ] an external-style review (separate reviewer session) of the protocol and relay finds
        no open high-severity item; findings and their tests recorded;
  - [ ] a repeatable threat-model checklist run passes: replay, forged command, stale approval,
        revoked device, relay compromise, QR photograph, lost phone;
  - [ ] store privacy disclosures match the data classes of section 3.7 (checked by a doc
        review table: every transmitted field mapped to a disclosure);
  - [ ] crash budget: at least 500 beta sessions over at least 10 devices across both OSes
        and 0 crashes or ANRs that lose a pending approval; crash-free sessions >= 99.5
        percent over that sample, or each crash has a filed issue and a test;
  - [ ] cold start to run list under 2 s on a mid-range reference device (documented
        method).

### Later slices (not planned in detail)

- **M7 Cloud runners** (F6.1): same protocol, runner identity is the managed runner's key;
  the UI labels "hosted by provider", ADR-D extended; depends on F3.1, F3.9, F4.9, F5.1.
- **M8 Artifacts and log excerpts on demand** (F4.10, F3.5): M2/M3 data classes, per-project
  opt-in, decrypted on device.
- **M9 Team approvals** (F5.5, F5.6, F6.4): identity and scope model beyond one user.

### What can be prototyped locally first, with no cloud

M1 entirely (no sockets). M2 with the opt-in LAN listener and a Go test client: this proves
pairing, device keys, the E2E channel, the projection, approvals, revoke and the app itself,
including accessibility, with zero accounts and zero hosting. The relay (M3) then only replaces
the transport underneath an already-tested protocol. The explicit LAN flag keeps the default
posture unchanged: no flag, no listener.

## 7. Risks

| Area | Risk | Mitigation |
| --- | --- | --- |
| Security | Remote approval becomes the weakest link of the human gate. | Digest binding, scopes, desktop-only L3 by default, biometric user presence, freshness TTL, reject always allowed, fail closed. |
| Security | Crypto design error. | Standard handshake framework, no bespoke primitives, ADR-B review, protocol test vectors, separate reviewer session. |
| Security | LAN prototype listener on a code-executing machine. | Opt-in, separate allowlist, indicator, pinned key, M2-only, off by default, socket test. |
| Security | Compromised phone. | Per-device scopes, biometric-gated key, desktop revoke is authoritative and immediate. |
| Privacy | Names and prompts leak via relay, push provider, screenshots or OS notification history. | E2E, generic push, lock-screen default hidden, app-switcher blur, no third-party analytics, data-class table. |
| Privacy | Redaction is best effort (ADR-0029 says so). | Projection allowlist, truncation, canary fuzz test, M2/M3 data classes off by default; docs say best effort, not guarantee. |
| Store policy | App Review: minimum functionality, background activity, push use, encryption export declarations, account sign-in rules; Play data-safety and background-execution rules. | Verify against current published guidelines before M6; no downloaded executable code; no background loops; push only for attention; accurate privacy labels. (Not verified here.) |
| Background limits | OS kills background sockets; push is best effort. | Foreground-only socket, push as hint, cursor resume, honest staleness UI. |
| Battery | Keep-alives and polling. | One socket in foreground, adaptive heartbeat, no discovery loops, measure in M2. |
| Cost | Relay ops, push, support, store fees. | Content-blind small relay, self-host option, no storage of content; estimate in M3 with measured connection counts. |
| Scope | The relay and accounts pull Phase 4/5 into the core; AGENTS.md says out of scope until activated. | Slice gating, separate top-level components, archtest, no mandatory service. |
| Product | The phone becomes a second UI to maintain. | Single reducer, bounded surface (supervise, not edit), shared fixtures. |
| Trace | Remote actions without identity weaken the audit trail (ADR-0017 had none). | Additive device id on decisions and commands, `remote_command_*` facts. |

## 8. Open questions with recommended defaults

| ID | Question | Recommended default |
| --- | --- | --- |
| O1 | Is a self-hostable relay required, or only ours? | Both; ship the relay single-binary self-hostable so the product never requires our service. |
| O2 | Server-readable metadata (F4.7/F4.8 web) vs E2E-only? | E2E-only live channel for v1; F4.7 metadata optional, coarser, off by default (two tiers, ADR-D). |
| O3 | May the phone approve L3 (hard-to-reverse) gates? | No by default: L3 requires the desktop; the owner may enable per gate class with extra user presence. L1/L2 may be approved on the phone. |
| O4 | Is accountless pairing acceptable for the relay? | Yes for self-hosted; the hosted relay requires an account (F4.1) for abuse control and revocation. |
| O5 | Where does minimal push end and F5.3 alerts begin? | M4 = attention/finish/fail wake only; rules, schedules, routing stay in F5.3. |
| O6 | Shared TypeScript package or vendored copy of the reducer? | Vendored with a golden-fixture parity test first; extract a shared module once two consumers are stable. |
| O7 | Remote start included in v1? | Yes but last (M5), graph opt-in, declared inputs only. |
| O8 | Notification detail on the lock screen? | Hidden by default, per-device local opt-in. |
| O9 | Mobile OS minimums and tablet support? | Recent two major versions of each OS; phone only; tablet layout later. |
| O10 | Which crypto dependency (Noise implementation vs `x/crypto` primitives)? | Decide in ADR-B after the M2 spike; prefer the smallest audited surface. |
| O11 | Name of the flag and config location for the LAN prototype? | `--mobile-lan` with an interface argument; stored under the runner's existing config area; bound by the M2 spec. |
| O12 | Cancel route timing: ship remote cancel with M2 or wait for F3.4? | Wait: the command exists only when F3.4's persisted-before-acted cancel route exists; M2 may ship without it. |

## 9. Next step if the owner approves

1. Accept this plan as the direction (recommendation 3.4, client 4.1).
2. Activate M0: write ADR-A..D drafts and the M1/M2 feature specs with measurable criteria.
3. Implement M1 (no network) as one feature branch with an independent review session.
4. Re-decide, with the M2 spike evidence, native-vs-RN and the crypto dependency before any
   phone code is written.
