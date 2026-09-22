package filewriter

import (
	"fmt"
	"os"
	"path/filepath"
)

// defaultFileMode is used for files the batch creates. Existing files keep
// the mode they already had.
const defaultFileMode os.FileMode = 0o644

// Writer applies parsed operations to one worktree.
//
// All validation, classification, and mutation happen here, so the parser
// stays pure and no other package needs to know how paths resolve against
// a worktree.
type Writer struct {
	v *PathValidator
}

// NewWriter returns a Writer bound to a validator.
func NewWriter(v *PathValidator) *Writer {
	return &Writer{v: v}
}

// Validator exposes the underlying path validator.
func (w *Writer) Validator() *PathValidator { return w.v }

// plannedOp is one operation after validation and classification, holding
// everything Apply needs so no decision is revisited mid-batch.
type plannedOp struct {
	rel       string
	abs       string
	operation OpKind // create, modify, or delete — never write
	content   string
	mode      os.FileMode
	skipped   bool   // delete whose target was already absent
	before    []byte // prior content, for rollback and diff
	existed   bool
}

// plan validates and classifies every operation without mutating anything.
//
// It reads the prior content of files the batch will change, so a rollback
// never depends on a read that could fail after mutation has begun.
func (w *Writer) plan(ops []FileOp) ([]plannedOp, error) {
	planned := make([]plannedOp, 0, len(ops))
	claimed := make(map[string]string, len(ops)) // abs -> rel that claimed it

	for _, op := range ops {
		rel, abs, err := w.v.Resolve(op.Path)
		if err != nil {
			return nil, err
		}

		if prev, dup := claimed[abs]; dup {
			return nil, &WriteError{
				Reason: ReasonDuplicateTarget,
				Path:   rel,
				Detail: fmt.Sprintf("resolves to the same file as %q", prev),
			}
		}
		claimed[abs] = rel

		info, statErr := os.Lstat(abs)
		switch {
		case statErr != nil && !os.IsNotExist(statErr):
			return nil, &WriteError{
				Reason: ReasonIOError,
				Path:   rel,
				Detail: "target could not be inspected",
				Err:    statErr,
			}
		case statErr == nil && !info.Mode().IsRegular():
			return nil, &WriteError{
				Reason: ReasonNotRegularFile,
				Path:   rel,
				Detail: "target exists and is not a regular file",
			}
		}
		exists := statErr == nil

		p := plannedOp{rel: rel, abs: abs, existed: exists, mode: defaultFileMode}
		if exists {
			p.mode = info.Mode().Perm()
			prior, readErr := os.ReadFile(abs)
			if readErr != nil {
				return nil, &WriteError{
					Reason: ReasonIOError,
					Path:   rel,
					Detail: "existing content could not be read",
					Err:    readErr,
				}
			}
			p.before = prior
		}

		switch op.Kind {
		case OpDelete:
			p.operation = OpDelete
			// Deleting an absent file is not an error: the desired end
			// state already holds (AM-2).
			p.skipped = !exists
		default:
			p.content = op.Content
			if exists {
				p.operation = OpModify
			} else {
				p.operation = OpCreate
			}
		}

		planned = append(planned, p)
	}

	return planned, nil
}

// DryRun validates and classifies a batch and returns the changes it would
// make, without touching the filesystem.
func (w *Writer) DryRun(ops []FileOp) ([]Change, error) {
	planned, err := w.plan(ops)
	if err != nil {
		return nil, err
	}
	changes := make([]Change, 0, len(planned))
	for _, p := range planned {
		changes = append(changes, p.change())
	}
	return changes, nil
}

// change renders the public result of a planned operation.
func (p plannedOp) change() Change {
	c := Change{Path: p.rel, Operation: p.operation, Skipped: p.skipped}
	if p.operation != OpDelete {
		c.Bytes = len(p.content)
	}
	return c
}

// mkdirAllTracked creates dir and returns the directories it created,
// shallowest first, so a rollback can remove them in reverse.
func mkdirAllTracked(dir string) ([]string, error) {
	var missing []string
	probe := dir
	for {
		if _, err := os.Stat(probe); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		missing = append(missing, probe)
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}

	if len(missing) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	created := make([]string, 0, len(missing))
	for i := len(missing) - 1; i >= 0; i-- {
		created = append(created, missing[i])
	}
	return created, nil
}
