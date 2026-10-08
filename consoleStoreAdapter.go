package alertconsole

import (
	"github.com/avanha/pmaas-plugin-alertconsole/data"
	"github.com/avanha/pmaas-plugin-alertconsole/internal/common"
	spi "github.com/avanha/pmaas-spi"
)

// Force implementation of common.ConsoleStore
var _ common.ConsoleStore = (*consoleStoreAdapter)(nil)

// consoleStoreAdapter is how HTTP requests reach the alerts. They come in on arbitrary goroutines, and the
// store belongs to the plugin's own, so each call runs there and waits for the answer.
type consoleStoreAdapter struct {
	parent *plugin
}

func (a *consoleStoreAdapter) GetConsole() (data.Console, error) {
	return spi.Exec(a.parent.container, a.parent.getConsole)
}

func (a *consoleStoreAdapter) Acknowledge(source string, key string) (bool, error) {
	return spi.Exec(a.parent.container, func() bool { return a.parent.acknowledge(source, key) })
}
