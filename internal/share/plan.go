package share

import "sort"

// Entry is a file's identity for sync purposes: size and modification time
// (Unix seconds). Second granularity tolerates FAT/qcow2/clock quirks.
type Entry struct {
	Size    int64
	ModUnix int64
}

// Snapshot maps a slash-separated relative path to its Entry.
type Snapshot map[string]Entry

// Op is a sync operation.
type Op int

const (
	OpUpload       Op = iota // copy host -> guest
	OpDownload               // copy guest -> host
	OpDeleteLocal            // remove on host (was deleted on guest)
	OpDeleteRemote           // remove on guest (was deleted on host)
	OpConflict               // both changed; resolved by last-writer-wins
)

func (o Op) String() string {
	switch o {
	case OpUpload:
		return "upload"
	case OpDownload:
		return "download"
	case OpDeleteLocal:
		return "delete-local"
	case OpDeleteRemote:
		return "delete-remote"
	case OpConflict:
		return "conflict"
	default:
		return "unknown"
	}
}

// Action is a single planned operation. For OpConflict, Resolution is the
// concrete op applied (OpUpload or OpDownload) after last-writer-wins.
type Action struct {
	Path       string
	Op         Op
	Resolution Op
}

// changed reports whether a differs from the manifest baseline b (or b absent).
func changed(a Entry, b Entry, ok bool, tol int64) bool {
	if !ok {
		return true
	}
	if a.Size != b.Size {
		return true
	}
	return abs(a.ModUnix-b.ModUnix) > tol
}

// Plan computes the set of actions to reconcile local and remote against the
// last-synced manifest. tol is the allowed mtime difference in seconds before
// two entries are considered different (handles cross-filesystem rounding).
//
// Decision matrix per path (over the union of local, remote, manifest):
//   - in both, content differs from baseline:
//     local-only changed   -> upload
//     remote-only changed  -> download
//     both changed         -> conflict (newer mtime wins; tie -> upload)
//   - local only:
//     in manifest          -> delete-local (deleted on remote)
//     not in manifest      -> upload (new local file)
//   - remote only:
//     in manifest          -> delete-remote (deleted on local)
//     not in manifest      -> download (new remote file)
//   - neither: nothing (manifest entry is pruned by the caller)
//
// The result is sorted by path for deterministic execution and testing.
func Plan(local, remote, manifest Snapshot, tol int64) []Action {
	if tol < 0 {
		tol = 0
	}
	seen := map[string]bool{}
	var actions []Action

	add := func(path string, op Op, res Op) {
		actions = append(actions, Action{Path: path, Op: op, Resolution: res})
	}

	for path, l := range local {
		seen[path] = true
		r, inRemote := remote[path]
		base, inBase := manifest[path]
		switch {
		case inRemote:
			lChanged := changed(l, base, inBase, tol)
			rChanged := changed(r, base, inBase, tol)
			switch {
			case lChanged && rChanged:
				if entriesEqual(l, r, tol) {
					// Both sides ended up identical; nothing to do.
					continue
				}
				if l.ModUnix >= r.ModUnix {
					add(path, OpConflict, OpUpload)
				} else {
					add(path, OpConflict, OpDownload)
				}
			case lChanged:
				add(path, OpUpload, OpUpload)
			case rChanged:
				add(path, OpDownload, OpDownload)
			}
		default: // local only
			if inBase {
				add(path, OpDeleteLocal, OpDeleteLocal)
			} else {
				add(path, OpUpload, OpUpload)
			}
		}
	}

	for path, r := range remote {
		if seen[path] {
			continue
		}
		_, inBase := manifest[path]
		if inBase {
			add(path, OpDeleteRemote, OpDeleteRemote)
		} else {
			_ = r
			add(path, OpDownload, OpDownload)
		}
	}

	sort.Slice(actions, func(i, j int) bool { return actions[i].Path < actions[j].Path })
	return actions
}

func entriesEqual(a, b Entry, tol int64) bool {
	return a.Size == b.Size && abs(a.ModUnix-b.ModUnix) <= tol
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
