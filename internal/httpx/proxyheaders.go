package httpx

import (
	"net/http"
	"strings"
)

const (
	HeaderRequestID               = "X-Request-Id"
	HeaderContentSecurityPolicy   = "Content-Security-Policy"
	HeaderFrameOptions            = "X-Frame-Options"
	HeaderContentTypeOptions      = "X-Content-Type-Options"
	HeaderStrictTransportSecurity = "Strict-Transport-Security"
)

var outerChainResponseHeaders = []string{
	HeaderRequestID,
	HeaderContentSecurityPolicy,
	HeaderFrameOptions,
	HeaderContentTypeOptions,
	HeaderStrictTransportSecurity,
}

var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
	"Host",
	// The failure-reason pair is consumed per hop: the proxy reads it into
	// RecordFailureReason, which re-stamps it on its own response for any
	// further hop, so a copied-through duplicate would be the one value no
	// recorder produced.
	FailureCodeHeader,
	FailureMessageHeader,
}

func OuterChainResponseHeaders() []string {
	return append([]string(nil), outerChainResponseHeaders...)
}

func OuterChainOwnsResponseHeader(key string) bool {
	return containsFold(outerChainResponseHeaders, key)
}

func IsHopByHopHeader(key string) bool {
	return containsFold(hopByHopHeaders, key)
}

func containsFold(names []string, key string) bool {
	for _, name := range names {
		if strings.EqualFold(name, key) {
			return true
		}
	}
	return false
}

// CopyProxiedResponseHeaders is the one response-header copy every proxy hop uses. The
// outer middleware chain has already set its own X-Request-Id and security headers on this
// response, so appending the upstream copies makes them multi-valued: a caller then sees
// two request ids, one of which the outer process never minted and therefore never logged.
func CopyProxiedResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		if IsHopByHopHeader(key) || OuterChainOwnsResponseHeader(key) {
			continue
		}
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}
