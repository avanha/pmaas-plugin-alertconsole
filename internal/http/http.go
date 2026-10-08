package http

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"reflect"
	"time"

	"github.com/avanha/pmaas-plugin-alertconsole/data"
	"github.com/avanha/pmaas-plugin-alertconsole/internal/common"
	"github.com/avanha/pmaas-spi"
)

//go:embed content/static content/templates
var contentFS embed.FS

var consoleTemplate = spi.TemplateInfo{
	Name: "alertconsole_console",
	FuncMap: template.FuncMap{
		"RelativeTime": relativeTime,
	},
	Paths:   []string{"templates/alertconsole_console.htmlt"},
	Styles:  []string{"css/alertconsole_console.css"},
	Scripts: []string{"js/alertconsole_console.js"},
}

// Handler owns the plugin's HTTP surface: the console page and the route that acknowledges an alert.
type Handler struct {
	container spi.IPMAASContainer
	store     common.ConsoleStore
}

func NewHandler() *Handler {
	return &Handler{}
}

func (h *Handler) Init(container spi.IPMAASContainer, store common.ConsoleStore) {
	h.container = container
	h.store = store

	container.ProvideContentFS(&contentFS, "content")
	container.EnableStaticContent("static")
	container.AddRouteWithOptions(
		"",
		h.handleHttpListRequest,
		&spi.HttpHandlerOptions{SupportsXsrfValidation: true, MenuLabel: "Alerts", MenuIcon: "bell"})
	container.AddJsonRoute(
		"acknowledge",
		func() any { return &acknowledgeRequest{} },
		h.handleHttpAcknowledgeRequest)
	container.RegisterEntityRenderer(
		reflect.TypeOf((*data.Console)(nil)).Elem(),
		h.consoleRendererFactory)
}

func (h *Handler) handleHttpListRequest(w http.ResponseWriter, r *http.Request) {
	console, err := h.store.GetConsole()

	if err != nil {
		fmt.Printf("alertconsole.http handleHttpListRequest: unable to retrieve alerts: %v\n", err)
		console = data.Console{}
	}

	h.container.RenderList(w, r, spi.RenderListOptions{Title: "Alerts"}, []any{&console})
}

type acknowledgeRequest struct {
	Source string
	Key    string
}

type acknowledgeResponse struct {
	// Acknowledged is false if there was no such active alert, e.g. because it was cleared in the meantime.
	Acknowledged bool
}

func (h *Handler) handleHttpAcknowledgeRequest(_ http.ResponseWriter, r *http.Request, request any) (any, error) {
	if r.Method != http.MethodPost {
		return nil, errors.New("method not allowed")
	}

	body, ok := request.(*acknowledgeRequest)

	if !ok || body.Source == "" || body.Key == "" {
		return nil, errors.New("a source and a key are required")
	}

	acknowledged, err := h.store.Acknowledge(body.Source, body.Key)

	if err != nil {
		return nil, err
	}

	return acknowledgeResponse{Acknowledged: acknowledged}, nil
}

func (h *Handler) consoleRendererFactory() (spi.EntityRenderer, error) {
	compiledTemplate, err := h.container.GetTemplate(&consoleTemplate)

	if err != nil {
		return spi.EntityRenderer{}, fmt.Errorf("unable to load alertconsole_console template: %v", err)
	}

	renderer := func(w io.Writer, entity any) error {
		console, ok := entity.(*data.Console)

		if !ok {
			return errors.New("item is not an instance of *Console")
		}

		if err := compiledTemplate.Instance.Execute(w, console); err != nil {
			return fmt.Errorf("unable to execute alertconsole_console template: %w", err)
		}

		return nil
	}

	return spi.EntityRenderer{
		StreamingRenderFunc: renderer,
		Styles:              compiledTemplate.Styles,
		Scripts:             compiledTemplate.Scripts,
	}, nil
}

// relativeTime says how long ago a time was, in its two most significant units: "5m ago", "2h 5m ago",
// "3d 4h ago". A time that hasn't happened, or is still in the future by a little (the clocks of a
// producer and this server needn't agree), reads as "just now".
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}

	elapsed := time.Since(t).Truncate(time.Minute)

	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh %dm ago", int(elapsed.Hours()), int(elapsed.Minutes())%60)
	default:
		return fmt.Sprintf("%dd %dh ago", int(elapsed.Hours())/24, int(elapsed.Hours())%24)
	}
}
