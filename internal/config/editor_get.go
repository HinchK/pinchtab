package config

import (
	"fmt"
	"strconv"
	"strings"
)

// GetConfigValue reads a dotted-path field from FileConfig and returns its string representation.
// Pointer fields that are unset return an empty string. Slice fields are comma-separated.
// The section vocabulary lives once, in configSections.
func GetConfigValue(fc *FileConfig, path string) (string, error) {
	section, field, err := lookupConfigSection(path)
	if err != nil {
		return "", err
	}
	return section.get(fc, field)
}

func getServerField(s *ServerConfig, field string) (string, error) {
	switch field {
	case "port":
		return s.Port, nil
	case "bind":
		return s.Bind, nil
	case "token":
		return s.Token, nil
	case "stateDir":
		return s.StateDir, nil
	case "logLevel":
		return s.LogLevel, nil
	case "trustProxyHeaders":
		return formatBoolPtr(s.TrustProxyHeaders), nil
	case "cookieSecure":
		return formatBoolPtr(s.CookieSecure), nil
	default:
		return "", fmt.Errorf("unknown field server.%s", field)
	}
}

func getBrowserField(b *BrowserConfig, field string) (string, error) {
	if strings.HasPrefix(field, "cloak.") {
		return getCloakBrowserField(&b.Cloak, strings.TrimPrefix(field, "cloak."))
	}
	if strings.HasPrefix(field, "proxy.") {
		return getBrowserProxyField(&b.Proxy, strings.TrimPrefix(field, "proxy."))
	}
	if strings.HasPrefix(field, "targets.") {
		return getBrowserTargetField(b.Targets, strings.TrimPrefix(field, "targets."))
	}
	switch field {
	case "provider":
		return "", fmt.Errorf("browser.provider is no longer supported; use browsers.default")
	case "version":
		return b.BrowserVersion, nil
	case "binary":
		return b.BrowserBinary, nil
	case "extraFlags":
		return b.BrowserExtraFlags, nil
	case "defaultTarget":
		return b.DefaultTarget, nil
	case "fallbackOrder":
		return strings.Join(b.FallbackOrder, ","), nil
	default:
		return "", fmt.Errorf("unknown field browser.%s", field)
	}
}

// getBrowserProxyField returns proxy values plainly — like server.token, the
// CLI editor reads the operator's own file.
func getBrowserProxyField(p *BrowserProxyConfig, field string) (string, error) {
	if strings.HasPrefix(field, "geo.") {
		if p.Geo == nil {
			return "", nil
		}
		switch strings.TrimPrefix(field, "geo.") {
		case "timezone":
			return p.Geo.Timezone, nil
		case "locale":
			return p.Geo.Locale, nil
		case "webrtcIP":
			return p.Geo.WebRTCIP, nil
		case "countryISO":
			return p.Geo.CountryISO, nil
		default:
			return "", fmt.Errorf("unknown field proxy.%s", field)
		}
	}
	switch field {
	case "server":
		return p.Server, nil
	case "bypassList":
		return strings.Join(p.BypassList, ","), nil
	case "username":
		return p.Username, nil
	case "password":
		return p.Password, nil
	default:
		return "", fmt.Errorf("unknown field proxy.%s", field)
	}
}

func getBrowserTargetField(targets BrowserTargetsConfig, path string) (string, error) {
	name, field, ok := strings.Cut(path, ".")
	if !ok || name == "" || field == "" {
		return "", fmt.Errorf("invalid browser.targets path %q (expected targets.<name>.<field>)", path)
	}
	t, ok := targets[name]
	if !ok {
		return "", fmt.Errorf("browser target %q not found", name)
	}
	switch {
	case strings.HasPrefix(field, "cloak."):
		return getCloakBrowserField(&t.Cloak, strings.TrimPrefix(field, "cloak."))
	case strings.HasPrefix(field, "proxy."):
		return getBrowserProxyField(&t.Proxy, strings.TrimPrefix(field, "proxy."))
	case field == "provider":
		return t.Provider, nil
	case field == "binary":
		return t.Binary, nil
	case field == "extraFlags":
		return t.ExtraFlags, nil
	default:
		return "", fmt.Errorf("unknown field browser.targets.%s.%s", name, field)
	}
}

func getBrowsersField(b *BrowsersConfig, field string) (string, error) {
	switch field {
	case "default":
		return b.Default, nil
	case "available":
		return strings.Join(b.Available, ","), nil
	default:
		return "", fmt.Errorf("unknown field browsers.%s", field)
	}
}

func getCloakBrowserField(c *CloakBrowserConfig, field string) (string, error) {
	switch field {
	case "fingerprintSeed":
		return c.FingerprintSeed, nil
	case "platform":
		return c.Platform, nil
	case "locale":
		return c.Locale, nil
	case "timezone":
		return c.Timezone, nil
	case "webrtcIP":
		return c.WebRTCIP, nil
	case "fontsDir":
		return c.FontsDir, nil
	case "storageQuotaMB":
		return formatIntPtr(c.StorageQuotaMB), nil
	case "disableDefaultStealthArgs":
		return formatBoolPtr(c.DisableDefaultStealthArgs), nil
	default:
		return "", fmt.Errorf("unknown field browser.cloak.%s", field)
	}
}

func getObservabilityField(o *ObservabilityFileConfig, field string) (string, error) {
	if strings.HasPrefix(field, "activity.") {
		return getActivityField(&o.Activity, strings.TrimPrefix(field, "activity."))
	}
	return "", fmt.Errorf("unknown field observability.%s", field)
}

func getActivityField(a *ActivityFileConfig, field string) (string, error) {
	if strings.HasPrefix(field, "events.") {
		return getActivityEventField(&a.Events, strings.TrimPrefix(field, "events."))
	}

	switch field {
	case "enabled":
		return formatBoolPtr(a.Enabled), nil
	case "sessionIdleSec":
		return formatIntPtr(a.SessionIdleSec), nil
	case "retentionDays":
		return formatIntPtr(a.RetentionDays), nil
	case "stateDir":
		return a.StateDir, nil
	default:
		return "", fmt.Errorf("unknown field observability.activity.%s", field)
	}
}

func getActivityEventField(e *ActivityEventsFileConfig, field string) (string, error) {
	switch field {
	case "dashboard":
		return formatBoolPtr(e.Dashboard), nil
	case "server":
		return formatBoolPtr(e.Server), nil
	case "bridge":
		return formatBoolPtr(e.Bridge), nil
	case "orchestrator":
		return formatBoolPtr(e.Orchestrator), nil
	case "scheduler":
		return formatBoolPtr(e.Scheduler), nil
	case "mcp":
		return formatBoolPtr(e.MCP), nil
	case "other":
		return formatBoolPtr(e.Other), nil
	default:
		return "", fmt.Errorf("unknown field observability.activity.events.%s", field)
	}
}

func getSessionsField(s *SessionsFileConfig, field string) (string, error) {
	if strings.HasPrefix(field, "dashboard.") {
		return getDashboardSessionField(&s.Dashboard, strings.TrimPrefix(field, "dashboard."))
	}
	return "", fmt.Errorf("unknown field sessions.%s", field)
}

func getDashboardSessionField(s *DashboardSessionFileConfig, field string) (string, error) {
	switch field {
	case "persist":
		return formatBoolPtr(s.Persist), nil
	case "idleTimeoutSec":
		return formatIntPtr(s.IdleTimeoutSec), nil
	case "maxLifetimeSec":
		return formatIntPtr(s.MaxLifetimeSec), nil
	case "elevationWindowSec":
		return formatIntPtr(s.ElevationWindowSec), nil
	case "persistElevationAcrossRestart":
		return formatBoolPtr(s.PersistElevationAcrossRestart), nil
	case "requireElevation":
		return formatBoolPtr(s.RequireElevation), nil
	default:
		return "", fmt.Errorf("unknown field sessions.dashboard.%s", field)
	}
}

func getInstanceDefaultsField(c *InstanceDefaultsConfig, field string) (string, error) {
	if after, ok := strings.CutPrefix(field, "tabPolicy."); ok {
		return getTabPolicyField(c.TabPolicy, after)
	}
	switch field {
	case "mode":
		return c.Mode, nil
	case "noRestore":
		return formatBoolPtr(c.NoRestore), nil
	case "timezone":
		return c.Timezone, nil
	case "blockImages":
		return formatBoolPtr(c.BlockImages), nil
	case "blockMedia":
		return formatBoolPtr(c.BlockMedia), nil
	case "blockAds":
		return formatBoolPtr(c.BlockAds), nil
	case "maxTabs":
		return formatIntPtr(c.MaxTabs), nil
	case "maxParallelTabs":
		return formatIntPtr(c.MaxParallelTabs), nil
	case "userAgent":
		return c.UserAgent, nil
	case "noAnimations":
		return formatBoolPtr(c.NoAnimations), nil
	case "captureAllowActivation":
		return formatBoolPtr(c.CaptureAllowActivation), nil
	case "humanize":
		return formatBoolPtr(c.Humanize), nil
	case "stealthLevel":
		return c.StealthLevel, nil
	case "tabEvictionPolicy":
		return c.TabEvictionPolicy, nil
	default:
		return "", fmt.Errorf("unknown field instanceDefaults.%s", field)
	}
}

func getTabPolicyField(tp *TabPolicyDefaults, field string) (string, error) {
	if tp == nil {
		tp = &TabPolicyDefaults{}
	}
	switch field {
	case "eviction":
		return tp.Eviction, nil
	case "lifecycle":
		return tp.Lifecycle, nil
	case "closeDelaySec":
		return formatIntPtr(tp.CloseDelaySec), nil
	default:
		return "", fmt.Errorf("unknown field instanceDefaults.tabPolicy.%s", field)
	}
}

func getSecurityField(s *SecurityConfig, field string) (string, error) {
	if strings.HasPrefix(field, "attach.") {
		return getAttachField(&s.Attach, strings.TrimPrefix(field, "attach."))
	}
	if strings.HasPrefix(field, "idpi.") {
		return getIDPIField(&s.IDPI, strings.TrimPrefix(field, "idpi."))
	}

	switch field {
	case "allowEvaluate":
		return formatBoolPtr(s.AllowEvaluate), nil
	case "allowClipboard":
		return formatBoolPtr(s.AllowClipboard), nil
	case "allowMacro":
		return formatBoolPtr(s.AllowMacro), nil
	case "allowScreencast":
		return formatBoolPtr(s.AllowScreencast), nil
	case "allowDownload":
		return formatBoolPtr(s.AllowDownload), nil
	case "allowCookies":
		return formatBoolPtr(s.AllowCookies), nil
	case "allowStateExport":
		return formatBoolPtr(s.AllowStateExport), nil
	case "allowNetworkIntercept":
		return formatBoolPtr(s.AllowNetworkIntercept), nil
	case "allowFileScheme":
		return formatBoolPtr(s.AllowFileScheme), nil
	case "allowedDomains":
		return strings.Join(s.AllowedDomains, ","), nil
	case "downloadAllowedDomains":
		return strings.Join(s.DownloadAllowedDomains, ","), nil
	case "downloadMaxBytes":
		return formatIntPtr(s.DownloadMaxBytes), nil
	case "allowUpload":
		return formatBoolPtr(s.AllowUpload), nil
	case "enableActionGuards":
		return formatBoolPtr(s.EnableActionGuards), nil
	case "uploadMaxRequestBytes":
		return formatIntPtr(s.UploadMaxRequestBytes), nil
	case "uploadMaxFiles":
		return formatIntPtr(s.UploadMaxFiles), nil
	case "uploadMaxFileBytes":
		return formatIntPtr(s.UploadMaxFileBytes), nil
	case "uploadMaxTotalBytes":
		return formatIntPtr(s.UploadMaxTotalBytes), nil
	case "maxRedirects":
		return formatIntPtr(s.MaxRedirects), nil
	case "trustedProxyCIDRs":
		return strings.Join(s.TrustedProxyCIDRs, ","), nil
	case "trustedResolveCIDRs":
		return strings.Join(s.TrustedResolveCIDRs, ","), nil
	case "trustLoopbackProxy":
		return formatBoolPtr(s.TrustLoopbackProxy), nil
	default:
		return "", fmt.Errorf("unknown field security.%s", field)
	}
}

func getProfilesField(p *ProfilesConfig, field string) (string, error) {
	switch field {
	case "baseDir":
		return p.BaseDir, nil
	case "defaultProfile":
		return p.DefaultProfile, nil
	default:
		return "", fmt.Errorf("unknown field profiles.%s", field)
	}
}

func getMultiInstanceField(o *MultiInstanceConfig, field string) (string, error) {
	if strings.HasPrefix(field, "restart.") {
		return getMultiInstanceRestartField(&o.Restart, strings.TrimPrefix(field, "restart."))
	}

	switch field {
	case "strategy":
		return o.Strategy, nil
	case "allocationPolicy":
		return o.AllocationPolicy, nil
	case "instancePortStart":
		return formatIntPtr(o.InstancePortStart), nil
	case "instancePortEnd":
		return formatIntPtr(o.InstancePortEnd), nil
	default:
		return "", fmt.Errorf("unknown field multiInstance.%s", field)
	}
}

func getMultiInstanceRestartField(r *MultiInstanceRestartConfig, field string) (string, error) {
	switch field {
	case "maxRestarts":
		return formatIntPtr(r.MaxRestarts), nil
	case "initBackoffSec":
		return formatIntPtr(r.InitBackoffSec), nil
	case "maxBackoffSec":
		return formatIntPtr(r.MaxBackoffSec), nil
	case "stableAfterSec":
		return formatIntPtr(r.StableAfterSec), nil
	default:
		return "", fmt.Errorf("unknown field multiInstance.restart.%s", field)
	}
}

func getAttachField(a *AttachConfig, field string) (string, error) {
	switch field {
	case "enabled":
		return formatBoolPtr(a.Enabled), nil
	case "allowHosts":
		return strings.Join(a.AllowHosts, ","), nil
	case "allowSchemes":
		return strings.Join(a.AllowSchemes, ","), nil
	case "forwardProxyAuth":
		return formatBoolPtr(a.ForwardProxyAuth), nil
	default:
		return "", fmt.Errorf("unknown field security.attach.%s", field)
	}
}

func getIDPIField(i *IDPIConfig, field string) (string, error) {
	switch field {
	case "enabled":
		return strconv.FormatBool(i.Enabled), nil
	case "strictMode":
		return strconv.FormatBool(i.StrictMode), nil
	case "scanContent":
		return strconv.FormatBool(i.ScanContent), nil
	case "wrapContent":
		return strconv.FormatBool(i.WrapContent), nil
	case "customPatterns":
		return strings.Join(i.CustomPatterns, ","), nil
	default:
		return "", fmt.Errorf("unknown field security.idpi.%s", field)
	}
}

func getTimeoutsField(t *TimeoutsConfig, field string) (string, error) {
	switch field {
	case "actionSec":
		return strconv.Itoa(t.ActionSec), nil
	case "navigateSec":
		return strconv.Itoa(t.NavigateSec), nil
	case "shutdownSec":
		return strconv.Itoa(t.ShutdownSec), nil
	case "waitNavMs":
		return strconv.Itoa(t.WaitNavMs), nil
	default:
		return "", fmt.Errorf("unknown field timeouts.%s", field)
	}
}

func formatBoolPtr(b *bool) string {
	if b == nil {
		return ""
	}
	if *b {
		return "true"
	}
	return "false"
}

func formatIntPtr(n *int) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(*n)
}

func getSchedulerField(s *SchedulerFileConfig, field string) (string, error) {
	switch field {
	case "enabled":
		return formatBoolPtr(s.Enabled), nil
	case "strategy":
		return s.Strategy, nil
	case "maxQueueSize":
		return formatIntPtr(s.MaxQueueSize), nil
	case "maxPerAgent":
		return formatIntPtr(s.MaxPerAgent), nil
	case "maxInflight":
		return formatIntPtr(s.MaxInflight), nil
	case "maxPerAgentInflight":
		return formatIntPtr(s.MaxPerAgentFlight), nil
	case "resultTTLSec":
		return formatIntPtr(s.ResultTTLSec), nil
	case "workerCount":
		return formatIntPtr(s.WorkerCount), nil
	default:
		return "", fmt.Errorf("unknown field scheduler.%s", field)
	}
}

func getAutoSolverField(a *AutoSolverFileConfig, field string) (string, error) {
	if rest, ok := strings.CutPrefix(field, "external."); ok {
		return getAutoSolverExternalField(&a.External, rest)
	}
	if rest, ok := strings.CutPrefix(field, "credentials."); ok {
		return getAutoSolverCredentialsField(&a.Credentials, rest)
	}

	switch field {
	case "enabled":
		return formatBoolPtr(a.Enabled), nil
	case "autoTrigger":
		return formatBoolPtr(a.AutoTrigger), nil
	case "triggerOnNavigate":
		return formatBoolPtr(a.TriggerOnNavigate), nil
	case "triggerOnAction":
		return formatBoolPtr(a.TriggerOnAction), nil
	case "llmFallback":
		return formatBoolPtr(a.LLMFallback), nil
	case "maxAttempts":
		return formatIntPtr(a.MaxAttempts), nil
	case "solverTimeoutSec":
		return formatIntPtr(a.SolverTimeoutSec), nil
	case "retryBaseDelayMs":
		return formatIntPtr(a.RetryBaseDelayMs), nil
	case "retryMaxDelayMs":
		return formatIntPtr(a.RetryMaxDelayMs), nil
	case "llmProvider":
		return a.LLMProvider, nil
	case "solvers":
		return strings.Join(a.Solvers, ","), nil
	default:
		return "", fmt.Errorf("unknown field autoSolver.%s", field)
	}
}

func getAutoSolverExternalField(e *AutoSolverExtConf, field string) (string, error) {
	switch field {
	case "capsolverKey":
		return e.CapsolverKey, nil
	case "twoCaptchaKey":
		return e.TwoCaptchaKey, nil
	default:
		return "", fmt.Errorf("unknown field autoSolver.external.%s", field)
	}
}

func getAutoSolverCredentialsField(c *AutoSolverCredentialsConf, field string) (string, error) {
	switch {
	case strings.HasPrefix(field, "login."):
		switch strings.TrimPrefix(field, "login.") {
		case "user":
			return c.Login.User, nil
		case "password":
			return c.Login.Password, nil
		}
	case strings.HasPrefix(field, "signup."):
		switch strings.TrimPrefix(field, "signup.") {
		case "name":
			return c.Signup.Name, nil
		case "email":
			return c.Signup.Email, nil
		case "password":
			return c.Signup.Password, nil
		}
	case strings.HasPrefix(field, "form."):
		switch strings.TrimPrefix(field, "form.") {
		case "field1":
			return c.Form.Field1, nil
		case "field2":
			return c.Form.Field2, nil
		case "email":
			return c.Form.Email, nil
		}
	}
	return "", fmt.Errorf("unknown field autoSolver.credentials.%s", field)
}
