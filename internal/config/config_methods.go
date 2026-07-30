package config

import "path/filepath"

// EnabledSensitiveEndpoints returns the names of sensitive endpoint families
// that are currently enabled in the runtime configuration.
func (cfg *RuntimeConfig) EnabledSensitiveEndpoints() []string {
	if cfg == nil {
		return nil
	}

	enabled := make([]string, 0, 7)
	if cfg.AllowEvaluate {
		enabled = append(enabled, "evaluate")
	}
	if cfg.AllowMacro {
		enabled = append(enabled, "macro")
	}
	if cfg.AllowScreencast {
		enabled = append(enabled, "screencast")
	}
	if cfg.AllowDownload {
		enabled = append(enabled, "download")
	}
	if cfg.AllowCookies {
		enabled = append(enabled, "cookies")
	}
	if cfg.AllowUpload {
		enabled = append(enabled, "upload")
	}
	if cfg.AllowNetworkIntercept {
		enabled = append(enabled, "networkIntercept")
	}
	return enabled
}

// ActivityStateDir returns the directory root used for activity log storage.
// When unset, activity logs live under the main server state directory.
func (cfg *RuntimeConfig) ActivityStateDir() string {
	if cfg == nil {
		return ""
	}
	return cfg.StateDir
}

// ActivityLogDir is where the activity store keeps its files. This package owns the
// state-directory layout — the same rule as profiles.baseDir — so the store is handed
// the directory rather than appending its own name to a parent, and `config get` can
// report it without a second join.
func (cfg *RuntimeConfig) ActivityLogDir() string {
	root := cfg.ActivityStateDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, activityLogSubdir)
}

const activityLogSubdir = "activity"

// activityStateDirReason is the fact both surfaces below state, written once. A user
// reaching for observability.activity.stateDir wants the logs elsewhere and deserves to
// learn why that is not on offer, rather than being told only that the key is wrong.
const activityStateDirReason = "activity logs are always written to <server.stateDir>/" + activityLogSubdir +
	" so two instances cannot share a log directory"

// ActivityStateDirRefusal is what `config set` answers. It ends in an action because there
// is one: server.stateDir moves the logs, and `config set server.stateDir <path>` performs
// it.
const ActivityStateDirRefusal = "observability.activity.stateDir is not settable: " + activityStateDirReason +
	"; set server.stateDir to move them"

// ActivityStateDirAdvisory is what a FILE already carrying the key is told, and it
// instructs nothing on purpose. The key is inert, so there is nothing the reader must do —
// and the wording it replaces ("remove the key") named an action no shipped command
// performs, while riding a diagnostic that aborted every later config write off a TTY.
const ActivityStateDirAdvisory = "observability.activity.stateDir is ignored: " + activityStateDirReason
