package share

import (
	"reflect"
	"testing"
)

func e(size, mod int64) Entry { return Entry{Size: size, ModUnix: mod} }

// actionMap reduces a plan to path->Op for order-independent comparison.
func actionMap(actions []Action) map[string]Op {
	m := make(map[string]Op, len(actions))
	for _, a := range actions {
		m[a.Path] = a.Op
	}
	return m
}

func TestPlan_NewFiles(t *testing.T) {
	local := Snapshot{"a.txt": e(1, 100)}
	remote := Snapshot{"b.txt": e(1, 100)}
	manifest := Snapshot{}
	got := actionMap(Plan(local, remote, manifest, 0))
	want := map[string]Op{"a.txt": OpUpload, "b.txt": OpDownload}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPlan_Deletes(t *testing.T) {
	// File existed at last sync, now gone on one side => delete the other side.
	manifest := Snapshot{"gone-remote.txt": e(1, 100), "gone-local.txt": e(1, 100)}
	local := Snapshot{"gone-remote.txt": e(1, 100)} // still local, removed remote
	remote := Snapshot{"gone-local.txt": e(1, 100)} // still remote, removed local
	got := actionMap(Plan(local, remote, manifest, 0))
	want := map[string]Op{"gone-remote.txt": OpDeleteLocal, "gone-local.txt": OpDeleteRemote}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPlan_OneSideChanged(t *testing.T) {
	manifest := Snapshot{"x": e(10, 100), "y": e(10, 100)}
	local := Snapshot{"x": e(20, 200), "y": e(10, 100)}  // x changed locally
	remote := Snapshot{"x": e(10, 100), "y": e(30, 300)} // y changed remotely
	got := actionMap(Plan(local, remote, manifest, 0))
	want := map[string]Op{"x": OpUpload, "y": OpDownload}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPlan_Unchanged(t *testing.T) {
	manifest := Snapshot{"x": e(10, 100)}
	local := Snapshot{"x": e(10, 100)}
	remote := Snapshot{"x": e(10, 100)}
	if got := Plan(local, remote, manifest, 0); len(got) != 0 {
		t.Fatalf("expected no actions, got %v", got)
	}
}

func TestPlan_ConflictNewerWins(t *testing.T) {
	manifest := Snapshot{"c": e(10, 100)}
	// both changed; local newer => upload wins
	local := Snapshot{"c": e(11, 300)}
	remote := Snapshot{"c": e(12, 200)}
	actions := Plan(local, remote, manifest, 0)
	if len(actions) != 1 || actions[0].Op != OpConflict || actions[0].Resolution != OpUpload {
		t.Fatalf("expected conflict resolved as upload, got %+v", actions)
	}

	// both changed; remote newer => download wins
	local2 := Snapshot{"c": e(11, 200)}
	remote2 := Snapshot{"c": e(12, 300)}
	actions2 := Plan(local2, remote2, manifest, 0)
	if len(actions2) != 1 || actions2[0].Resolution != OpDownload {
		t.Fatalf("expected conflict resolved as download, got %+v", actions2)
	}
}

func TestPlan_ConflictTiePrefersUpload(t *testing.T) {
	manifest := Snapshot{"c": e(10, 100)}
	local := Snapshot{"c": e(11, 200)}
	remote := Snapshot{"c": e(12, 200)} // same mtime, different size
	actions := Plan(local, remote, manifest, 0)
	if len(actions) != 1 || actions[0].Resolution != OpUpload {
		t.Fatalf("tie should prefer upload, got %+v", actions)
	}
}

func TestPlan_BothChangedToIdentical(t *testing.T) {
	// Both sides changed since baseline but ended up identical => no action.
	manifest := Snapshot{"c": e(10, 100)}
	local := Snapshot{"c": e(20, 200)}
	remote := Snapshot{"c": e(20, 200)}
	if got := Plan(local, remote, manifest, 0); len(got) != 0 {
		t.Fatalf("identical change should be a no-op, got %v", got)
	}
}

func TestPlan_Tolerance(t *testing.T) {
	// mtime differs by 1s but within tolerance and same size => unchanged.
	manifest := Snapshot{"x": e(10, 100)}
	local := Snapshot{"x": e(10, 101)}
	remote := Snapshot{"x": e(10, 100)}
	if got := Plan(local, remote, manifest, 2); len(got) != 0 {
		t.Fatalf("within tolerance should be a no-op, got %v", got)
	}
	// outside tolerance => upload
	if got := actionMap(Plan(local, remote, manifest, 0)); got["x"] != OpUpload {
		t.Fatalf("outside tolerance should upload, got %v", got)
	}
}

func TestPlan_NewBothSidesSameContent(t *testing.T) {
	// Brand-new file appears identically on both sides (no manifest): both
	// "changed vs baseline", identical => no-op (avoids needless transfer).
	local := Snapshot{"n": e(5, 50)}
	remote := Snapshot{"n": e(5, 50)}
	if got := Plan(local, remote, Snapshot{}, 0); len(got) != 0 {
		t.Fatalf("identical new file on both sides should be a no-op, got %v", got)
	}
}

func TestPlan_NewBothSidesDifferent(t *testing.T) {
	// Brand-new file appears differently on both sides => conflict.
	local := Snapshot{"n": e(5, 500)}
	remote := Snapshot{"n": e(9, 400)}
	actions := Plan(local, remote, Snapshot{}, 0)
	if len(actions) != 1 || actions[0].Op != OpConflict || actions[0].Resolution != OpUpload {
		t.Fatalf("expected conflict (upload, local newer), got %+v", actions)
	}
}

func TestPlan_Deterministic(t *testing.T) {
	local := Snapshot{"b": e(1, 1), "a": e(1, 1), "c": e(1, 1)}
	actions := Plan(local, Snapshot{}, Snapshot{}, 0)
	for i := 1; i < len(actions); i++ {
		if actions[i-1].Path > actions[i].Path {
			t.Fatalf("actions not sorted: %v", actions)
		}
	}
}

func TestPlan_NegativeToleranceClamped(t *testing.T) {
	manifest := Snapshot{"x": e(10, 100)}
	local := Snapshot{"x": e(10, 100)}
	remote := Snapshot{"x": e(10, 100)}
	if got := Plan(local, remote, manifest, -5); len(got) != 0 {
		t.Fatalf("negative tolerance should clamp to 0 and be a no-op, got %v", got)
	}
}
