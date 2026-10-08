package alertconsole

import "github.com/avanha/pmaas-plugin-alertconsole/internal/store"

// PluginConfig configures the plugin.
type PluginConfig struct {
	// MaxHistory is how many cleared alerts are remembered and shown. Zero means the default of 50.
	MaxHistory int
}

func NewPluginConfig() PluginConfig {
	return PluginConfig{MaxHistory: store.DefaultMaxHistory}
}
