package alertconsole

import (
	"errors"
	"io/fs"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/avanha/pmaas-plugin-alertconsole/data"
	spi "github.com/avanha/pmaas-spi"
	"github.com/avanha/pmaas-spi/alert"
	"github.com/avanha/pmaas-spi/events"
)

// fakeContainer implements just what the plugin uses. Calling anything else panics.
type fakeContainer struct {
	spi.IPMAASContainer

	predicate      events.EventPredicate
	receiver       events.EventReceiver
	registerErr    error
	deregistered   []int
	nextHandle     int
	registerCalled int

	routes        map[string]*spi.HttpHandlerOptions
	jsonRoutes    []string
	rendererTypes []reflect.Type
	staticDir     string
}

func (c *fakeContainer) ProvideContentFS(fs.FS, string) {}

func (c *fakeContainer) EnableStaticContent(staticContentDir string) { c.staticDir = staticContentDir }

func (c *fakeContainer) AddRouteWithOptions(path string, _ http.HandlerFunc, options *spi.HttpHandlerOptions) {
	if c.routes == nil {
		c.routes = make(map[string]*spi.HttpHandlerOptions)
	}

	c.routes[path] = options
}

func (c *fakeContainer) AddJsonRoute(path string, _ spi.RequestObjectFactoryFunc, _ spi.JsonHandlerFunc) {
	c.jsonRoutes = append(c.jsonRoutes, path)
}

func (c *fakeContainer) RegisterEntityRenderer(entityType reflect.Type, _ spi.EntityRendererFactory) {
	c.rendererTypes = append(c.rendererTypes, entityType)
}

func (c *fakeContainer) RegisterEventReceiver(predicate events.EventPredicate, receiver events.EventReceiver) (int, error) {
	c.registerCalled++

	if c.registerErr != nil {
		return 0, c.registerErr
	}

	c.predicate = predicate
	c.receiver = receiver
	c.nextHandle++

	return c.nextHandle, nil
}

func (c *fakeContainer) DeregisterEventReceiver(handle int) error {
	c.deregistered = append(c.deregistered, handle)
	return nil
}

// EnqueueOnPluginGoRoutine runs f on another goroutine, as the real one does, so spi.Exec works from the
// test goroutine.
func (c *fakeContainer) EnqueueOnPluginGoRoutine(f func()) error {
	go f()
	return nil
}

func (c *fakeContainer) ClosedCallbackChannel() chan func() {
	ch := make(chan func())
	close(ch)

	return ch
}

func (c *fakeContainer) deliver(event any) error {
	return c.receiver(&events.EventInfo{Event: event})
}

func newStartedPlugin(t *testing.T, config PluginConfig) (*plugin, *fakeContainer) {
	t.Helper()

	container := &fakeContainer{}
	p := NewPlugin(config).(*plugin)
	p.Init(container)
	p.Start()

	return p, container
}

func raised(key string, severity alert.Severity) alert.RaisedEvent {
	return alert.RaisedEvent{Source: "nestthermostat", Key: key, Severity: severity, Title: "Title " + key, Message: "Message " + key}
}

func TestStart_RegistersForAlertEventsOnly(t *testing.T) {
	_, container := newStartedPlugin(t, NewPluginConfig())

	if container.registerCalled != 1 || container.receiver == nil {
		t.Fatal("expected the plugin to register an event receiver")
	}

	for name, c := range map[string]struct {
		event any
		want  bool
	}{
		"raised":  {alert.RaisedEvent{}, true},
		"cleared": {alert.ClearedEvent{}, true},
		"other":   {events.EntityRegisteredEvent{}, false},
	} {
		if got := container.predicate(&events.EventInfo{Event: c.event}); got != c.want {
			t.Errorf("%s: predicate = %v, want %v", name, got, c.want)
		}
	}
}

func TestStart_PanicsIfItCannotRegister(t *testing.T) {
	container := &fakeContainer{registerErr: errors.New("nope")}
	p := NewPlugin(NewPluginConfig()).(*plugin)
	p.Init(container)

	defer func() {
		if recover() == nil {
			t.Fatal("a plugin that can't receive alerts is useless, and shouldn't pretend to run")
		}
	}()

	p.Start()
}

func TestRaisedEvent_AppearsOnTheConsole(t *testing.T) {
	p, container := newStartedPlugin(t, NewPluginConfig())
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }

	if err := container.deliver(raised("token", alert.SeverityCritical)); err != nil {
		t.Fatal(err)
	}

	console := p.getConsole()
	if len(console.Active) != 1 || console.Unacknowledged != 1 || len(console.History) != 0 {
		t.Fatalf("unexpected console %+v", console)
	}

	a := console.Active[0]
	if a.Source != "nestthermostat" || a.Key != "token" || a.Severity != "critical" || a.SeverityLabel != "Critical" ||
		a.Title != "Title token" || a.Message != "Message token" || !a.FirstRaised.Equal(now) || a.Acknowledged {
		t.Fatalf("unexpected alert %+v", a)
	}
}

func TestSeverityLabels(t *testing.T) {
	for severity, want := range map[alert.Severity]string{
		alert.SeverityInfo: "Info", alert.SeverityWarning: "Warning", alert.SeverityCritical: "Critical", 0: "Info", 42: "Info",
	} {
		if got := severityLabel(severity); got != want {
			t.Errorf("severityLabel(%d) = %q, want %q", severity, got, want)
		}
	}
}

func TestRaisingAgainUpdatesAndEscalationUnacknowledges(t *testing.T) {
	p, container := newStartedPlugin(t, NewPluginConfig())

	_ = container.deliver(raised("token", alert.SeverityWarning))
	p.acknowledge("nestthermostat", "token")

	// The producer re-checks and raises it again, unchanged: still one alert, still acknowledged.
	_ = container.deliver(raised("token", alert.SeverityWarning))

	console := p.getConsole()
	if len(console.Active) != 1 || !console.Active[0].Acknowledged || console.Unacknowledged != 0 {
		t.Fatalf("unexpected console %+v", console)
	}

	// It gets worse.
	_ = container.deliver(raised("token", alert.SeverityCritical))

	console = p.getConsole()
	if len(console.Active) != 1 || console.Active[0].Acknowledged || console.Unacknowledged != 1 ||
		console.Active[0].Severity != "critical" {
		t.Fatalf("expected the escalated alert to need acknowledging again, got %+v", console)
	}
}

func TestClearedEvent_MovesTheAlertToTheHistory(t *testing.T) {
	p, container := newStartedPlugin(t, NewPluginConfig())

	_ = container.deliver(raised("token", alert.SeverityWarning))
	_ = container.deliver(alert.ClearedEvent{Source: "nestthermostat", Key: "token"})

	console := p.getConsole()
	if len(console.Active) != 0 || len(console.History) != 1 || console.History[0].Key != "token" ||
		console.History[0].Cleared.IsZero() {
		t.Fatalf("unexpected console %+v", console)
	}

	// Clearing what isn't there is harmless, and isn't an error: producers clear unconditionally.
	if err := container.deliver(alert.ClearedEvent{Source: "nestthermostat", Key: "never-raised"}); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestRaisedEventWithoutASourceOrKeyIsReported(t *testing.T) {
	p, container := newStartedPlugin(t, NewPluginConfig())

	if err := container.deliver(alert.RaisedEvent{Title: "anonymous"}); err == nil {
		t.Fatal("expected an error for an alert that can't be identified")
	}

	if len(p.getConsole().Active) != 0 {
		t.Fatal("an unidentifiable alert was stored")
	}
}

func TestAcknowledgeThroughTheAdapter(t *testing.T) {
	p, container := newStartedPlugin(t, NewPluginConfig())
	_ = container.deliver(raised("token", alert.SeverityWarning))

	adapter := &consoleStoreAdapter{parent: p}

	found, err := adapter.Acknowledge("nestthermostat", "token")
	if err != nil || !found {
		t.Fatalf("got %v, %v", found, err)
	}

	found, err = adapter.Acknowledge("nestthermostat", "nothing")
	if err != nil || found {
		t.Fatalf("acknowledging a missing alert: got %v, %v", found, err)
	}

	console, err := adapter.GetConsole()
	if err != nil || len(console.Active) != 1 || !console.Active[0].Acknowledged {
		t.Fatalf("got %+v, %v", console, err)
	}
}

func TestMaxHistoryComesFromTheConfiguration(t *testing.T) {
	config := NewPluginConfig()
	config.MaxHistory = 2

	p, container := newStartedPlugin(t, config)

	for _, key := range []string{"a", "b", "c"} {
		_ = container.deliver(raised(key, alert.SeverityInfo))
		_ = container.deliver(alert.ClearedEvent{Source: "nestthermostat", Key: key})
	}

	history := p.getConsole().History
	if len(history) != 2 || history[0].Key != "c" || history[1].Key != "b" {
		t.Fatalf("unexpected history %+v", history)
	}
}

func TestStop_DeregistersTheReceiverOnceAndToleratesNeverHavingStarted(t *testing.T) {
	p, container := newStartedPlugin(t, NewPluginConfig())

	<-p.Stop()
	<-p.Stop()

	if len(container.deregistered) != 1 || container.deregistered[0] != 1 {
		t.Fatalf("unexpected deregistrations %v", container.deregistered)
	}

	neverStarted := NewPlugin(NewPluginConfig()).(*plugin)
	neverStarted.Init(&fakeContainer{})
	<-neverStarted.Stop()
}

func TestShortName(t *testing.T) {
	if got := NewPlugin(NewPluginConfig()).ShortName(); got != "alertconsole" {
		t.Fatalf("got %q", got)
	}
}

func TestInit_RegistersThePageTheAcknowledgeRouteAndTheRenderer(t *testing.T) {
	container := &fakeContainer{}
	p := NewPlugin(NewPluginConfig()).(*plugin)
	p.Init(container)

	list, ok := container.routes[""]
	if !ok || list.MenuLabel != "Alerts" || list.MenuIcon != "bell" || !list.SupportsXsrfValidation {
		t.Fatalf("unexpected list route %+v", list)
	}

	if len(container.jsonRoutes) != 1 || container.jsonRoutes[0] != "acknowledge" {
		t.Fatalf("unexpected JSON routes %v", container.jsonRoutes)
	}

	if len(container.rendererTypes) != 1 || container.rendererTypes[0] != reflect.TypeFor[data.Console]() {
		t.Fatalf("unexpected renderers %v", container.rendererTypes)
	}

	if container.staticDir != "static" {
		t.Fatalf("static content not enabled: %q", container.staticDir)
	}
}
