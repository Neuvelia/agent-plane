// Package edit applies editor operations to a graph file without rewriting it.
//
// graph.Marshal normalises a whole file (nodes alphabetical, comments and blank
// lines lost). The editor must change only what an edit changes, so edits are
// typed operations applied to the file's yaml.Node tree (ADR-0020). The package
// guarantees, and its fidelity tests pin:
//
//   - comments, key order, quoting and flow style of untouched elements are kept;
//   - a blank line above a key or sequence item (above the comment block directly
//     over it) is kept, keyed by the element's path;
//   - a UTF-8 byte order mark and the LF or CRLF convention are kept;
//   - a batch with no semantic change writes the identical bytes;
//   - comments attached to a deleted element go with it;
//   - indentation is two spaces.
//
// Accepted normalisations (semantically neutral): a document start marker is
// dropped, the space before a trailing comment becomes one space, runs of blank
// lines collapse to one, a folded block scalar is re-flowed.
//
// Files with anchors, aliases or merge keys are refused for operations (an edit
// could reach a second node through the anchor). Every operation also has a typed
// implementation on graph.Definition; after applying a batch the result is
// re-parsed and must equal the typed prediction, otherwise nothing is returned.
// The package never writes files and never validates graph semantics beyond what
// an operation needs; callers run graph.Validate on the result.
package edit
