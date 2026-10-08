package alertconsole

import (
	"fmt"
	"time"

	"github.com/avanha/pmaas-plugin-alertconsole/data"
	httphandler "github.com/avanha/pmaas-plugin-alertconsole/internal/http"
	"github.com/avanha/pmaas-plugin-alertconsole/internal/store"
	spi "github.com/avanha/pmaas-spi"
	"github.com/avanha/pmaas-spi/alert"
	"github.com/avanha/pmaas-spi/events"
)

type Plugin interface {
	spi.IPMAASPlugin
}

type plugin struct {
	config      PluginConfig
	container   spi.IPMAASContainer
	store       *store.Store
	httpHandler *httphandler.Handler

	receiverHandle     int
	receiverRegistered bool

	now func() time.Time
}

// Force implementation of spi.IPMAASPlugin
var _ spi.IPMAASPlugin = (*plugin)(nil)

func NewPlugin(config PluginConfig) Plugin {
	return &plugin{
		config:      config,
		httpHandler: httphandler.NewHandler(),
		now:         time.Now,
	}
}

func (p *plugin) ShortName() string {
	return "alertconsole"
}

func (p *plugin) Init(container spi.IPMAASContainer) {
	p.container = container
	p.store = store.New(p.config.MaxHistory)
	p.httpHandler.Init(container, &consoleStoreAdapter{parent: p})
}

func (p *plugin) Start() {
	fmt.Printf("%T Starting...\n", *p)

	handle, err := p.container.RegisterEventReceiver(alert.EventPredicate, p.onAlertEvent)
	if err != nil {
		panic(fmt.Sprintf("Unable to register for alert events: %v", err))
	}

	p.receiverHandle = handle
	p.receiverRegistered = true
}

func (p *plugin) Stop() chan func() {
	fmt.Printf("%T Stopping...\n", *p)

	if p.receiverRegistered {
		p.receiverRegistered = false

		if err := p.container.DeregisterEventReceiver(p.receiverHandle); err != nil {
			fmt.Printf("%T unable to deregister the alert event receiver: %v\n", *p, err)
		}
	}

	return p.container.ClosedCallbackChannel()
}

// onAlertEvent applies an alert event to the store. Runs on the plugin goroutine.
func (p *plugin) onAlertEvent(eventInfo *events.EventInfo) error {
	switch event := eventInfo.Event.(type) {
	case alert.RaisedEvent:
		switch p.store.Raise(event, p.now()) {
		case store.ChangeIgnored:
			return fmt.Errorf("ignoring an alert with no source or no key: %+v", event)
		case store.ChangeNew:
			fmt.Printf("%T alert raised: [%s] %s/%s: %s\n", *p, event.Severity, event.Source, event.Key, event.Title)
		case store.ChangeEscalated:
			fmt.Printf("%T alert escalated: [%s] %s/%s: %s\n", *p, event.Severity, event.Source, event.Key, event.Title)
		}
	case alert.ClearedEvent:
		if p.store.Clear(event.Source, event.Key, p.now()) {
			fmt.Printf("%T alert cleared: %s/%s\n", *p, event.Source, event.Key)
		}
	}

	return nil
}

// acknowledge acknowledges an alert on behalf of the user. Runs on the plugin goroutine.
func (p *plugin) acknowledge(source string, key string) bool {
	return p.store.Acknowledge(source, key, p.now())
}

// getConsole builds what the console page shows. Runs on the plugin goroutine.
func (p *plugin) getConsole() data.Console {
	active := p.store.Active()
	history := p.store.History()

	console := data.Console{
		Active:         make([]data.AlertView, len(active)),
		History:        make([]data.AlertView, len(history)),
		Unacknowledged: p.store.Unacknowledged(),
	}

	for i, a := range active {
		console.Active[i] = viewOf(a)
	}

	for i, a := range history {
		console.History[i] = viewOf(a)
	}

	return console
}

func viewOf(a store.Alert) data.AlertView {
	return data.AlertView{
		Source:           a.Source,
		Key:              a.Key,
		Severity:         a.Severity.String(),
		SeverityLabel:    severityLabel(a.Severity),
		Title:            a.Title,
		Message:          a.Message,
		FirstRaised:      a.FirstRaised,
		LastRaised:       a.LastRaised,
		Acknowledged:     a.Acknowledged,
		AcknowledgedTime: a.AcknowledgedTime,
		Cleared:          a.Cleared,
	}
}

func severityLabel(severity alert.Severity) string {
	switch severity.Normalize() {
	case alert.SeverityCritical:
		return "Critical"
	case alert.SeverityWarning:
		return "Warning"
	default:
		return "Info"
	}
}
