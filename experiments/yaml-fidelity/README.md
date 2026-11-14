# yaml-fidelity

Measurement tool for the E0 spike of the graph editor plan. It edits graph files through the `yaml.Node`
tree and reports what is preserved. Not product code; nothing imports it.

```bash
go run ./experiments/yaml-fidelity           # example graphs + testdata/*.yaml, markdown report
NO_BLANKS=1 go run ./experiments/yaml-fidelity   # plain encoder, without the blank-line pass
SHOW_DIFF=1 go run ./experiments/yaml-fidelity experiments/yaml-fidelity/testdata/long.yaml
```

Needs the `diff` binary. Results and conclusions: `docs/validation/E0-yaml-fidelity.md`.
