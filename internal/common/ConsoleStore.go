package common

import "github.com/avanha/pmaas-plugin-alertconsole/data"

// ConsoleStore is how internal/http reaches back into the plugin, which owns the alerts. It's
// implemented by the plugin's adapter, which crosses onto the plugin's own goroutine.
type ConsoleStore interface {
	GetConsole() (data.Console, error)

	// Acknowledge acknowledges the active alert identified by source and key, and reports whether there
	// was one.
	Acknowledge(source string, key string) (bool, error)
}
