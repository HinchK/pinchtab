package chrome

import (
	"testing"

	"github.com/pinchtab/pinchtab/internal/cdptk"
)

// The clipped browser test pins fromSurface=true for a clip; nothing else pins
// fromSurface=false for a nil one, because both flags return the viewport at the
// same dimensions and only the pixels differ. So an always-true
// "simplification" passes every dimension assertion this package has. This is
// the guard that fails instead — the unclipped screencast poll must keep the
// direct view read rather than wait on a frame an idle headed page never swaps.
func TestCaptureFromSurfaceOnlyForClippedCaptures(t *testing.T) {
	tests := []struct {
		name string
		clip *cdptk.ScreenshotClip
		want bool
	}{
		{name: "no clip keeps the fast view read", clip: nil, want: false},
		{name: "clip needs the compositor surface", clip: &cdptk.ScreenshotClip{Width: 120, Height: 60, Scale: 1}, want: true},
		{name: "offset clip needs the compositor surface", clip: &cdptk.ScreenshotClip{X: 40, Y: 60, Width: 120, Height: 60, Scale: 1}, want: true},
		{name: "zero-sized clip is still a clip", clip: &cdptk.ScreenshotClip{}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := captureFromSurface(tt.clip); got != tt.want {
				t.Errorf("captureFromSurface(%+v) = %v, want %v", tt.clip, got, tt.want)
			}
		})
	}
}
