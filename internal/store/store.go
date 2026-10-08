// Package store keeps the alerts the console knows about: the ones that are active, and a bounded
// history of the ones that have been cleared. It's plain data with no concurrency of its own, owned by
// the plugin goroutine.
package store

import (
	"sort"
	"time"

	"github.com/avanha/pmaas-spi/alert"
)

// DefaultMaxHistory is how many cleared alerts are remembered unless told otherwise.
const DefaultMaxHistory = 50

// Alert is one alert, active or cleared.
type Alert struct {
	Source   string
	Key      string
	Severity alert.Severity
	Title    string
	Message  string

	// FirstRaised is when this alert became active. If it's cleared and later raised again, that's a new
	// alert with a new FirstRaised.
	FirstRaised time.Time
	// LastRaised is when it was most recently raised, which is more recent than FirstRaised for a
	// producer that raises it again each time it re-evaluates its condition.
	LastRaised time.Time

	Acknowledged     bool
	AcknowledgedTime time.Time

	// Cleared is when the alert was cleared. Zero while it's active.
	Cleared time.Time
}

// IsActive reports whether the alert is still active.
func (a Alert) IsActive() bool {
	return a.Cleared.IsZero()
}

type id struct {
	source string
	key    string
}

// Store holds alerts.
type Store struct {
	active     map[id]*Alert
	history    []Alert // oldest first
	maxHistory int
}

// New creates a Store remembering up to maxHistory cleared alerts. A maxHistory below one means
// DefaultMaxHistory.
func New(maxHistory int) *Store {
	if maxHistory < 1 {
		maxHistory = DefaultMaxHistory
	}

	return &Store{active: make(map[id]*Alert), maxHistory: maxHistory}
}

// Change is what raising an alert did.
type Change int

const (
	// ChangeIgnored: the event couldn't be used. One with no source or no key can't be told apart from any
	// other, so it's dropped.
	ChangeIgnored Change = iota
	// ChangeNew: the alert wasn't active, and now is.
	ChangeNew
	// ChangeEscalated: the alert was active, and its severity went up.
	ChangeEscalated
	// ChangeUpdated: the alert was active, and was raised again at the same or a lower severity. A
	// producer that re-evaluates its condition regularly does this each time.
	ChangeUpdated
)

// Raise makes the alert described by e active, or updates it if it already is, and reports what that
// changed.
//
// Raising an active alert again updates its severity, title and message. It stays acknowledged if the
// user has acknowledged it, unless the severity went up: acknowledging a warning isn't acknowledging
// the critical alert it became.
func (s *Store) Raise(e alert.RaisedEvent, now time.Time) Change {
	if e.Source == "" || e.Key == "" {
		return ChangeIgnored
	}

	severity := e.Severity.Normalize()
	title := e.Title

	if title == "" {
		title = e.Key
	}

	key := id{e.Source, e.Key}
	existing, isActive := s.active[key]

	if !isActive {
		s.active[key] = &Alert{
			Source:      e.Source,
			Key:         e.Key,
			Severity:    severity,
			Title:       title,
			Message:     e.Message,
			FirstRaised: now,
			LastRaised:  now,
		}

		return ChangeNew
	}

	change := ChangeUpdated

	if severity > existing.Severity {
		existing.Acknowledged = false
		existing.AcknowledgedTime = time.Time{}
		change = ChangeEscalated
	}

	existing.Severity = severity
	existing.Title = title
	existing.Message = e.Message
	existing.LastRaised = now

	return change
}

// Clear moves the active alert identified by source and key to the history. It reports whether there
// was one.
func (s *Store) Clear(source string, key string, now time.Time) bool {
	k := id{source, key}
	cleared, isActive := s.active[k]

	if !isActive {
		return false
	}

	delete(s.active, k)

	cleared.Cleared = now
	s.history = append(s.history, *cleared)

	if excess := len(s.history) - s.maxHistory; excess > 0 {
		s.history = append([]Alert(nil), s.history[excess:]...)
	}

	return true
}

// Acknowledge marks the active alert identified by source and key as seen. It reports whether there was
// one. Acknowledging an alert that's already acknowledged leaves the time of the first acknowledgement.
func (s *Store) Acknowledge(source string, key string, now time.Time) bool {
	existing, isActive := s.active[id{source, key}]

	if !isActive {
		return false
	}

	if !existing.Acknowledged {
		existing.Acknowledged = true
		existing.AcknowledgedTime = now
	}

	return true
}

// Active returns the active alerts, those that haven't been acknowledged first, then the most severe
// first, then the most recently raised first. The result is a copy.
func (s *Store) Active() []Alert {
	result := make([]Alert, 0, len(s.active))

	for _, a := range s.active {
		result = append(result, *a)
	}

	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]

		switch {
		case left.Acknowledged != right.Acknowledged:
			return !left.Acknowledged
		case left.Severity != right.Severity:
			return left.Severity > right.Severity
		case !left.FirstRaised.Equal(right.FirstRaised):
			return left.FirstRaised.After(right.FirstRaised)
		case left.Source != right.Source:
			return left.Source < right.Source
		default:
			return left.Key < right.Key
		}
	})

	return result
}

// History returns the cleared alerts, the most recently cleared first. The result is a copy.
func (s *Store) History() []Alert {
	result := make([]Alert, len(s.history))

	for i, a := range s.history {
		result[len(s.history)-1-i] = a
	}

	return result
}

// Unacknowledged returns how many active alerts haven't been acknowledged.
func (s *Store) Unacknowledged() int {
	count := 0

	for _, a := range s.active {
		if !a.Acknowledged {
			count++
		}
	}

	return count
}
