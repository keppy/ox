package main

// Hermes Agent hooks are shell hooks in the profile's config.yaml — there is
// no per-project hook file — so every operation here is user-scoped. The
// external ox-adapter-hermes binary owns the YAML editing.

// hermesHookConsentHint explains the one Hermes-specific step left after
// install: shell hooks need first-use consent, which non-interactive surfaces
// (gateway, cron, desktop background sessions) cannot give.
const hermesHookConsentHint = "Hermes asks once per hook the first time it fires in an interactive session. " +
	"To pre-approve for the gateway and desktop app: hermes config set hooks_auto_accept true"

func installHermesHooks() error   { return installExternalAdapterHooks("hermes", true) }
func uninstallHermesHooks() error { return uninstallExternalAdapterHooks("hermes", true) }
func hasHermesHooks() bool        { return checkExternalAdapterHooks("hermes", true) }
