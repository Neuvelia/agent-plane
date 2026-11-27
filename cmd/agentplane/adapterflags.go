package main

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"

	"agentplane/internal/agent"
	"agentplane/internal/agent/codex"
)

const (
	codexAdapterID  agent.AdapterID = "codex"
	claudeAdapterID agent.AdapterID = "claude"
)

// adapterFlags holds the explicit Agent adapter choice of `run`: a repeatable
// --adapter and one absolute binary flag per adapter (D7).
type adapterFlags struct {
	ids       inputFlags
	codexBin  string
	claudeBin string
}

func (a *adapterFlags) register(fs *flag.FlagSet) {
	fs.Var(&a.ids, "adapter", "explicit installed adapter (codex, claude); repeatable")
	fs.StringVar(&a.codexBin, "codex-bin", "", "absolute Codex CLI binary path")
	fs.StringVar(&a.claudeBin, "claude-bin", "", "absolute Claude Code CLI binary path")
}

func (a *adapterFlags) selected(id agent.AdapterID) bool {
	for _, value := range a.ids {
		if agent.AdapterID(value) == id {
			return true
		}
	}
	return false
}

// validate rejects an unknown or repeated adapter, a missing or relative binary
// for a selected adapter and a binary flag without its adapter.
func (a *adapterFlags) validate() error {
	seen := map[string]bool{}
	for _, value := range a.ids {
		if agent.AdapterID(value) != codexAdapterID && agent.AdapterID(value) != claudeAdapterID {
			return errors.New("--adapter must be codex or claude")
		}
		if seen[value] {
			return fmt.Errorf("--adapter %s was given more than once", value)
		}
		seen[value] = true
	}
	for _, binary := range []struct {
		id    agent.AdapterID
		flag  string
		value string
	}{{codexAdapterID, "codex-bin", a.codexBin}, {claudeAdapterID, "claude-bin", a.claudeBin}} {
		if err := a.validateBinary(binary.id, binary.flag, binary.value); err != nil {
			return err
		}
	}
	return nil
}

func (a *adapterFlags) validateBinary(id agent.AdapterID, flagName, value string) error {
	switch {
	case a.selected(id) && !filepath.IsAbs(value):
		return fmt.Errorf("use --adapter %s with --%s ABSOLUTE_PATH", id, flagName)
	case !a.selected(id) && value != "":
		return fmt.Errorf("--%s requires --adapter %s", flagName, id)
	}
	return nil
}

// adapters builds the explicit per-run adapter map, nil when none was chosen.
func (a *adapterFlags) adapters() map[agent.AdapterID]agent.AgentAdapter {
	if len(a.ids) == 0 {
		return nil
	}
	adapters := map[agent.AdapterID]agent.AgentAdapter{}
	if a.selected(codexAdapterID) {
		adapters[codexAdapterID] = codex.Adapter{Binary: a.codexBin}
	}
	if a.selected(claudeAdapterID) {
		adapters[claudeAdapterID] = newClaudeAdapter(a.claudeBin)
	}
	return adapters
}
