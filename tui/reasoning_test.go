package main

import "testing"

// The regression the grouping could have caused, and the reason Reasoning
// carries yaml:",inline". effort: and thinking: were top-level keys before a
// third setting joined them, so they are already written at the top level of
// every config file that mentions them. load() decodes with KnownFields, so a
// key that quietly moved under a heading would not degrade, it would refuse to
// start the next time that file was read.
func TestTheOldTopLevelEffortAndThinkingKeysStillLoad(t *testing.T) {
	written(t, "backend: openrouter\neffort: high\nthinking: true\nmax_iterations: 12\n")

	config, err := settings(Config{})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if config.Effort != "high" {
		t.Errorf("effort = %q, want the file's top-level effort", config.Effort)
	}
	if !*config.Thinking {
		t.Error("thinking = false, want the file's top-level thinking")
	}
	if config.Backend != "openrouter" || *config.MaxIterations != 12 {
		t.Errorf("config = %+v, want the keys around the grouped ones untouched", config)
	}
}

// The budget is the setting the grouping was for, and it crosses the same
// three layers everything else does. Zero is its default and means no ceiling
// is asked for from here, which is why the deref in build() is safe.
func TestTheReasoningBudgetCrossesEveryLayer(t *testing.T) {
	if budget := *defaults().Budget; budget != 0 {
		t.Errorf("default budget = %d, want 0 meaning no ceiling from here", budget)
	}

	written(t, "reasoning_budget: 8000\n")
	config, err := settings(Config{})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if *config.Budget != 8000 {
		t.Errorf("reasoning budget = %d, want the file's 8000", *config.Budget)
	}

	t.Setenv(EnvPrefix+"REASONING_BUDGET", "12000")
	if config, err = settings(Config{}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if *config.Budget != 12000 {
		t.Errorf("reasoning budget = %d, want the environment to beat the file", *config.Budget)
	}
}
