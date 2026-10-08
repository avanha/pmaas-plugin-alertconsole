package http

import (
	"bytes"
	"errors"
	"net/http"
	"path"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/avanha/pmaas-plugin-alertconsole/data"
)

// renderConsole renders the template the way PMAAS does. Its template engine plugin uses text/template,
// which doesn't escape anything, so a template has to do that itself: rendering with html/template here
// would escape automatically and hide a template that doesn't.
func renderConsole(t *testing.T, console data.Console) string {
	t.Helper()

	name := path.Base(consoleTemplate.Paths[0])
	tmpl, err := template.New(name).Funcs(consoleTemplate.FuncMap).ParseFS(contentFS, "content/"+consoleTemplate.Paths[0])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, name, &console); err != nil {
		t.Fatalf("execute: %v", err)
	}

	return out.String()
}

func view(severity string, label string, title string) data.AlertView {
	return data.AlertView{
		Source: "nestthermostat", Key: "k-" + title, Severity: severity, SeverityLabel: label,
		Title: title, Message: "Message for " + title,
		FirstRaised: time.Now().Add(-2 * time.Hour), LastRaised: time.Now(),
	}
}

func TestConsoleTemplate_Empty(t *testing.T) {
	out := renderConsole(t, data.Console{})

	for _, want := range []string{"No active alerts", "Nothing has been cleared yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}

	for _, unwanted := range []string{"ack-button", "severity-badge"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("unexpectedly contains %q", unwanted)
		}
	}
}

func TestConsoleTemplate_ActiveAlertsShowSeverityBadgesAndAcknowledgeButtons(t *testing.T) {
	acknowledged := view("warning", "Warning", "Seen")
	acknowledged.Acknowledged = true
	acknowledged.AcknowledgedTime = time.Now().Add(-10 * time.Minute)

	out := renderConsole(t, data.Console{
		Active:         []data.AlertView{view("critical", "Critical", "Burning"), acknowledged, view("info", "Info", "FYI")},
		Unacknowledged: 2,
	})

	for _, want := range []string{
		`severity-badge critical">Critical`,
		`severity-badge warning">Warning`,
		`severity-badge info">Info`,
		"Burning", "Message for Burning", "nestthermostat",
		"(3, 2 unacknowledged)",
		`data-source="nestthermostat" data-key="k-Burning"`,
		"Acknowledged 10m ago",
		`alert warning acknowledged`,
		"(2h 0m ago)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	// Only the two that haven't been acknowledged get a button.
	if got := strings.Count(out, `class="ack-button"`); got != 2 {
		t.Errorf("expected 2 acknowledge buttons, got %d", got)
	}
}

func TestConsoleTemplate_NoUnacknowledgedAlertsOmitsThatFromTheSummary(t *testing.T) {
	a := view("info", "Info", "Seen")
	a.Acknowledged = true
	a.AcknowledgedTime = time.Now()

	out := renderConsole(t, data.Console{Active: []data.AlertView{a}})

	if !strings.Contains(out, "(1)") || strings.Contains(out, "unacknowledged") {
		t.Errorf("unexpected summary in:\n%s", out)
	}
}

func TestConsoleTemplate_AnAlertWithNoMessageOmitsTheMessageLine(t *testing.T) {
	a := view("info", "Info", "Terse")
	a.Message = ""

	if out := renderConsole(t, data.Console{Active: []data.AlertView{a}}); strings.Contains(out, `class="message"`) {
		t.Error("an empty message was rendered")
	}
}

func TestConsoleTemplate_History(t *testing.T) {
	a := view("critical", "Critical", "Was burning")
	a.Cleared = time.Now().Add(-30 * time.Minute)

	out := renderConsole(t, data.Console{History: []data.AlertView{a}})

	for _, want := range []string{"Recently Cleared", "Was burning", "cleared ", "(30m ago)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	if strings.Contains(out, "Nothing has been cleared yet") || strings.Contains(out, "ack-button") {
		t.Error("history rendered like an empty list, or with acknowledge buttons")
	}
}

// Alert text comes from other plugins, and some of it (an error message, a device name) may be partly
// under the control of something outside. It must reach the page as text.
func TestConsoleTemplate_EscapesAlertText(t *testing.T) {
	a := view("info", "Info", `<script>alert("title")</script>`)
	a.Message = `<img src=x onerror=alert(1)>`
	a.Source = `"><b>source</b>`

	// The key goes into an attribute, where a quote is what lets an attacker out of it.
	a.Key = `x" onclick="alert(1)`

	out := renderConsole(t, data.Console{Active: []data.AlertView{a}})

	for _, dangerous := range []string{"<script>", "<img src=x", "<b>source", `onclick="alert(1)"`} {
		if strings.Contains(out, dangerous) {
			t.Errorf("unescaped %q in:\n%s", dangerous, out)
		}
	}
}

type fakeStore struct {
	acknowledged [][2]string
	result       bool
	err          error
}

func (s *fakeStore) GetConsole() (data.Console, error) { return data.Console{}, s.err }

func (s *fakeStore) Acknowledge(source string, key string) (bool, error) {
	s.acknowledged = append(s.acknowledged, [2]string{source, key})
	return s.result, s.err
}

func TestAcknowledgeRequest(t *testing.T) {
	post := &http.Request{Method: http.MethodPost}

	t.Run("acknowledges and says whether it was there", func(t *testing.T) {
		for _, found := range []bool{true, false} {
			store := &fakeStore{result: found}
			h := &Handler{store: store}

			response, err := h.handleHttpAcknowledgeRequest(nil, post, &acknowledgeRequest{Source: "nest", Key: "token"})
			if err != nil {
				t.Fatal(err)
			}

			if got := response.(acknowledgeResponse).Acknowledged; got != found {
				t.Errorf("got %v, want %v", got, found)
			}

			if len(store.acknowledged) != 1 || store.acknowledged[0] != [2]string{"nest", "token"} {
				t.Errorf("unexpected acknowledgement %v", store.acknowledged)
			}
		}
	})

	t.Run("only POST", func(t *testing.T) {
		store := &fakeStore{}
		h := &Handler{store: store}

		if _, err := h.handleHttpAcknowledgeRequest(nil, &http.Request{Method: http.MethodGet}, &acknowledgeRequest{Source: "a", Key: "b"}); err == nil {
			t.Error("a GET was accepted")
		}

		if len(store.acknowledged) != 0 {
			t.Error("a rejected request still acknowledged something")
		}
	})

	t.Run("needs a source and a key", func(t *testing.T) {
		for _, body := range []any{
			&acknowledgeRequest{Key: "token"},
			&acknowledgeRequest{Source: "nest"},
			&acknowledgeRequest{},
			nil,
			"not a request",
		} {
			store := &fakeStore{}
			h := &Handler{store: store}

			if _, err := h.handleHttpAcknowledgeRequest(nil, post, body); err == nil {
				t.Errorf("%#v: expected an error", body)
			}

			if len(store.acknowledged) != 0 {
				t.Errorf("%#v: acknowledged something", body)
			}
		}
	})

	t.Run("passes a failure on", func(t *testing.T) {
		h := &Handler{store: &fakeStore{err: errors.New("boom")}}

		if _, err := h.handleHttpAcknowledgeRequest(nil, post, &acknowledgeRequest{Source: "a", Key: "b"}); err == nil {
			t.Error("expected the failure to be reported")
		}
	})
}

func TestRelativeTime(t *testing.T) {
	for _, c := range []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero", time.Time{}, "never"},
		{"now", time.Now(), "just now"},
		{"slightly in the future", time.Now().Add(30 * time.Second), "just now"},
		{"minutes", time.Now().Add(-5*time.Minute - 10*time.Second), "5m ago"},
		{"hours", time.Now().Add(-3*time.Hour - 20*time.Minute - 10*time.Second), "3h 20m ago"},
		{"days", time.Now().Add(-(2*24+5)*time.Hour - time.Minute), "2d 5h ago"},
	} {
		if got := relativeTime(c.t); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
