package data

import "time"

// AlertView is one alert as the console page shows it.
type AlertView struct {
	Source string
	Key    string

	// Severity is "info", "warning" or "critical", which is also the CSS class that colours it.
	// SeverityLabel is the same for display.
	Severity      string
	SeverityLabel string

	Title   string
	Message string

	FirstRaised time.Time
	LastRaised  time.Time

	Acknowledged     bool
	AcknowledgedTime time.Time

	// Cleared is when the alert was cleared, or zero for one that's still active.
	Cleared time.Time
}

// Console is everything the console page shows.
type Console struct {
	// Active is ordered with the alerts that need attention first (see store.Store.Active).
	Active []AlertView
	// History is the cleared alerts, most recently cleared first.
	History []AlertView
	// Unacknowledged is how many of Active haven't been acknowledged.
	Unacknowledged int
}
