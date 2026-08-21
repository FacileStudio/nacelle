package main

import (
	"strings"
	"testing"
)

// ptr is how a test names a setting whose empty value means something.
func ptr(s string) *string { return &s }

// The absence of a default here is the setting, not an oversight. nacelle is
// public, so any instance shipped as a default would send a stranger's
// queries to somebody else's machine and leave them in that operator's logs.
func TestSearchIsOffUntilAnInstanceIsNamed(t *testing.T) {
	written(t, "")

	config, err := settings(Config{})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if *config.Search != "" {
		t.Errorf("search = %q, want no instance chosen on anyone's behalf", *config.Search)
	}
}

func TestSearchComesFromTheFileAndEveryLayerAboveOutranksIt(t *testing.T) {
	written(t, "search: https://from-the-file.example\n")

	config, err := settings(Config{})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if *config.Search != "https://from-the-file.example" {
		t.Errorf("search = %q, want the file's instance", *config.Search)
	}

	t.Setenv(EnvPrefix+"SEARCH", "https://from-the-environment.example")
	if config, err = settings(Config{}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if *config.Search != "https://from-the-environment.example" {
		t.Errorf("search = %q, want the environment to win", *config.Search)
	}

	if config, err = settings(Config{Web: Web{Search: ptr("https://from-the-flag.example")}}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if *config.Search != "https://from-the-flag.example" {
		t.Errorf("search = %q, want the flag to win", *config.Search)
	}
}

// A misconfigured endpoint has to stop the client rather than quietly leaving
// the tool unmounted: search silently missing looks exactly like a model that
// has decided not to search, and nothing on screen would connect that to a
// typo in ~/.nacelle.yml.
func TestAnUnusableSearchEndpointStopsTheClient(t *testing.T) {
	config := defaults()
	config.Root = t.TempDir()
	config.Search = ptr("furet.example/search")

	set, _, err := localTools(config)
	if set != nil {
		t.Cleanup(func() {
			if err := set.Close(); err != nil {
				t.Errorf("closing the tool set: %v", err)
			}
		})
	}
	if err == nil {
		t.Fatal("localTools accepted an endpoint with no scheme, want a refusal before the client starts")
	}
	if !strings.Contains(err.Error(), "furet.example") {
		t.Errorf("error = %q, want it to quote the endpoint that is wrong", err)
	}
}

// The bug this pointer exists for. With a plain string, `-search ""` against a
// configured instance was indistinguishable from not passing -search at all,
// so the file's endpoint survived and the run searched anyway — no error, no
// message, and a flag that looked like the way to turn search off.
func TestSearchCanBeTurnedOffForOneRunByAnEmptyFlag(t *testing.T) {
	written(t, "search: https://from-the-file.example\n")

	config, err := settings(Config{Web: Web{Search: ptr("")}})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if *config.Search != "" {
		t.Errorf("search = %q, want an explicitly empty flag to turn it off", *config.Search)
	}
}

// The same distinction one layer down: an absent variable must fall through to
// the file, while a present but empty one must override it.
func TestAnAbsentSearchVariableFallsThroughButAnEmptyOneOverrides(t *testing.T) {
	written(t, "search: https://from-the-file.example\n")

	config, err := settings(Config{})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if *config.Search != "https://from-the-file.example" {
		t.Errorf("search = %q, want an unset variable to leave the file alone", *config.Search)
	}

	t.Setenv(EnvPrefix+"SEARCH", "")
	if config, err = settings(Config{}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if *config.Search != "" {
		t.Errorf("search = %q, want an empty variable to turn it off", *config.Search)
	}
}

// Fetch is the one web setting that is on unless someone says otherwise, so
// the test that matters is that a file can turn it off — the same distinction
// as search, in the opposite direction.
func TestFetchIsOnByDefaultAndTheFileCanTurnItOff(t *testing.T) {
	written(t, "")

	config, err := settings(Config{})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if !*config.Fetch {
		t.Error("fetch = false, want reading a page to be available without asking")
	}

	written(t, "fetch: false\n")
	if config, err = settings(Config{}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if *config.Fetch {
		t.Error("fetch = true, want the file able to turn it off")
	}
}

func TestTheBannerNamesFetchOnlyWhenItIsOff(t *testing.T) {
	on, off := true, false

	quiet := banner(&answeringStub{}, asSettled(Config{Root: ".", Web: Web{Fetch: &on}}), loaded{}, connected{})
	if strings.Contains(quiet, "fetch") {
		t.Errorf("banner = %q, want nothing said about fetch when it is on", quiet)
	}

	loud := banner(&answeringStub{}, asSettled(Config{Root: ".", Web: Web{Fetch: &off}}), loaded{}, connected{})
	if !strings.Contains(loud, "fetch off") {
		t.Errorf("banner = %q, want the reason a page cannot be read on screen", loud)
	}
}
