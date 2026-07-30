package bridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func decodeScrollRequest(t *testing.T, body string) ActionRequest {
	t.Helper()

	var req ActionRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return req
}

// An absent delta and an explicit zero are different requests, and only the JSON key
// presence can tell them apart — both leave ScrollX and ScrollY at 0.
func TestHasScrollSeparatesAnAbsentDeltaFromAnExplicitZero(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{`{"kind":"scroll"}`, false},
		{`{"kind":"scroll","selector":"#footer"}`, false},
		{`{"kind":"scroll","scrollY":0}`, true},
		{`{"kind":"scroll","scrollX":0}`, true},
		{`{"kind":"scroll","scrollY":800}`, true},
	} {
		if got := decodeScrollRequest(t, tc.body).HasScroll; got != tc.want {
			t.Errorf("%s: HasScroll = %v, want %v", tc.body, got, tc.want)
		}
	}
}

// The defect this pins: scrollX==0 && scrollY==0 was answered with a default 120px step
// whatever the caller meant, so an explicit zero scrolled DOWN. A caller computing a
// remaining distance reaches zero exactly when it wants no movement, so that is the wrong
// direction reported as success. The refusal must not fire for an absent delta, which has
// always meant one step.
func TestScrollRefusesAnExplicitZeroDeltaButKeepsTheDefaultWhenAbsent(t *testing.T) {
	b := &Bridge{}

	_, err := b.actionScroll(context.Background(), decodeScrollRequest(t, `{"kind":"scroll","scrollY":0}`))
	if err == nil {
		t.Fatal("an explicit zero delta was accepted, so it scrolled a default step in a direction nobody asked for")
	}
	if !strings.Contains(err.Error(), "zero delta") {
		t.Errorf("error = %v, want it to name the zero delta", err)
	}

	// An absent delta must reach the default step. Resolving the viewport needs a browser
	// this test does not have, so the assertion is that it got PAST the zero-delta branch
	// and failed later — a refusal here would mean the default step was lost.
	_, err = b.actionScroll(context.Background(), decodeScrollRequest(t, `{"kind":"scroll"}`))
	if err != nil && strings.Contains(err.Error(), "zero delta") {
		t.Errorf("a bare scroll was refused as a zero delta: %v", err)
	}
}
