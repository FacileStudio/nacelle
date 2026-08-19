package main

import "testing"

// A directory nobody has trusted yet is the ordinary state for a project
// nacelle has never run against, not a corrupt store.
func TestLoadTrustIsEmptyWithNoStoreYet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := loadTrust()
	if err != nil {
		t.Fatalf("loadTrust: %v", err)
	}
	if len(store) != 0 {
		t.Errorf("store = %+v, want empty with nothing ever saved", store)
	}
	if trusted(store, "/some/project/.agents/skills") {
		t.Error("an unmentioned directory read back as trusted")
	}
}

// The whole point of the store: a decision made once has to survive to the
// next run, which means a fresh process reading the file back, not just the
// in-memory map still holding it.
func TestATrustDecisionSurvivesASaveAndFreshLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := "/home/example/project/.agents/skills"

	store, err := loadTrust()
	if err != nil {
		t.Fatalf("loadTrust: %v", err)
	}
	trust(store, dir)
	if err := saveTrust(store); err != nil {
		t.Fatalf("saveTrust: %v", err)
	}

	reloaded, err := loadTrust()
	if err != nil {
		t.Fatalf("loadTrust after save: %v", err)
	}
	if !trusted(reloaded, dir) {
		t.Errorf("store = %+v, want %q trusted after a fresh load", reloaded, dir)
	}
}

// Trusting one directory must not trust its neighbour — the whole reason
// trust is keyed by canonical directory rather than granted globally.
func TestTrustingOneDirectoryDoesNotTrustAnother(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := map[string]trustRecord{}
	trust(store, "/home/example/trusted-project/.agents/skills")

	if trusted(store, "/home/example/other-project/.agents/skills") {
		t.Error("an unrelated project's directory read back as trusted")
	}
}
