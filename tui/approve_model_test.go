package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// An approvalRequest arriving through Update is what program.Send delivers
// in the real client — this is the seam between the two goroutines, and the
// one place a bug would mean a question nobody ever sees.
func TestAnApprovalRequestShowsInTheStatusLine(t *testing.T) {
	m := sized()
	decision := make(chan approvalDecision, 1)

	m.Update(approvalRequest{name: "search_content", input: []byte(`{"pattern":"x"}`), decision: decision})

	if m.run.pending == nil {
		t.Fatal("the request did not set run.pending")
	}
	status := m.status()
	if !strings.Contains(status, "search_content") {
		t.Errorf("status = %q, want the tool's name", status)
	}
	if !strings.Contains(status, "y = once") {
		t.Errorf("status = %q, want the key hint", status)
	}
}

// y, a and n are the only keys that can answer a pending approval — this
// checks the ordinary one: a single yes.
func TestPressingYApprovesOnce(t *testing.T) {
	m := sized()
	decision := make(chan approvalDecision, 1)
	m.run.pending = &approvalRequest{name: "search", decision: decision}

	handled, _ := m.key(tea.KeyPressMsg{Code: 'y'})

	if !handled {
		t.Fatal("y was not treated as answering the pending approval")
	}
	if m.run.pending != nil {
		t.Error("run.pending was not cleared after a decision")
	}
	select {
	case d := <-decision:
		if d != allowedOnce {
			t.Errorf("decision = %v, want allowedOnce", d)
		}
	default:
		t.Fatal("no decision was sent")
	}
}

func TestPressingAApprovesForTheSession(t *testing.T) {
	m := sized()
	decision := make(chan approvalDecision, 1)
	m.run.pending = &approvalRequest{name: "search", decision: decision}

	m.key(tea.KeyPressMsg{Code: 'a'})

	if d := <-decision; d != allowedForSession {
		t.Errorf("decision = %v, want allowedForSession", d)
	}
}

func TestPressingNDenies(t *testing.T) {
	m := sized()
	decision := make(chan approvalDecision, 1)
	m.run.pending = &approvalRequest{name: "search", decision: decision}

	m.key(tea.KeyPressMsg{Code: 'n'})

	if d := <-decision; d != denied {
		t.Errorf("decision = %v, want denied", d)
	}
}

// Any other key must not leak into the prompt or scroll the transcript
// while a decision is open — it also must not accidentally answer with a
// zero-value decision, which is denied and would silently refuse a call the
// reader never meant to.
func TestAnyOtherKeyIsSwallowedWhilePending(t *testing.T) {
	m := sized()
	decision := make(chan approvalDecision, 1)
	m.run.pending = &approvalRequest{name: "search", decision: decision}

	handled, _ := m.key(tea.KeyPressMsg{Code: 'x'})

	if !handled {
		t.Error("an unrelated key was passed through while an approval was pending")
	}
	if m.run.pending == nil {
		t.Error("the pending approval was cleared by a key that was not a decision")
	}
	select {
	case d := <-decision:
		t.Errorf("a decision (%v) was sent for a key that answered nothing", d)
	default:
	}
}

// Ctrl+C has to keep working even mid-question — a person is not otherwise
// locked into answering a prompt they no longer want to see, and the run
// being cancelled is itself the answer: nothing further should be asked
// about a run that just ended.
func TestCtrlCClearsAPendingApprovalAndCancelsTheRun(t *testing.T) {
	m := sized()
	cancelled := false
	m.run.busy = true
	m.run.cancel = func() { cancelled = true }
	decision := make(chan approvalDecision, 1)
	m.run.pending = &approvalRequest{name: "search", decision: decision}

	handled, cmd := m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	if !handled || cmd != nil {
		t.Fatalf("ctrl+c = %v, %v; want the run cancelled and the session kept", handled, cmd)
	}
	if !cancelled {
		t.Error("the run was not cancelled")
	}
	if m.run.pending != nil {
		t.Error("run.pending survived a cancel — the status line would keep asking a dead question")
	}
}
