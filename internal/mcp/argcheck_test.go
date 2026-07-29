package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// upstreamRecorder records every request that reaches PinchTab, so a test can
// assert a rejected call never got there — an error result alone cannot tell a
// pre-dispatch rejection from a request that went out and came back failing.
func upstreamRecorder(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	paths := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.Method+" "+r.URL.Path)
		resp := map[string]any{"path": r.URL.Path}
		if body, _ := io.ReadAll(r.Body); len(body) > 0 {
			var parsed map[string]any
			if json.Unmarshal(body, &parsed) == nil {
				resp["body"] = parsed
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, paths
}

// A malformed delta must be reported, not dropped. Dropping it degrades a wheel
// scroll into a bare scroll with no magnitude, because hasDeltaY gates the wheel
// branch.
func TestScrollRejectsAMalformedDeltaWithoutCallingUpstream(t *testing.T) {
	srv, paths := upstreamRecorder(t)

	for _, malformed := range []string{"-300px", "300 pixels", "three hundred"} {
		t.Run(malformed, func(t *testing.T) {
			*paths = nil
			result := callTool(t, "pinchtab_scroll", map[string]any{"deltaY": malformed}, srv)

			if !result.IsError {
				t.Fatalf("deltaY=%q was accepted: %s", malformed, resultText(t, result))
			}
			message := resultText(t, result)
			if !strings.Contains(message, "deltaY") {
				t.Errorf("error %q does not name the argument", message)
			}
			if !strings.Contains(message, malformed) {
				t.Errorf("error %q does not echo the received value", message)
			}
			if len(*paths) != 0 {
				t.Errorf("rejected call still reached upstream: %v", *paths)
			}
		})
	}
}

// The case a caller cannot detect: direction synthesises a magnitude precisely
// because the malformed deltaY was dropped, so the tool scrolls DOWN by 120 when
// asked to scroll UP by 300 — sign inverted, magnitude invented, no error.
func TestScrollRejectsAMalformedDeltaRatherThanLettingDirectionInventOne(t *testing.T) {
	srv, paths := upstreamRecorder(t)

	result := callTool(t, "pinchtab_scroll", map[string]any{
		"deltaY":    "-300px",
		"direction": "down",
	}, srv)

	if !result.IsError {
		body := resultText(t, result)
		if strings.Contains(body, "120") {
			t.Fatalf("direction invented a magnitude for a malformed deltaY: %s", body)
		}
		t.Fatalf("malformed deltaY with direction was accepted: %s", body)
	}
	if len(*paths) != 0 {
		t.Errorf("rejected call still reached upstream: %v", *paths)
	}
}

// withBounds is an opt-out, so a dropped "no" leaves bounds switched on and the
// response looks like the default rather than like the request.
func TestCaptureRejectsAMalformedBoolean(t *testing.T) {
	srv, paths := upstreamRecorder(t)

	result := callTool(t, "pinchtab_capture", map[string]any{"withBounds": "no"}, srv)

	if !result.IsError {
		t.Fatalf(`withBounds="no" was accepted: %s`, resultText(t, result))
	}
	message := resultText(t, result)
	if !strings.Contains(message, "withBounds") {
		t.Errorf("error %q does not name the argument", message)
	}
	if !strings.Contains(message, "no") {
		t.Errorf("error %q does not echo the received value", message)
	}
	if len(*paths) != 0 {
		t.Errorf("rejected call still reached upstream: %v", *paths)
	}
}

// Models emit "" for "not set"; turning that into a failure would be a
// regression, and so would rejecting an argument nobody passed.
func TestAbsentAndEmptyTypedArgumentsAreNotRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{name: "absent", args: map[string]any{}},
		{name: "empty string", args: map[string]any{"deltaY": "", "steps": "", "x": ""}},
		{name: "explicit null", args: map[string]any{"deltaY": nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateTypedArgs("pinchtab_scroll", tc.args); err != nil {
				t.Errorf("validateTypedArgs(%v) = %v, want nil — not set must not become a failure", tc.args, err)
			}
		})
	}
}

// Every shape the accessors read today must still pass validation, or the fix
// would break working callers rather than malformed ones.
func TestReadableTypedArgumentsPassValidation(t *testing.T) {
	for _, args := range []map[string]any{
		{"deltaY": float64(-300)},
		{"deltaY": "-300"},
		{"deltaY": " -300 "},
		{"deltaY": "-300.5"},
		{"steps": "2", "x": "10", "y": float64(20)},
	} {
		if err := validateTypedArgs("pinchtab_scroll", args); err != nil {
			t.Errorf("validateTypedArgs(%v) = %v, want nil", args, err)
		}
	}
	for _, args := range []map[string]any{
		{"withBounds": true},
		{"withBounds": "true"},
		{"withBounds": "false"},
		{"withBounds": "1"},
	} {
		if err := validateTypedArgs("pinchtab_capture", args); err != nil {
			t.Errorf("validateTypedArgs(%v) = %v, want nil", args, err)
		}
	}
}

// The argument list is derived from the schemas, so a tool gaining a WithNumber
// argument is validated on arrival. This asserts the derivation actually found
// the declared types rather than silently returning an empty map, which would
// make every test above pass vacuously.
func TestTypedArgsAreDerivedFromTheToolSchemas(t *testing.T) {
	types := schemaArgTypesOnce()
	if len(types) == 0 {
		t.Fatal("no tool schemas parsed — validation would be a no-op for every tool")
	}

	declared := 0
	for _, tool := range allTools() {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal %s schema: %v", tool.Name, err)
		}
		for name, property := range schema.Properties {
			switch property.Type {
			case "number", "integer", "boolean":
				declared++
				if got := types[tool.Name][name]; got != property.Type {
					t.Errorf("%s.%s typed %q by the schema but %q by the validator", tool.Name, name, property.Type, got)
				}
			default:
				if got, ok := types[tool.Name][name]; ok {
					t.Errorf("%s.%s is %q in the schema but the validator typed it %q", tool.Name, name, property.Type, got)
				}
			}
		}
	}
	if declared == 0 {
		t.Fatal("no numeric or boolean argument found in any schema — this guard is checking nothing")
	}
	t.Logf("validating %d schema-declared numeric/boolean arguments", declared)
}

// A handler with no schema would silently skip validation, so a new tool cannot
// be added to one side only.
func TestEveryHandlerHasASchemaAndEverySchemaAHandler(t *testing.T) {
	handlers := rawHandlerMap(NewClient("http://example.invalid", ""))
	schemas := map[string]struct{}{}
	for _, tool := range allTools() {
		schemas[tool.Name] = struct{}{}
	}

	for name := range handlers {
		if _, ok := schemas[name]; !ok {
			t.Errorf("handler %q has no tool schema, so its arguments are never validated", name)
		}
	}
	for name := range schemas {
		if _, ok := handlers[name]; !ok {
			t.Errorf("tool %q has a schema but no handler", name)
		}
	}
}

// Every argument the package reads with a TYPED accessor must be declared as
// number/integer/boolean in some tool's schema. Two things follow from a missing
// declaration: a model cannot discover the argument, and validateTypedArgs cannot
// see it either — it derives from the schemas, so an undeclared argument keeps the
// silent-coercion-drop this package otherwise rejects.
//
// Name-level, not per-tool, and deliberately so: a per-tool assertion needs a
// tool-to-handler-source mapping that is declared nowhere and would be brittle
// enough to invite deletion. Name-level needs no mapping and still catches the
// class.
//
// optString is deliberately NOT scanned. A string argument read without a
// declaration is undiscoverable but not a correctness hazard: there is no
// coercion, so nothing is silently dropped. Widening this census to optString
// would produce false positives on the many string aliases the handlers accept.
func TestEveryTypedAccessorArgumentIsDeclaredInASchema(t *testing.T) {
	declared := map[string]string{}
	for _, tool := range allTools() {
		for name, kind := range typedArgsOf(tool) {
			declared[name] = kind
		}
	}
	if len(declared) == 0 {
		t.Fatal("no typed arguments found in any schema — this census is checking nothing")
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	accessor := regexp.MustCompile(`opt(?:Int|Float|Bool)\(r, "([^"]+)"\)`)

	scanned, found := 0, 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		for _, match := range accessor.FindAllStringSubmatch(string(raw), -1) {
			found++
			arg := match[1]
			if _, ok := declared[arg]; !ok {
				t.Errorf("%s reads %q with a typed accessor but no tool schema declares it as number/integer/boolean — "+
					"a model cannot discover it and validateTypedArgs cannot validate it, so a malformed value is silently dropped", name, arg)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no Go source scanned — this census is checking nothing")
	}
	if found == 0 {
		t.Fatal("no typed accessor call found — the pattern no longer matches how arguments are read")
	}
	t.Logf("checked %d typed-accessor call sites against %d declared typed arguments", found, len(declared))
}

// Declaring humanize is only half the fix: it was previously read solely to raise
// the mutual-exclusion error and never forwarded, so humanized input was
// unreachable from MCP even for a caller who guessed the name.
func TestPointerToolsForwardHumanizeToUpstream(t *testing.T) {
	for _, tool := range []string{"pinchtab_click", "pinchtab_hover"} {
		t.Run(tool, func(t *testing.T) {
			srv, _ := upstreamRecorder(t)

			result := callTool(t, tool, map[string]any{"selector": "#b", "humanize": true}, srv)
			if result.IsError {
				t.Fatalf("humanize=true was rejected: %s", resultText(t, result))
			}
			body, _ := resultJSON(t, result)["body"].(map[string]any)
			if got, ok := body["humanize"]; !ok || got != true {
				t.Errorf("outbound body humanize = %v (present: %v), want true — the argument must be usable, not just accepted", got, ok)
			}
		})
	}
}

// An explicit false is an opt-OUT and must travel, because the wire field is a
// per-request override of the instance default rather than a flag.
func TestPointerToolsForwardAnExplicitHumanizeFalse(t *testing.T) {
	srv, _ := upstreamRecorder(t)

	result := callTool(t, "pinchtab_click", map[string]any{"selector": "#b", "humanize": false}, srv)
	body, _ := resultJSON(t, result)["body"].(map[string]any)
	if got, ok := body["humanize"]; !ok || got != false {
		t.Errorf("outbound body humanize = %v (present: %v), want false to travel as an opt-out", got, ok)
	}
}

// Forwarding must not become an unconditional default: a call that omits humanize
// has to leave the instance config in charge.
func TestOmittedHumanizeIsNotForwarded(t *testing.T) {
	for _, tool := range []string{"pinchtab_click", "pinchtab_hover"} {
		t.Run(tool, func(t *testing.T) {
			srv, _ := upstreamRecorder(t)

			result := callTool(t, tool, map[string]any{"selector": "#b"}, srv)
			body, _ := resultJSON(t, result)["body"].(map[string]any)
			if got, ok := body["humanize"]; ok {
				t.Errorf("outbound body carries humanize = %v with none requested; the instance default must stay in charge", got)
			}
		})
	}
}

// The third defect: before humanize was declared, the schema-derived validator
// could not see it, so "yes" was silently dropped and the mutual-exclusion guard
// it feeds was bypassed — the guard fired for true and "true" but not "yes".
// argcheck.go is untouched; the declaration alone brings this under validation.
func TestMalformedHumanizeIsRejectedFromTheDeclarationAlone(t *testing.T) {
	srv, paths := upstreamRecorder(t)

	result := callTool(t, "pinchtab_click", map[string]any{"selector": "#b", "mode": "dom", "humanize": "yes"}, srv)
	if !result.IsError {
		t.Fatalf(`humanize="yes" was accepted: %s`, resultText(t, result))
	}
	message := resultText(t, result)
	if !strings.Contains(message, "humanize") {
		t.Errorf("error %q does not name the argument", message)
	}
	if len(*paths) != 0 {
		t.Errorf("rejected call still reached upstream: %v", *paths)
	}
}

// The mutual-exclusion rule the CLI enforces stays enforced, for the parsed string
// form too, and only on the tool that has both arguments.
func TestModeAndHumanizeRemainMutuallyExclusiveOnClick(t *testing.T) {
	for _, humanize := range []any{true, "true"} {
		srv, _ := upstreamRecorder(t)
		result := callTool(t, "pinchtab_click", map[string]any{"selector": "#b", "mode": "dom", "humanize": humanize}, srv)
		if !result.IsError {
			t.Errorf("mode+humanize=%v was accepted: %s", humanize, resultText(t, result))
		}
	}
}

// Only the pointer tools declare it. A tool without a pointer path must not, or
// the schema would advertise an argument its handler ignores.
func TestOnlyPointerToolsDeclareHumanize(t *testing.T) {
	declaring := map[string]bool{}
	for _, tool := range allTools() {
		if _, ok := typedArgsOf(tool)["humanize"]; ok {
			declaring[tool.Name] = true
		}
	}
	want := map[string]bool{"pinchtab_click": true, "pinchtab_hover": true}
	if !reflect.DeepEqual(declaring, want) {
		t.Errorf("tools declaring humanize = %v, want %v (the MCP members of the CLI addPointerActionFlags set)", declaring, want)
	}
	for kind := range humanizeAction {
		if !declaring["pinchtab_"+kind] {
			t.Errorf("handler forwards humanize for %q but pinchtab_%s does not declare it", kind, kind)
		}
	}
}
