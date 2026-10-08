# pmaas-plugin-alertconsole

A PMAAS plugin that collects alerts from the server and from other plugins and shows them on one page,
**Alerts**, in the navigation menu.

## Raising an alert

A plugin never talks to this one. It broadcasts an event, using the helpers in `pmaas-spi/alert`, and
this plugin picks it up. That means a plugin can raise alerts whether or not the console is installed, and
a different plugin (say one that sends an email) can listen for the same events.

```go
import "github.com/avanha/pmaas-spi/alert"

// Raise it, or update it if it's already active. Raising it every time you re-check is fine.
alert.Raise(p.container, "nestthermostat", "refresh-token", alert.SeverityCritical,
	"Refresh token rejected",
	"Google no longer accepts the plugin's token. Use Get Token to authorize it again.")

// When the problem is gone.
alert.Clear(p.container, "nestthermostat", "refresh-token")
```

Alerts are state, not notifications. An alert is identified by its source (your plugin's short name) and
key, and stays active until you clear it. Raising it again updates it; if the severity goes up, it's
shown as new even if the user had already acknowledged the lower one.

Severities are `SeverityInfo`, `SeverityWarning` and `SeverityCritical`.

## The page

* **Active**: alerts that haven't been acknowledged come first, most severe first. Each has a coloured
  severity label, its title, who raised it, its message, and when it was raised.
* **Acknowledge** marks an alert as seen. It stays listed, dimmed, until whoever raised it clears it.
* **Recently Cleared**: the last few alerts that were cleared, newest first.

## Configuration

```go
conf := alertconsole.NewPluginConfig() // MaxHistory defaults to 50 cleared alerts
coreConfig.AddPlugin(alertconsole.NewPlugin(conf), config.PluginConfig{})
```

Alerts are kept in memory only, so they're gone after a restart. A plugin that re-checks its condition
regularly raises its alerts again, which brings back the ones that still apply.
