package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/avanha/pmaas-spi/alert"
)

var t0 = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func raised(source string, key string, severity alert.Severity) alert.RaisedEvent {
	return alert.RaisedEvent{Source: source, Key: key, Severity: severity, Title: "Title " + key, Message: "Message " + key}
}

func TestRaise_NewAlertBecomesActive(t *testing.T) {
	s := New(10)

	if got := s.Raise(raised("nest", "token", alert.SeverityWarning), t0); got != ChangeNew {
		t.Fatalf("got change %v, want ChangeNew", got)
	}

	active := s.Active()
	if len(active) != 1 {
		t.Fatalf("got %+v", active)
	}

	a := active[0]
	if a.Source != "nest" || a.Key != "token" || a.Severity != alert.SeverityWarning || a.Title != "Title token" ||
		a.Message != "Message token" || !a.FirstRaised.Equal(t0) || !a.LastRaised.Equal(t0) ||
		a.Acknowledged || !a.IsActive() {
		t.Fatalf("unexpected alert %+v", a)
	}
}

func TestRaise_IgnoresEventsThatCantBeIdentified(t *testing.T) {
	s := New(10)

	for _, e := range []alert.RaisedEvent{
		{Key: "k", Title: "no source"},
		{Source: "s", Title: "no key"},
		{},
	} {
		if got := s.Raise(e, t0); got != ChangeIgnored {
			t.Errorf("%+v: got change %v, want ChangeIgnored", e, got)
		}
	}

	if len(s.Active()) != 0 {
		t.Fatal("something was stored")
	}
}

func TestRaise_FillsInWhatTheProducerLeftOut(t *testing.T) {
	s := New(10)

	s.Raise(alert.RaisedEvent{Source: "s", Key: "k"}, t0)

	a := s.Active()[0]
	if a.Title != "k" || a.Severity != alert.SeverityInfo {
		t.Fatalf("expected the key as the title and info severity, got %+v", a)
	}

	s.Raise(alert.RaisedEvent{Source: "s", Key: "bad", Severity: 99}, t0)

	for _, a := range s.Active() {
		if a.Key == "bad" && a.Severity != alert.SeverityInfo {
			t.Fatalf("an undefined severity was kept: %+v", a)
		}
	}
}

func TestRaise_AgainUpdatesTheAlertInsteadOfAddingAnother(t *testing.T) {
	s := New(10)
	s.Raise(raised("nest", "token", alert.SeverityWarning), t0)

	later := t0.Add(time.Hour)
	update := alert.RaisedEvent{Source: "nest", Key: "token", Severity: alert.SeverityWarning, Title: "New title", Message: "New message"}
	if got := s.Raise(update, later); got != ChangeUpdated {
		t.Fatalf("got change %v, want ChangeUpdated", got)
	}

	active := s.Active()
	if len(active) != 1 {
		t.Fatalf("expected one alert, got %+v", active)
	}

	a := active[0]
	if a.Title != "New title" || a.Message != "New message" || !a.FirstRaised.Equal(t0) || !a.LastRaised.Equal(later) {
		t.Fatalf("unexpected alert %+v", a)
	}
}

func TestRaise_SameKeyFromDifferentSourcesAreDifferentAlerts(t *testing.T) {
	s := New(10)

	s.Raise(raised("a", "k", alert.SeverityInfo), t0)
	s.Raise(raised("b", "k", alert.SeverityInfo), t0)

	if got := len(s.Active()); got != 2 {
		t.Fatalf("got %d alerts", got)
	}

	s.Clear("a", "k", t0)

	if active := s.Active(); len(active) != 1 || active[0].Source != "b" {
		t.Fatalf("clearing one source's alert affected the other: %+v", active)
	}
}

func TestAcknowledge(t *testing.T) {
	s := New(10)
	s.Raise(raised("nest", "token", alert.SeverityCritical), t0)

	if s.Acknowledge("nest", "nothing", t0) || s.Acknowledge("other", "token", t0) {
		t.Fatal("acknowledged an alert that isn't there")
	}

	first := t0.Add(time.Minute)
	if !s.Acknowledge("nest", "token", first) {
		t.Fatal("expected the acknowledgement to be accepted")
	}

	// Acknowledging again keeps the time of the first.
	if !s.Acknowledge("nest", "token", first.Add(time.Hour)) {
		t.Fatal("expected a repeat acknowledgement to be accepted")
	}

	a := s.Active()[0]
	if !a.Acknowledged || !a.AcknowledgedTime.Equal(first) {
		t.Fatalf("unexpected alert %+v", a)
	}
}

func TestRaise_AcknowledgementSurvivesAnUpdateUnlessSeverityGoesUp(t *testing.T) {
	for _, c := range []struct {
		name            string
		first, second   alert.Severity
		wantAcknowledge bool
	}{
		{"same severity", alert.SeverityWarning, alert.SeverityWarning, true},
		{"severity goes down", alert.SeverityCritical, alert.SeverityWarning, true},
		{"severity goes up", alert.SeverityWarning, alert.SeverityCritical, false},
		{"info to critical", alert.SeverityInfo, alert.SeverityCritical, false},
	} {
		s := New(10)
		s.Raise(raised("s", "k", c.first), t0)
		s.Acknowledge("s", "k", t0)

		change := s.Raise(raised("s", "k", c.second), t0.Add(time.Hour))

		wantChange := ChangeUpdated
		if c.second > c.first {
			wantChange = ChangeEscalated
		}

		if change != wantChange {
			t.Errorf("%s: got change %v, want %v", c.name, change, wantChange)
		}

		a := s.Active()[0]
		if a.Acknowledged != c.wantAcknowledge || (!a.Acknowledged && !a.AcknowledgedTime.IsZero()) {
			t.Errorf("%s: unexpected alert %+v", c.name, a)
		}
	}
}

func TestClear_MovesTheAlertToTheHistory(t *testing.T) {
	s := New(10)
	s.Raise(raised("nest", "token", alert.SeverityWarning), t0)

	if s.Clear("nest", "nothing", t0) {
		t.Fatal("cleared an alert that isn't there")
	}

	cleared := t0.Add(time.Hour)
	if !s.Clear("nest", "token", cleared) {
		t.Fatal("expected the alert to be cleared")
	}

	if len(s.Active()) != 0 {
		t.Fatal("the alert is still active")
	}

	history := s.History()
	if len(history) != 1 || !history[0].Cleared.Equal(cleared) || history[0].IsActive() ||
		!history[0].FirstRaised.Equal(t0) || history[0].Title != "Title token" {
		t.Fatalf("unexpected history %+v", history)
	}

	// Clearing again does nothing.
	if s.Clear("nest", "token", cleared) || len(s.History()) != 1 {
		t.Fatal("clearing twice added to the history")
	}
}

func TestRaise_AfterClearingIsANewAlert(t *testing.T) {
	s := New(10)
	s.Raise(raised("s", "k", alert.SeverityWarning), t0)
	s.Acknowledge("s", "k", t0)
	s.Clear("s", "k", t0.Add(time.Hour))

	again := t0.Add(2 * time.Hour)
	s.Raise(raised("s", "k", alert.SeverityWarning), again)

	a := s.Active()[0]
	if !a.FirstRaised.Equal(again) || a.Acknowledged {
		t.Fatalf("expected a fresh, unacknowledged alert, got %+v", a)
	}

	if len(s.History()) != 1 {
		t.Fatal("the earlier occurrence should still be in the history")
	}
}

func TestHistory_IsNewestFirstAndBounded(t *testing.T) {
	s := New(3)

	for i := 1; i <= 5; i++ {
		key := fmt.Sprintf("k%d", i)
		s.Raise(raised("s", key, alert.SeverityInfo), t0)
		s.Clear("s", key, t0.Add(time.Duration(i)*time.Minute))
	}

	history := s.History()
	if len(history) != 3 || history[0].Key != "k5" || history[1].Key != "k4" || history[2].Key != "k3" {
		t.Fatalf("unexpected history %+v", history)
	}
}

func TestNew_DefaultsTheHistoryBound(t *testing.T) {
	s := New(0)

	for i := 0; i < DefaultMaxHistory+10; i++ {
		key := fmt.Sprintf("k%d", i)
		s.Raise(raised("s", key, alert.SeverityInfo), t0)
		s.Clear("s", key, t0)
	}

	if got := len(s.History()); got != DefaultMaxHistory {
		t.Fatalf("got %d, want %d", got, DefaultMaxHistory)
	}
}

func TestActive_OrderingNeedsAttentionFirst(t *testing.T) {
	s := New(10)

	// Raised in this order, so recency alone would give the opposite of the expected result.
	s.Raise(raised("s", "info-new", alert.SeverityInfo), t0.Add(5*time.Minute))
	s.Raise(raised("s", "critical-acked", alert.SeverityCritical), t0)
	s.Raise(raised("s", "warning-old", alert.SeverityWarning), t0.Add(1*time.Minute))
	s.Raise(raised("s", "warning-newer", alert.SeverityWarning), t0.Add(2*time.Minute))
	s.Raise(raised("s", "critical-new", alert.SeverityCritical), t0.Add(3*time.Minute))
	s.Acknowledge("s", "critical-acked", t0)

	want := []string{"critical-new", "warning-newer", "warning-old", "info-new", "critical-acked"}

	var got []string
	for _, a := range s.Active() {
		got = append(got, a.Key)
	}

	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestActive_IsStableForTies(t *testing.T) {
	s := New(10)

	for _, key := range []string{"c", "a", "b"} {
		s.Raise(raised("s", key, alert.SeverityInfo), t0)
	}

	for i := 0; i < 20; i++ {
		var got string
		for _, a := range s.Active() {
			got += a.Key
		}

		if got != "abc" {
			t.Fatalf("iteration %d: got %q, want a stable order", i, got)
		}
	}
}

func TestResultsAreCopies(t *testing.T) {
	s := New(10)
	s.Raise(raised("s", "k", alert.SeverityInfo), t0)
	s.Raise(raised("s", "gone", alert.SeverityInfo), t0)
	s.Clear("s", "gone", t0)

	s.Active()[0].Title = "changed"
	s.History()[0].Title = "changed"

	if s.Active()[0].Title == "changed" || s.History()[0].Title == "changed" {
		t.Fatal("changing a returned alert changed the store")
	}
}

func TestUnacknowledged(t *testing.T) {
	s := New(10)

	if s.Unacknowledged() != 0 {
		t.Fatal("expected none")
	}

	s.Raise(raised("s", "a", alert.SeverityInfo), t0)
	s.Raise(raised("s", "b", alert.SeverityInfo), t0)
	s.Acknowledge("s", "a", t0)

	if got := s.Unacknowledged(); got != 1 {
		t.Fatalf("got %d", got)
	}
}
