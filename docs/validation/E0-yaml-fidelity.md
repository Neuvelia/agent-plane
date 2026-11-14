# E0 spike: YAML fidelity of graph edits

Part of the graph editor plan (`docs/superpowers/plans/2026-11-14-graph-editor.md`, slice E0). It measures
what survives when a graph file is edited through the `yaml.Node` tree instead of `graph.Parse` and
`graph.Marshal`, so ADR-A (edit operations) can state a guarantee that was measured, not assumed.

## Method

`go run ./experiments/yaml-fidelity` (needs the `diff` binary). For each file it applies eight editor
operations to the YAML tree (identity, set a scalar, replace a flow list, add a node with a head comment,
add an edge, remove a leaf node with its edges, move a node by creating `metadata.layout`, rename a node
and its references), encodes with indent 2, and reports:

- **diff lines:** added plus removed lines against the original (unified diff);
- **comments lost:** original comment texts that no longer appear in the output;
- **blank lines**, **key order kept** (top level and `nodes`, ignoring keys that were added or removed),
  **quoting and flow nodes**;
- **re-parses:** `graph.Parse` accepts the output;
- **semantic version as predicted:** the output's `graph.SemanticVersion` equals the version predicted by
  applying the same edit to the typed `Definition` (unchanged for identity and layout moves). This is the
  safety net proposed in the plan, measured here.

Files: the three example graphs in `.agents/graphs/`, and fixtures in
`experiments/yaml-fidelity/testdata/`: `hostile.yaml` (anchor, merge key, alias, block scalar, comments in
odd places, quoted `#`, flow styles, document start), `comments.yaml` (trailing comments on keys),
`comments.yaml` (trailing comments on keys), `crlf.yaml`,
`bom.yaml`, `long.yaml` (190-character arguments, folded block scalar).

The measured tool uses two things beyond a plain `yaml.v3` encode: a pass that restores blank lines (the
encoder drops all of them) and a pass that restores a byte order mark and CRLF line endings. Both are small
and are part of the result below. `NO_BLANKS=1` shows the plain encoder.

## Results

### .agents/graphs/demo-vertical.yaml

Unpatchable constructs (editor opens canvas read-only): none

| operation | diff lines | comments lost | blank lines (before > after) | key order kept | quoting/flow nodes (before > after) | re-parses | semantic version as predicted |
| --- | --- | --- | --- | --- | --- | --- | --- |
| identity (decode, encode) | 0 | 0 | 1 > 1 | yes | 20 > 20 | yes | yes (unchanged) |
| set `config.command` of the first node | 2 | 0 | 1 > 1 | yes | 20 > 20 | yes | yes (matches typed edit) |
| replace `args` of the first node | 2 | 0 | 1 > 1 | yes | 20 > 19 | yes | yes (matches typed edit) |
| add a node (with a head comment) | 5 | 0 | 1 > 1 | yes | 20 > 21 | yes | yes (matches typed edit) |
| add an edge | 2 | 0 | 1 > 1 | yes | 20 > 20 | yes | yes (matches typed edit) |
| remove a leaf node | 8 | 0 | 1 > 1 | yes | 20 > 16 | yes | yes (matches typed edit) |
| move a node (create `metadata.layout`) | 4 | 0 | 1 > 1 | yes | 20 > 21 | yes | yes (unchanged) |
| move the same node again | 4 | 0 | 1 > 1 | yes | 20 > 21 | yes | yes (unchanged) |
| rename a node (key and references) | 4 | 0 | 1 > 1 | yes | 20 > 20 | yes | yes (matches typed edit) |

### .agents/graphs/hello-parallel.yaml

Unpatchable constructs (editor opens canvas read-only): none

| operation | diff lines | comments lost | blank lines (before > after) | key order kept | quoting/flow nodes (before > after) | re-parses | semantic version as predicted |
| --- | --- | --- | --- | --- | --- | --- | --- |
| identity (decode, encode) | 0 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (unchanged) |
| set `config.command` of the first node | 2 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (matches typed edit) |
| replace `args` of the first node | 2 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (matches typed edit) |
| add a node (with a head comment) | 5 | 0 | 7 > 7 | yes | 14 > 15 | yes | yes (matches typed edit) |
| add an edge | 2 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (matches typed edit) |
| remove a leaf node | 23 | 0 | 7 > 6 | yes | 14 > 10 | yes | yes (matches typed edit) |
| move a node (create `metadata.layout`) | 4 | 0 | 7 > 7 | yes | 14 > 15 | yes | yes (unchanged) |
| move the same node again | 4 | 0 | 7 > 7 | yes | 14 > 15 | yes | yes (unchanged) |
| rename a node (key and references) | 4 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (matches typed edit) |

### .agents/graphs/phase0-cli.yaml

Unpatchable constructs (editor opens canvas read-only): none

| operation | diff lines | comments lost | blank lines (before > after) | key order kept | quoting/flow nodes (before > after) | re-parses | semantic version as predicted |
| --- | --- | --- | --- | --- | --- | --- | --- |
| identity (decode, encode) | 0 | 0 | 8 > 8 | yes | 15 > 15 | yes | yes (unchanged) |
| set `config.command` of the first node | 2 | 0 | 8 > 8 | yes | 15 > 15 | yes | yes (matches typed edit) |
| replace `args` of the first node | 2 | 0 | 8 > 8 | yes | 15 > 15 | yes | yes (matches typed edit) |
| add a node (with a head comment) | 5 | 0 | 8 > 8 | yes | 15 > 16 | yes | yes (matches typed edit) |
| add an edge | 2 | 0 | 8 > 8 | yes | 15 > 15 | yes | yes (matches typed edit) |
| remove a leaf node | 25 | 0 | 8 > 7 | yes | 15 > 11 | yes | yes (matches typed edit) |
| move a node (create `metadata.layout`) | 4 | 0 | 8 > 8 | yes | 15 > 16 | yes | yes (unchanged) |
| move the same node again | 4 | 0 | 8 > 8 | yes | 15 > 16 | yes | yes (unchanged) |
| rename a node (key and references) | 9 | 0 | 8 > 7 | yes | 15 > 15 | yes | yes (matches typed edit) |

### experiments/yaml-fidelity/testdata/bom.yaml

Unpatchable constructs (editor opens canvas read-only): none

| operation | diff lines | comments lost | blank lines (before > after) | key order kept | quoting/flow nodes (before > after) | re-parses | semantic version as predicted |
| --- | --- | --- | --- | --- | --- | --- | --- |
| identity (decode, encode) | 0 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (unchanged) |
| set `config.command` of the first node | 2 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (matches typed edit) |
| replace `args` of the first node | 2 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (matches typed edit) |
| add a node (with a head comment) | 5 | 0 | 7 > 7 | yes | 14 > 15 | yes | yes (matches typed edit) |
| add an edge | 2 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (matches typed edit) |
| remove a leaf node | 23 | 0 | 7 > 6 | yes | 14 > 10 | yes | yes (matches typed edit) |
| move a node (create `metadata.layout`) | 4 | 0 | 7 > 7 | yes | 14 > 15 | yes | yes (unchanged) |
| move the same node again | 4 | 0 | 7 > 7 | yes | 14 > 15 | yes | yes (unchanged) |
| rename a node (key and references) | 4 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (matches typed edit) |

### experiments/yaml-fidelity/testdata/comments.yaml

Unpatchable constructs (editor opens canvas read-only): none

| operation | diff lines | comments lost | blank lines (before > after) | key order kept | quoting/flow nodes (before > after) | re-parses | semantic version as predicted |
| --- | --- | --- | --- | --- | --- | --- | --- |
| identity (decode, encode) | 12 | 0 | 1 > 1 | yes | 4 > 4 | yes | yes (unchanged) |
| set `config.command` of the first node | 14 | 0 | 1 > 1 | yes | 4 > 4 | yes | yes (matches typed edit) |
| replace `args` of the first node | skipped: first node has no config.args | | | | | | |
| add a node (with a head comment) | 17 | 0 | 1 > 1 | yes | 4 > 5 | yes | yes (matches typed edit) |
| add an edge | 14 | 1 (# none yet) | 1 > 1 | yes | 4 > 3 | yes | yes (matches typed edit) |
| remove a leaf node | skipped: no leaf node | | | | | | |
| move a node (create `metadata.layout`) | 16 | 0 | 1 > 1 | yes | 4 > 5 | yes | yes (unchanged) |
| move the same node again | 16 | 0 | 1 > 1 | yes | 4 > 5 | yes | yes (unchanged) |
| rename a node (key and references) | 12 | 0 | 1 > 1 | yes | 4 > 4 | yes | yes (matches typed edit) |

### experiments/yaml-fidelity/testdata/crlf.yaml

Unpatchable constructs (editor opens canvas read-only): none

| operation | diff lines | comments lost | blank lines (before > after) | key order kept | quoting/flow nodes (before > after) | re-parses | semantic version as predicted |
| --- | --- | --- | --- | --- | --- | --- | --- |
| identity (decode, encode) | 0 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (unchanged) |
| set `config.command` of the first node | 2 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (matches typed edit) |
| replace `args` of the first node | 2 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (matches typed edit) |
| add a node (with a head comment) | 5 | 0 | 7 > 7 | yes | 14 > 15 | yes | yes (matches typed edit) |
| add an edge | 2 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (matches typed edit) |
| remove a leaf node | 23 | 0 | 7 > 6 | yes | 14 > 10 | yes | yes (matches typed edit) |
| move a node (create `metadata.layout`) | 4 | 0 | 7 > 7 | yes | 14 > 15 | yes | yes (unchanged) |
| move the same node again | 4 | 0 | 7 > 7 | yes | 14 > 15 | yes | yes (unchanged) |
| rename a node (key and references) | 4 | 0 | 7 > 7 | yes | 14 > 14 | yes | yes (matches typed edit) |

### experiments/yaml-fidelity/testdata/hostile.yaml

Unpatchable constructs (editor opens canvas read-only): anchor, merge key, alias

| operation | diff lines | comments lost | blank lines (before > after) | key order kept | quoting/flow nodes (before > after) | re-parses | semantic version as predicted |
| --- | --- | --- | --- | --- | --- | --- | --- |
| identity (decode, encode) | 15 | 0 | 8 > 7 | yes | 11 > 11 | yes | yes (unchanged) |
| set `config.command` of the first node | 15 | 0 | 8 > 7 | yes | 11 > 11 | yes | yes (matches typed edit) |
| replace `args` of the first node | 15 | 0 | 8 > 7 | yes | 11 > 9 | yes | NO (differs from typed edit) |
| add a node (with a head comment) | 20 | 0 | 8 > 7 | yes | 11 > 12 | yes | yes (matches typed edit) |
| add an edge | 17 | 0 | 8 > 7 | yes | 11 > 11 | yes | yes (matches typed edit) |
| remove a leaf node | 23 | 3 (# the only edge / # source / # a trailing comment inside ...) | 8 > 6 | yes | 11 > 10 | yes | yes (matches typed edit) |
| move a node (create `metadata.layout`) | 18 | 0 | 8 > 7 | yes | 11 > 12 | yes | yes (unchanged) |
| move the same node again | 18 | 0 | 8 > 7 | yes | 11 > 12 | yes | yes (unchanged) |
| rename a node (key and references) | 17 | 0 | 8 > 7 | yes | 11 > 11 | yes | yes (matches typed edit) |

### experiments/yaml-fidelity/testdata/long.yaml

Unpatchable constructs (editor opens canvas read-only): none

| operation | diff lines | comments lost | blank lines (before > after) | key order kept | quoting/flow nodes (before > after) | re-parses | semantic version as predicted |
| --- | --- | --- | --- | --- | --- | --- | --- |
| identity (decode, encode) | 4 | 0 | 1 > 2 | yes | 10 > 10 | yes | yes (unchanged) |
| set `config.command` of the first node | 6 | 0 | 1 > 2 | yes | 10 > 10 | yes | yes (matches typed edit) |
| replace `args` of the first node | 6 | 0 | 1 > 2 | yes | 10 > 7 | yes | yes (matches typed edit) |
| add a node (with a head comment) | 9 | 0 | 1 > 2 | yes | 10 > 11 | yes | yes (matches typed edit) |
| add an edge | 8 | 0 | 1 > 2 | yes | 10 > 9 | yes | yes (matches typed edit) |
| remove a leaf node | skipped: no leaf node | | | | | | |
| move a node (create `metadata.layout`) | 7 | 0 | 1 > 2 | yes | 10 > 11 | yes | yes (unchanged) |
| move the same node again | 7 | 0 | 1 > 2 | yes | 10 > 11 | yes | yes (unchanged) |
| rename a node (key and references) | 6 | 0 | 1 > 2 | yes | 10 > 10 | yes | yes (matches typed edit) |

## Findings

1. **Comments and key order survive every operation on every file.** The only comments that disappear are
   those attached to an element the edit deleted (the edge comments in `hostile.yaml` when the leaf node's
   edge is removed). That is the expected rule: deleting an element deletes its comments.
2. **The plain encoder loses every blank line.** Without the restoring pass, a no-op save of
   `hello-parallel.yaml` changes 6 lines (7 blank lines become 1) and `phase0-cli.yaml` 7 lines. With the
   pass, a no-op save of all three example graphs is **byte-identical (0 diff lines)**.
3. **Edits are local.** On the example graphs: setting a scalar or adding an edge changes 2 lines, adding a
   node 5 lines (the head comment, the key, `type`, `config` and `command`), a node move 4 lines (the new `metadata.layout`
   block, inserted before `inputs`), a rename 4 to 9 lines (the key and every reference), removing a leaf
   node the node's own lines plus its edges.
4. **Quoting and flow style are kept** for untouched nodes (counts equal on identity). Replacing the content
   of a flow list keeps it a flow list.
5. **CRLF and a BOM need explicit handling.** The plain encode of a CRLF file rewrites every line (110 diff
   lines on a 55-line file) and drops a BOM. Re-applying the original line ending and the BOM makes a no-op
   save byte-identical again.
6. **Anchors, aliases and merge keys are the one real hazard.** On `hostile.yaml`, editing the `args` of the
   node that owns the anchor also changes the node that merges it. A typed edit predicts that only the first
   node changes, so the semantic-version check **flags it ("NO (differs from typed edit)")**: the safety net
   works as designed. The editor should not rely on the net for this case; it should refuse canvas edits up
   front (the harness detects anchor, alias and merge key) and leave the YAML view editable.
7. **Normalisations that remain** (all semantically neutral, all re-parse to the same version):
   a document start `---` is dropped; a trailing comment's spacing becomes a single space; runs of blank
   lines collapse to one; a folded block scalar (`>`) is re-flowed into one line plus a blank line; a leading
   merge key is written `!!merge <<`. Trailing comments on keys stay on their line (`comments.yaml`), with
   spacing normalised to one space.
8. **A comment can move, not only disappear.** The "comments lost" column counts only comments that vanish.
   In `hostile.yaml`, the trailing comment on `config: &shell   # anchor...` moved onto the next line, to
   `command: printf # anchor...`, so it now describes a different element. It happens with an anchor on the
   key line, which is one more reason to refuse canvas edits on such files. Indentation is two spaces, which matches the repository's graphs.

## What ADR-A can promise

- A canvas or form edit changes only the lines it must. Comments, key order, quoting and flow style of
  untouched elements, blank lines before keys, the byte order mark and the line-ending convention are
  preserved. A save with no semantic change writes the identical bytes.
- Comments attached to a deleted element are deleted with it.
- Indentation is normalised to two spaces; document start markers, comment spacing, repeated blank lines and
  folded block scalars are normalised (listed above).
- Files containing anchors, aliases or merge keys are opened with a read-only canvas and an editable YAML
  view, with a message that says why.
- Every edit is checked: the result is re-parsed and its semantic version must equal the typed prediction
  (or the previous version for layout edits); otherwise nothing is written.
- Raw YAML text typed in the YAML view is saved as typed; nothing is reformatted behind the user's back.

## Limits of this spike

- It measures a handful of files and eight operations on one yaml library version
  (`go.yaml.in/yaml/v3` v3.0.5). It is evidence for the design, not a proof; the real operations engine
  needs its own table tests and a fidelity regression suite built from these fixtures.
- Blank-line restoration is keyed by key or item path. It is exact for these files; paths of renamed or
  newly added keys get no blank line from the original, and a comment block separated from its key by a
  blank line is treated as part of the key's block. The engine's tests must pin this behaviour.
- Moves of whole sections, multi-document files and tabs are not measured. A multi-document file is already
  rejected by `graph.Parse`.
- Comment position is not measured automatically, only presence; the one displacement found is described in
  finding 8 and was seen by reading the diff. The engine's fidelity suite must compare comment positions.
- Windows `CRLF` was measured on Linux with a converted fixture; mixed line endings are not handled.
