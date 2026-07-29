package chrome

import (
	"testing"

	"github.com/pinchtab/pinchtab/internal/cdptk"
)

// The browser-level clip tests cannot pin this choice on their own: in headless
// Chrome fromSurface is a no-op, so an always-true "simplification" produces
// byte-identical images and passes them. This is the guard that fails instead —
// the unclipped screencast poll must keep the direct view read.
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
