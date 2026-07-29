package httpx

import (
	"fmt"
	"net/http"
)

// DisabledEndpointMessage returns a consistent message for locked endpoint
// families. capability names the gated capability, which is not always the
// endpoint the caller asked for — /storage is gated by stateExport — so the
// sentence names the requirement instead of calling the label an endpoint.
func DisabledEndpointMessage(capability, setting string) string {
	return fmt.Sprintf("this endpoint requires the %s capability; enable %s in config to use it", capability, setting)
}

// DisabledEndpointHandler returns a handler that reports a capability gate lock.
func DisabledEndpointHandler(capability, setting, code string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		ErrorCode(w, http.StatusForbidden, code, DisabledEndpointMessage(capability, setting), false, map[string]any{
			"setting": setting,
			"hint":    fmt.Sprintf("Enable %s to use this feature.", setting),
			"remedy":  fmt.Sprintf("pinchtab config set %s true", setting),
		})
	}
}
