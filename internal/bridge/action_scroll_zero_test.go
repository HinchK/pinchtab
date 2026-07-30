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
	// Scroll and wheel share one resolver, so each must still name its OWN spelling:
	// telling a scroll caller to pass deltaX names a field its surface does not accept.
	if !strings.Contains(err.Error(), "scrollX/scrollY") {
		t.Errorf("error = %v, want it to name the spelling scroll accepts", err)
	}

	// An absent delta must reach the default step. Resolving the viewport needs a browser
	// this test does not have, so the assertion is that it got PAST the zero-delta branch
	// and failed later — a refusal here would mean the default step was lost.
	_, err = b.actionScroll(context.Background(), decodeScrollRequest(t, `{"kind":"scroll"}`))
	if err != nil && strings.Contains(err.Error(), "zero delta") {
		t.Errorf("a bare scroll was refused as a zero delta: %v", err)
	}
}

// mouse-wheel is the sibling spelling of the same question, and it reaches further: the MCP
// scroll tool routes to wheel semantics whenever deltaX/deltaY or a coordinate is given, so
// an agent passing a computed deltaY of 0 lands here rather than on actionScroll. Scroll
// refusing a zero while wheel silently scrolled a notch would be one rule with two answers.
func TestWheelDeltaRefusesAnExplicitZeroInEitherSpelling(t *testing.T) {
	for _, tc := range []struct {
		body     string
		wantErr  bool
		wantX    int
		wantY    int
		whyNotOK string
	}{
		{body: `{"kind":"mouse-wheel"}`, wantY: 120, whyNotOK: "a bare wheel has always meant one notch down"},
		{body: `{"kind":"mouse-wheel","deltaY":0}`, wantErr: true},
		{body: `{"kind":"mouse-wheel","deltaX":0}`, wantErr: true},
		{body: `{"kind":"mouse-wheel","deltaX":0,"deltaY":0}`, wantErr: true},
		{body: `{"kind":"mouse-wheel","scrollY":0}`, wantErr: true, whyNotOK: "the legacy spelling asks the same question"},
		{body: `{"kind":"mouse-wheel","deltaX":0,"deltaY":500}`, wantX: 0, wantY: 500, whyNotOK: "a zero on one axis is a real scroll on the other"},
		{body: `{"kind":"mouse-wheel","deltaY":-300}`, wantY: -300},
		{body: `{"kind":"mouse-wheel","deltaY":0,"scrollY":300}`, wantY: 300, whyNotOK: "an explicit zero delta still carries a non-zero legacy delta"},
	} {
		gotX, gotY, err := wheelDelta(decodeScrollRequest(t, tc.body))

		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: accepted, resolving to (%d,%d) — a zero delta scrolls a default notch nobody asked for", tc.body, gotX, gotY)
				continue
			}
			if !strings.Contains(err.Error(), "deltaX/deltaY") {
				t.Errorf("%s: error = %v, want it to name the spelling wheel accepts", tc.body, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: refused (%v); %s", tc.body, err, tc.whyNotOK)
			continue
		}
		if gotX != tc.wantX || gotY != tc.wantY {
			t.Errorf("%s: delta = (%d,%d), want (%d,%d); %s", tc.body, gotX, gotY, tc.wantX, tc.wantY, tc.whyNotOK)
		}
	}
}
