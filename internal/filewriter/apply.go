package filewriter

import (
	"context"
	"os"
	"path/filepath"
)

// tempPattern names staging files. They are siblings of their target so the
// rename that puts them into place stays on one filesystem, and the prefix
// makes leftovers obvious if a process is killed mid-batch.
const tempPattern = ".gw-tmp-*"

// Batch is the result of a successful Apply.
type Batch struct {
	// Changes is one entry per operation, in batch order.
	Changes []Change

	// entries carry prior and new content for diff generation.
	entries []batchEntry
}

type batchEntry struct {
	change  Change
	before  string
	after   string
	existed bool
}

// Apply writes a whole batch or nothing at all.
//
// Order of business: validate and classify every operation first, then
// apply writes, then deletes. Any failure rolls the worktree back to its
// prior state and removes every staging file.
func (w *Writer) Apply(ctx context.Context, ops []FileOp) (*Batch, error) {
	planned, err := w.plan(ops)
	if err != nil {
		return nil, err
	}

	// undos run in reverse on failure.
	var undos []func()
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			undos[i]()
		}
	}
	fail := func(err error) (*Batch, error) {
		rollback()
		return nil, err
	}

	// Writes first. Deletes are deferred so a failed write never leaves a
	// file deleted for a batch that did not complete.
	for _, p := range planned {
		if p.operation == OpDelete {
			continue
		}
		if err := ctx.Err(); err != nil {
			return fail(&WriteError{Reason: ReasonIOError, Path: p.rel, Detail: "cancelled", Err: err})
		}
		if err := w.applyWrite(p, &undos); err != nil {
			return fail(err)
		}
	}

	for _, p := range planned {
		if p.operation != OpDelete || p.skipped {
			continue
		}
		if err := ctx.Err(); err != nil {
			return fail(&WriteError{Reason: ReasonIOError, Path: p.rel, Detail: "cancelled", Err: err})
		}
		if err := applyDelete(p, &undos); err != nil {
			return fail(err)
		}
	}

	batch := &Batch{
		Changes: make([]Change, 0, len(planned)),
		entries: make([]batchEntry, 0, len(planned)),
	}
	for _, p := range planned {
		c := p.change()
		batch.Changes = append(batch.Changes, c)
		batch.entries = append(batch.entries, batchEntry{
			change:  c,
			before:  string(p.before),
			after:   p.content,
			existed: p.existed,
		})
	}
	return batch, nil
}

// applyWrite stages content next to the target and renames it into place,
// registering an undo that restores the prior state.
func (w *Writer) applyWrite(p plannedOp, undos *[]func()) error {
	dir := filepath.Dir(p.abs)

	created, err := mkdirAllTracked(dir)
	if err != nil {
		return &WriteError{Reason: ReasonIOError, Path: p.rel, Detail: "creating parent directories", Err: err}
	}
	if len(created) > 0 {
		*undos = append(*undos, func() {
			for i := len(created) - 1; i >= 0; i-- {
				_ = os.Remove(created[i]) // only succeeds while empty
			}
		})
	}

	tmp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return &WriteError{Reason: ReasonIOError, Path: p.rel, Detail: "creating staging file", Err: err}
	}
	tmpName := tmp.Name()
	cleanupTemp := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.WriteString(p.content); err != nil {
		_ = tmp.Close()
		cleanupTemp()
		return &WriteError{Reason: ReasonIOError, Path: p.rel, Detail: "writing staging file", Err: err}
	}
	if err := tmp.Chmod(p.mode); err != nil {
		_ = tmp.Close()
		cleanupTemp()
		return &WriteError{Reason: ReasonIOError, Path: p.rel, Detail: "setting file mode", Err: err}
	}
	if err := tmp.Close(); err != nil {
		cleanupTemp()
		return &WriteError{Reason: ReasonIOError, Path: p.rel, Detail: "closing staging file", Err: err}
	}

	if err := os.Rename(tmpName, p.abs); err != nil {
		cleanupTemp()
		return &WriteError{Reason: ReasonIOError, Path: p.rel, Detail: "installing staging file", Err: err}
	}

	prior, existed, mode := p.before, p.existed, p.mode
	abs := p.abs
	*undos = append(*undos, func() {
		if existed {
			_ = os.WriteFile(abs, prior, mode)
			return
		}
		_ = os.Remove(abs)
	})
	return nil
}

// applyDelete removes an existing target, registering an undo that restores
// it byte for byte.
func applyDelete(p plannedOp, undos *[]func()) error {
	if err := os.Remove(p.abs); err != nil {
		return &WriteError{Reason: ReasonIOError, Path: p.rel, Detail: "removing file", Err: err}
	}

	prior, mode, abs := p.before, p.mode, p.abs
	*undos = append(*undos, func() {
		_ = os.WriteFile(abs, prior, mode)
	})
	return nil
}
