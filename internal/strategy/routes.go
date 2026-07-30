package strategy

import (
	"net/http"

	"github.com/pinchtab/pinchtab/internal/orchestrator"
	"github.com/pinchtab/pinchtab/internal/routes"
)

// capabilitySetting returns the label, config key and disabled code a refused route
// answers with, from routes.Meta — the one owner. This front previously restated the whole
// table, which is how the same route came to answer two different codes depending on which
// front a client reached; the handler layer reads Meta, so this must too.
//
// The fallback only covers a capability the catalogue gates without describing, which
// CapabilityEndpoints cannot currently produce. It is deliberately not the old synthesis:
// that derived "security.allow"+cap and cap+"_disabled", which is wrong for every
// camelCase capability — the reason each one needed an explicit case in the first place.
func capabilitySetting(cap routes.Capability) (feature, setting, code string) {
	if meta, ok := routes.Meta(cap); ok {
		return meta.Label, meta.Setting, meta.DisabledCode
	}
	return string(cap), "", ""
}

// RegisterShorthandRoutes registers all shorthand proxy routes on the mux,
// binding them to the given handler. It also registers capability-gated
// routes (evaluate, download, upload, screencast, macro) using the
// orchestrator's security settings.
//
// Routes are sourced from the shared routes.Core() catalogue.
func RegisterShorthandRoutes(mux *http.ServeMux, orch *orchestrator.Orchestrator, handler http.HandlerFunc) {
	wrapped := orch.WrapShorthand(handler)

	for _, route := range routes.ShorthandRoutes() {
		mux.HandleFunc(route, wrapped)
	}

	for cap, eps := range routes.CapabilityEndpoints() {
		enabled := orch.Allows(cap)
		feature, setting, code := capabilitySetting(cap)
		for _, ep := range eps {
			RegisterCapabilityRoute(mux, ep.Route(), enabled, feature, setting, code, wrapped)
		}
	}
}
