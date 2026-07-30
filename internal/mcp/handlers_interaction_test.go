package mcp

import (
	"strings"
	"testing"
)

func TestHandleClick(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_click", map[string]any{
		"ref": "e5",
	}, srv)

	text := resultText(t, r)
	if !strings.Contains(text, "click") {
		t.Errorf("expected click in response, got %s", text)
	}
}

func TestHandleClickWaitNav(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_click", map[string]any{
		"ref":     "e5",
		"waitNav": true,
	}, srv)

	text := resultText(t, r)
	if !strings.Contains(text, `"waitNav":true`) {
		t.Errorf("expected waitNav in action payload, got %s", text)
	}
}

func TestHandleClickMode(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_click", map[string]any{
		"ref":  "e5",
		"mode": "dispatch",
	}, srv)

	resp := resultJSON(t, r)
	body, _ := resp["body"].(map[string]any)
	if got, _ := body["mode"].(string); got != "dispatch" {
		t.Fatalf("mode = %q, want dispatch", got)
	}
}

func TestHandleClickModeRejectsInvalidValue(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_click", map[string]any{
		"ref":  "e5",
		"mode": "raw",
	}, srv)
	if !r.IsError {
		t.Fatal("expected error for invalid mode")
	}
}

func TestHandleClickRejectsModeAndHumanizeTogether(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_click", map[string]any{
		"ref":      "e5",
		"mode":     "dom",
		"humanize": true,
	}, srv)
	if !r.IsError {
		t.Fatal("expected error when mode and humanize are both set")
	}
}

func TestHandleClickMissingRef(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_click", map[string]any{}, srv)
	if !r.IsError {
		t.Error("expected error for missing ref")
	}
}

func TestHandleClickCoordinates(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_click", map[string]any{
		"x": float64(120),
		"y": float64(340),
	}, srv)

	resp := resultJSON(t, r)
	body, _ := resp["body"].(map[string]any)
	if got, ok := body["hasXY"].(bool); !ok || !got {
		t.Fatalf("expected hasXY=true, got %#v", body["hasXY"])
	}
	if got, _ := body["x"].(float64); got != 120 {
		t.Fatalf("x = %v, want 120", got)
	}
	if got, _ := body["y"].(float64); got != 340 {
		t.Fatalf("y = %v, want 340", got)
	}
}

func TestHandleClickQueryAliasUsesSemanticSelector(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_click", map[string]any{
		"query": "login button",
	}, srv)

	resp := resultJSON(t, r)
	body, _ := resp["body"].(map[string]any)
	if got, _ := body["selector"].(string); got != "find:login button" {
		t.Fatalf("selector = %q, want %q", got, "find:login button")
	}
}

func TestHandleClickQueryAliasNumericTextUsesSemanticSelector(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_click", map[string]any{
		"query": "50.50",
	}, srv)

	resp := resultJSON(t, r)
	body, _ := resp["body"].(map[string]any)
	if got, _ := body["selector"].(string); got != "find:50.50" {
		t.Fatalf("selector = %q, want %q", got, "find:50.50")
	}
}

func TestHandleClickQueryAliasPreservesStructuredLocator(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_click", map[string]any{
		"query": "label:Email",
	}, srv)

	resp := resultJSON(t, r)
	body, _ := resp["body"].(map[string]any)
	if got, _ := body["selector"].(string); got != "label:Email" {
		t.Fatalf("selector = %q, want label:Email", got)
	}
}

func TestHandleClickDialogActionPassThrough(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_click", map[string]any{
		"ref":          "e5",
		"dialogAction": "accept",
		"dialogText":   "pinchtab",
	}, srv)

	resp := resultJSON(t, r)
	body, _ := resp["body"].(map[string]any)
	if got, _ := body["dialogAction"].(string); got != "accept" {
		t.Fatalf("dialogAction = %q, want accept", got)
	}
	if got, _ := body["dialogText"].(string); got != "pinchtab" {
		t.Fatalf("dialogText = %q, want pinchtab", got)
	}
}

func TestHandleClickDialogActionRejectsInvalidValue(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_click", map[string]any{
		"ref":          "e5",
		"dialogAction": "maybe",
	}, srv)

	if !r.IsError {
		t.Fatal("expected error for invalid dialogAction")
	}
}

func TestHandleType(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_type", map[string]any{
		"ref":  "e12",
		"text": "hello world",
	}, srv)

	text := resultText(t, r)
	if !strings.Contains(text, "type") {
		t.Errorf("expected type in response, got %s", text)
	}
	if !strings.Contains(text, "hello world") {
		t.Errorf("expected text in response, got %s", text)
	}
}

func TestHandlePress(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_press", map[string]any{
		"key": "Enter",
	}, srv)

	text := resultText(t, r)
	if !strings.Contains(text, "Enter") {
		t.Errorf("expected Enter in response, got %s", text)
	}
}

func TestHandleSelect(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_select", map[string]any{
		"ref":   "e3",
		"value": "option2",
	}, srv)

	text := resultText(t, r)
	if !strings.Contains(text, "select") {
		t.Errorf("expected select, got %s", text)
	}
}

func TestHandleScroll(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_scroll", map[string]any{
		"pixels": float64(500),
	}, srv)

	text := resultText(t, r)
	if !strings.Contains(text, "scroll") {
		t.Errorf("expected scroll, got %s", text)
	}
}

func TestHandleScrollDirectionUsesMouseWheel(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_scroll", map[string]any{
		"direction": "down",
		"steps":     float64(2),
	}, srv)

	resp := resultJSON(t, r)
	body, _ := resp["body"].(map[string]any)
	if got, _ := body["kind"].(string); got != "mouse-wheel" {
		t.Fatalf("kind = %q, want mouse-wheel", got)
	}
	if got, _ := body["deltaY"].(float64); got != 240 {
		t.Fatalf("deltaY = %v, want 240", got)
	}
}

func TestHandleScrollSelectorPixelsUsesMouseWheel(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_scroll", map[string]any{
		"selector": "#list",
		"pixels":   float64(300),
	}, srv)

	resp := resultJSON(t, r)
	body, _ := resp["body"].(map[string]any)
	if got, _ := body["kind"].(string); got != "mouse-wheel" {
		t.Fatalf("kind = %q, want mouse-wheel", got)
	}
	if got, _ := body["deltaY"].(float64); got != 300 {
		t.Fatalf("deltaY = %v, want 300", got)
	}
}

func TestHandleScrollIntoView(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_scroll_into_view", map[string]any{
		"ref": "e9",
	}, srv)

	resp := resultJSON(t, r)
	body, _ := resp["body"].(map[string]any)
	if got, _ := body["kind"].(string); got != "scrollintoview" {
		t.Fatalf("kind = %q, want scrollintoview", got)
	}
	if got, _ := body["selector"].(string); got != "e9" {
		t.Fatalf("selector = %q, want e9", got)
	}
}

func TestHandleFill(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_fill", map[string]any{
		"ref":   "e7",
		"value": "test@example.com",
	}, srv)

	text := resultText(t, r)
	if !strings.Contains(text, "fill") {
		t.Errorf("expected fill, got %s", text)
	}
}

func TestHandleHover(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_hover", map[string]any{"ref": "e3"}, srv)
	text := resultText(t, r)
	if !strings.Contains(text, "hover") {
		t.Errorf("expected hover, got %s", text)
	}
}

func TestHandleFocus(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_focus", map[string]any{"ref": "e1"}, srv)
	text := resultText(t, r)
	if !strings.Contains(text, "focus") {
		t.Errorf("expected focus, got %s", text)
	}
}

// actionToolTargets is the per-tool matrix this card decided: every action tool
// declares nodeId, because the bridge honours req.NodeID for every one of the nine
// kinds. requiredArgs are the tool's own non-target requirements, and
// selectorOptionalWithNodeID marks the tools whose MCP layer used to demand a
// selector even though the bridge does not.
var actionToolTargets = []struct {
	tool                       string
	requiredArgs               map[string]any
	selectorOptionalWithNodeID bool
}{
	{tool: "pinchtab_click", selectorOptionalWithNodeID: true},
	{tool: "pinchtab_hover", selectorOptionalWithNodeID: true},
	{tool: "pinchtab_focus", selectorOptionalWithNodeID: true},
	{tool: "pinchtab_type", requiredArgs: map[string]any{"text": "hi"}, selectorOptionalWithNodeID: true},
	{tool: "pinchtab_fill", requiredArgs: map[string]any{"value": "v"}, selectorOptionalWithNodeID: true},
	{tool: "pinchtab_select", requiredArgs: map[string]any{"value": "v"}, selectorOptionalWithNodeID: true},
	{tool: "pinchtab_scroll_into_view", selectorOptionalWithNodeID: true},
	{tool: "pinchtab_scroll"},
	{tool: "pinchtab_press", requiredArgs: map[string]any{"key": "Enter"}},
}

func actionArgs(tool string, extra map[string]any) map[string]any {
	args := map[string]any{}
	for _, entry := range actionToolTargets {
		if entry.tool != tool {
			continue
		}
		for name, value := range entry.requiredArgs {
			args[name] = value
		}
	}
	for name, value := range extra {
		args[name] = value
	}
	return args
}

// nodeId was read before the switch on kind, so all nine tools forwarded it, but
// only click, hover and focus declared it. On the other six that meant no
// discovery and — because validateTypedArgs keys its type map per tool — no
// validation either, so a malformed value was dropped in silence.
func TestEveryActionToolAcceptsAndValidatesNodeID(t *testing.T) {
	for _, tc := range actionToolTargets {
		t.Run(tc.tool, func(t *testing.T) {
			srv, _ := upstreamRecorder(t)
			result := callTool(t, tc.tool, actionArgs(tc.tool, map[string]any{"selector": "#a", "nodeId": float64(42)}), srv)
			if result.IsError {
				t.Fatalf("a valid nodeId was rejected: %s", resultText(t, result))
			}
			body, _ := resultJSON(t, result)["body"].(map[string]any)
			if got, ok := body["nodeId"]; !ok || got != float64(42) {
				t.Errorf("outbound nodeId = %v (present: %v), want 42 — the bridge honours it for this kind", got, ok)
			}

			srv2, paths := upstreamRecorder(t)
			malformed := callTool(t, tc.tool, actionArgs(tc.tool, map[string]any{"selector": "#a", "nodeId": "abc"}), srv2)
			if !malformed.IsError {
				t.Fatalf("nodeId \"abc\" was accepted and silently dropped; upstream saw %v", *paths)
			}
			if text := resultText(t, malformed); !strings.Contains(text, "nodeId") {
				t.Errorf("rejection %q does not name nodeId, so the caller cannot correct it", text)
			}
			if len(*paths) != 0 {
				t.Errorf("upstream was called %v despite the malformed argument", *paths)
			}
		})
	}
}

// The MCP layer required a selector on type, fill, select and scroll_into_view
// even though the bridge resolves those kinds from NodeID alone. Declaring nodeId
// there without relaxing this would advertise an argument that cannot be used.
func TestNodeIDAloneSatisfiesTheTargetRequirement(t *testing.T) {
	for _, tc := range actionToolTargets {
		if !tc.selectorOptionalWithNodeID {
			continue
		}
		t.Run(tc.tool, func(t *testing.T) {
			srv, _ := upstreamRecorder(t)
			result := callTool(t, tc.tool, actionArgs(tc.tool, map[string]any{"nodeId": float64(42)}), srv)
			if result.IsError {
				t.Fatalf("nodeId alone was rejected: %s", resultText(t, result))
			}
			body, _ := resultJSON(t, result)["body"].(map[string]any)
			if _, ok := body["selector"]; ok {
				t.Errorf("outbound body carries a selector the caller never sent: %v", body)
			}
			if got := body["nodeId"]; got != float64(42) {
				t.Errorf("outbound nodeId = %v, want 42", got)
			}

			srv2, paths := upstreamRecorder(t)
			neither := callTool(t, tc.tool, actionArgs(tc.tool, nil), srv2)
			if !neither.IsError {
				t.Fatalf("a call with no target at all was accepted; upstream saw %v", *paths)
			}
			if text := resultText(t, neither); !strings.Contains(text, "selector") {
				t.Errorf("the no-target rejection %q should still name selector", text)
			}
		})
	}
}

// x/y had the same shape as nodeId — read before the switch, so forwarded for all
// nine kinds with hasXY set — but the opposite correct answer: the bridge honours
// coordinates only for the pointer kinds, so the fix is to stop reading it
// elsewhere rather than to declare it everywhere.
func TestCoordinatesReachTheWireOnlyForTheToolsThatDeclareThem(t *testing.T) {
	for _, tc := range actionToolTargets {
		t.Run(tc.tool, func(t *testing.T) {
			_, declared := schemaArgTypesOnce()[tc.tool]["x"]
			srv, _ := upstreamRecorder(t)
			result := callTool(t, tc.tool, actionArgs(tc.tool, map[string]any{"selector": "#a", "x": float64(11), "y": float64(22)}), srv)
			if result.IsError {
				t.Fatalf("coordinates were rejected: %s", resultText(t, result))
			}
			body, _ := resultJSON(t, result)["body"].(map[string]any)
			_, forwarded := body["hasXY"]
			if declared != forwarded {
				t.Errorf("%s declares x/y = %v but forwards them = %v (body %v)", tc.tool, declared, forwarded, body)
			}
		})
	}
}

// pinchtab_fill posted the caller's string under "value", a real ActionRequest field that
// actionFill does not read, so the write was empty and the tool answered filled:true with
// len:0. The tool surface is the only place this is visible — the bridge action was always
// correct, and no unit test at that layer could see it.
func TestHandleFillForwardsTheFieldTheFillActionReads(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_fill", map[string]any{
		"ref":   "e0",
		"value": "FILLED",
	}, srv)

	body, _ := resultJSON(t, r)["body"].(map[string]any)
	if got, _ := body["text"].(string); got != "FILLED" {
		t.Fatalf("forwarded text = %q, want the caller's value; payload was %v", got, body)
	}
	if _, leftover := body["value"]; leftover {
		t.Errorf("payload still carries a value key that fill ignores: %v", body)
	}
}

// The discriminator: two tools, the same client argument name, and opposite consumers —
// actionSelect reads Value, actionFill reads Text. Both must post to the field their own
// action reads, which is what a shared "value" key silently got wrong for one of them.
func TestFillAndSelectEachPostToTheFieldTheirActionReads(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	fill, _ := resultJSON(t, callTool(t, "pinchtab_fill", map[string]any{"ref": "e0", "value": "ZZZ"}, srv))["body"].(map[string]any)
	sel, _ := resultJSON(t, callTool(t, "pinchtab_select", map[string]any{"ref": "e1", "value": "y"}, srv))["body"].(map[string]any)

	if got, _ := fill["text"].(string); got != "ZZZ" {
		t.Errorf("fill payload = %v, want text=ZZZ", fill)
	}
	if got, _ := sel["value"].(string); got != "y" {
		t.Errorf("select payload = %v, want value=y", sel)
	}
}

// The other spelling still works, since the tool has always accepted both.
func TestHandleFillAcceptsTheTextSpellingToo(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_fill", map[string]any{"ref": "e0", "text": "FROM_TEXT"}, srv)
	body, _ := resultJSON(t, r)["body"].(map[string]any)
	if got, _ := body["text"].(string); got != "FROM_TEXT" {
		t.Fatalf("forwarded text = %q, want FROM_TEXT; payload was %v", got, body)
	}
}

// The pair is the assertion. Supplied-empty must clear and absent must still refuse —
// either half alone is satisfiable by deleting the check, which would silently forward a
// fill with no text at all. Driven through the tool surface, because the bridge action
// was always able to express both and the MCP layer was the only one that could not.
func TestFillClearsOnASuppliedEmptyValueAndStillRefusesAnAbsentOne(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	for _, key := range []string{"value", "text"} {
		t.Run("supplied empty "+key+" clears", func(t *testing.T) {
			r := callTool(t, "pinchtab_fill", map[string]any{"ref": "e0", key: ""}, srv)
			if r.IsError {
				t.Fatalf("a supplied empty %s was refused: %s", key, resultText(t, r))
			}
			body, _ := resultJSON(t, r)["body"].(map[string]any)
			text, present := body["text"]
			if !present {
				t.Fatalf("payload carries no text key, so the bridge cannot tell a clear from a request whose text never arrived: %v", body)
			}
			if got, _ := text.(string); got != "" {
				t.Errorf("forwarded text = %q, want the empty string that clears the field", got)
			}
		})
	}

	t.Run("absent value is refused", func(t *testing.T) {
		r := callTool(t, "pinchtab_fill", map[string]any{"ref": "e0"}, srv)
		if !r.IsError {
			body, _ := resultJSON(t, r)["body"].(map[string]any)
			t.Fatalf("a fill with no value at all was forwarded as %v; absent is not a clear", body)
		}
		message := resultText(t, r)
		if strings.Contains(message, "is missing") && strings.Contains(message, "'value'") {
			t.Errorf("refusal %q reads as a supplied parameter being missing; it must say what fill needs and how to clear", message)
		}
		if !strings.Contains(message, "clear") {
			t.Errorf("refusal %q does not tell the caller how to clear a field, which is the case it is most likely to be confused with", message)
		}
	})
}

// Whitespace is content, not a clear: the raw API fills it verbatim, so the tool must not
// trim a supplied value into the clear idiom.
func TestFillForwardsASuppliedValueVerbatim(t *testing.T) {
	srv := mockPinchTab()
	defer srv.Close()

	r := callTool(t, "pinchtab_fill", map[string]any{"ref": "e0", "value": "  spaced  "}, srv)
	body, _ := resultJSON(t, r)["body"].(map[string]any)
	if got, _ := body["text"].(string); got != "  spaced  " {
		t.Errorf("forwarded text = %q, want the caller's string unmodified", got)
	}
}
