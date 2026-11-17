package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"agentplane/internal/runtime"
)

const fakeBaseSHA = "0123456789abcdef0123456789abcdef01234567"

type fakeHandle struct{ path string }

func (h *fakeHandle) Path() string { return h.path }

// fakeWorkspaces is a scripted runtime.WorkspaceManager. Fields are set before
// the run starts; the recorded calls are read after it returns.
type fakeWorkspaces struct {
	mu sync.Mutex

	root         string
	preflightErr error
	createErr    error
	snapshotErr  error
	integrateErr error
	removeErr    error
	malformedTip bool
	onRemove     func()
	conflict     map[string][]string // node ID -> conflicting paths

	preflights []string
	created    []string
	removed    []string
	snapshots  []string
	integrated []string
	tip        string
	order      []string
}

func (f *fakeWorkspaces) record(entry string) {
	f.mu.Lock()
	f.order = append(f.order, entry)
	f.mu.Unlock()
}

func (f *fakeWorkspaces) Preflight(_ context.Context, source string) (runtime.WorkspaceSource, error) {
	f.mu.Lock()
	f.preflights = append(f.preflights, source)
	f.mu.Unlock()
	if f.preflightErr != nil {
		return runtime.WorkspaceSource{}, f.preflightErr
	}
	return runtime.WorkspaceSource{Root: source, BaseSHA: fakeBaseSHA}, nil
}

func (f *fakeWorkspaces) Create(ctx context.Context, _ runtime.WorkspaceSource, _, nodeRunID string) (runtime.Workspace, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	path := f.root + "/wt-" + nodeRunID
	f.mu.Lock()
	f.created = append(f.created, path)
	f.mu.Unlock()
	f.record("create " + path)
	return &fakeHandle{path: path}, nil
}

func (f *fakeWorkspaces) Snapshot(_ context.Context, _ runtime.Workspace, ref string) (runtime.WorkspaceSnapshot, error) {
	if f.snapshotErr != nil {
		return runtime.WorkspaceSnapshot{}, f.snapshotErr
	}
	f.mu.Lock()
	f.snapshots = append(f.snapshots, ref)
	f.mu.Unlock()
	f.record("snapshot " + ref)
	return runtime.WorkspaceSnapshot{Commit: fakeCommit(len(f.snapshots)), ChangedPaths: 1}, nil
}

func (f *fakeWorkspaces) Integrate(_ context.Context, _ runtime.WorkspaceSource, _, unit string) (runtime.WorkspaceIntegration, error) {
	if f.integrateErr != nil {
		return runtime.WorkspaceIntegration{}, f.integrateErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.integrated = append(f.integrated, unit)
	f.order = append(f.order, "integrate "+unit)
	if f.tip == "" {
		f.tip = fakeBaseSHA
	}
	for node, paths := range f.conflict {
		if unit == f.nodeCommit(node) {
			return runtime.WorkspaceIntegration{Conflict: true, ConflictPaths: paths, TipCommit: f.tip}, nil
		}
	}
	f.tip = fakeCommit(100 + len(f.integrated))
	if f.malformedTip {
		return runtime.WorkspaceIntegration{TipCommit: "not-a-commit"}, nil
	}
	return runtime.WorkspaceIntegration{TipCommit: f.tip, ChangedPaths: 1}, nil
}

// nodeCommit maps a node ID to the commit its snapshot returned.
func (f *fakeWorkspaces) nodeCommit(node string) string {
	for i, ref := range f.snapshots {
		if ref == "refs/agentplane/fake/units/"+node {
			return fakeCommit(i + 1)
		}
	}
	return ""
}

func (f *fakeWorkspaces) Remove(_ context.Context, w runtime.Workspace) error {
	if f.onRemove != nil {
		f.onRemove()
	}
	if f.removeErr != nil {
		return f.removeErr
	}
	f.mu.Lock()
	f.removed = append(f.removed, w.Path())
	f.mu.Unlock()
	f.record("remove " + w.Path())
	return nil
}

func (*fakeWorkspaces) UnitRef(_, nodeID string) (string, error) {
	if nodeID == "" {
		return "", errors.New("empty node ID")
	}
	return "refs/agentplane/fake/units/" + nodeID, nil
}

func (*fakeWorkspaces) IntegratedRef(string) (string, error) {
	return "refs/agentplane/fake/integrated", nil
}

func fakeCommit(n int) string { return fmt.Sprintf("%040x", n) }
