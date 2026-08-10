package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// Browser engines accepted by capabilities[].config.engine.
const (
	EngineFirefox = "firefox"
	EngineChrome  = "chrome"
	// EngineAuto picks the first engine installed on the host. It is the
	// default when config.engine is absent.
	EngineAuto = "auto"
)

// EnvBrowserProfile carries the resolved profile directory of the active Alter.
const EnvBrowserProfile = "AXF_BROWSER_PROFILE"

// ActionLaunch is the browser-profile action starting the browser. The spec
// names it in the audit example of section 15.
const ActionLaunch = "launch"

// ErrInvalidProfileDir reports a config.profileDir override that does not
// resolve inside $AXF_HOME/profiles.
var ErrInvalidProfileDir = errors.New("runtime: invalid profileDir")

// BrowserProfileProvider implements the browser-profile capability by giving
// the Alter an isolated browser profile directory, so that cookies, sessions
// and history never cross between two Alters.
//
// Config (capabilities[].config):
//
//	{"engine": "firefox" | "chrome" | "auto", "profileDir": "/optional/path"}
//
// engine defaults to "auto", which picks the first engine installed on the
// host. profileDir overrides the default location,
// $AXF_HOME/profiles/<alter>/<engine>, but must still resolve inside
// $AXF_HOME/profiles: it renames the leaf, it does not escape the isolation
// boundary that directory provides.
//
// Activate resolves and creates the profile directory but starts no browser.
// Launching is the "launch" action, so an Alter opts into it with a lifecycle
// hook:
//
//	"postActivation": [{"capability": "browser-profile", "action": "launch"}]
//
// That split is what keeps `axf up` idempotent: re-activating the same Alter in
// a second terminal must not open a second browser window.
type BrowserProfileProvider struct {
	// Launcher starts the browser process. A nil Launcher means ExecLauncher,
	// so the zero value works and only a test has to inject anything.
	Launcher Launcher
	// GOOS overrides the platform the command table is selected for. Empty
	// means the host platform; a test sets it to check both.
	GOOS string
}

// browserCommand is one concrete way to start a browser on an isolated profile.
type browserCommand struct {
	// Engine is the canonical engine name this command starts.
	Engine string
	// Probe tells whether the command exists on this host: an absolute path to
	// stat (a macOS application bundle) or a binary name to find in PATH.
	Probe string
	// Name is the executable to run.
	Name string
	// Args builds the argument list for a profile directory. The directory is
	// only known once the engine is resolved, hence a function rather than a
	// fixed slice.
	Args func(profileDir string) []string
}

// browserCommands returns every way axf v1 knows to start a browser on goos, in
// preference order: Firefox, then Chrome, then Chromium.
//
// Keeping the table a pure function of goos is what makes browser selection
// testable for both platforms without either browser being installed.
func browserCommands(goos string) ([]browserCommand, error) {
	switch goos {
	case "darwin":
		// `open -na` starts a new instance instead of reusing the running one,
		// which is what lets two Alters browse side by side.
		return []browserCommand{
			{EngineFirefox, "/Applications/Firefox.app", "open", func(dir string) []string {
				return []string{"-na", "Firefox", "--args", "--profile", dir}
			}},
			{EngineChrome, "/Applications/Google Chrome.app", "open", func(dir string) []string {
				return []string{"-na", "Google Chrome", "--args", "--user-data-dir=" + dir}
			}},
			{EngineChrome, "/Applications/Chromium.app", "open", func(dir string) []string {
				return []string{"-na", "Chromium", "--args", "--user-data-dir=" + dir}
			}},
		}, nil
	case "linux":
		return []browserCommand{
			{EngineFirefox, "firefox", "firefox", func(dir string) []string {
				return []string{"--profile", dir}
			}},
			{EngineChrome, "google-chrome", "google-chrome", func(dir string) []string {
				return []string{"--user-data-dir=" + dir}
			}},
			{EngineChrome, "chromium", "chromium", func(dir string) []string {
				return []string{"--user-data-dir=" + dir}
			}},
		}, nil
	default:
		return nil, fmt.Errorf(
			"browser-profile: unsupported platform %q: axf v1 targets darwin and linux", goos)
	}
}

// Capability returns "browser-profile".
func (BrowserProfileProvider) Capability() string { return "browser-profile" }

// Activate resolves the engine, creates the isolated profile directory and
// exports its path. It starts no browser: see the type documentation.
func (p BrowserProfileProvider) Activate(t Target, cfg Config) (ActivationResult, error) {
	_, dir, err := p.resolve(t, cfg)
	if err != nil {
		return ActivationResult{}, err
	}
	return ActivationResult{Env: map[string]string{EnvBrowserProfile: dir}}, nil
}

// EnvVarNames returns the single profile variable. It probes nothing, so
// `axf down` unsets it without needing a browser to still be installed.
func (BrowserProfileProvider) EnvVarNames(_ Target, _ Config) []string {
	return []string{EnvBrowserProfile}
}

// Run implements the "launch" action: it starts the browser on the Alter's
// isolated profile as a background process and returns immediately, so the
// caller never waits for a browser window to be closed.
func (p BrowserProfileProvider) Run(t Target, action string, cfg Config) error {
	if action != ActionLaunch {
		return fmt.Errorf("%w: browser-profile action %q", ErrActionNotImplemented, action)
	}
	cmd, dir, err := p.resolve(t, cfg)
	if err != nil {
		return err
	}
	return p.launcher().Launch(cmd.Name, cmd.Args(dir))
}

// resolve picks the installed command for the configured engine, works out the
// profile directory and creates it.
func (p BrowserProfileProvider) resolve(t Target, cfg Config) (browserCommand, string, error) {
	engine, override, err := browserConfig(cfg)
	if err != nil {
		return browserCommand{}, "", err
	}
	cmd, err := p.selectCommand(engine)
	if err != nil {
		return browserCommand{}, "", err
	}
	dir := filepath.Join(t.Home, "profiles", t.Name, cmd.Engine)
	if override != "" {
		dir, err = resolveProfileDir(t.Home, override)
		if err != nil {
			return browserCommand{}, "", err
		}
	}
	// 0o700: the profile holds the cookies and session tokens of this Alter.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return browserCommand{}, "", fmt.Errorf("browser-profile: creating the profile directory: %w", err)
	}
	return cmd, dir, nil
}

// resolveProfileDir validates a config.profileDir override against
// $AXF_HOME/profiles and returns its absolute path.
//
// profileDir exists to rename the leaf of the default location, not to reach
// outside the isolation $AXF_HOME/profiles exists to provide: two Alters'
// cookies and session tokens must never share a directory tree, and an
// override escaping $AXF_HOME/profiles would silently defeat that guarantee
// the way an unvalidated Alter name would defeat Store's isolation of one
// document from another (see checkAlterName).
func resolveProfileDir(home, override string) (string, error) {
	abs, err := filepath.Abs(override)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrInvalidProfileDir, override, err)
	}
	base := filepath.Join(home, "profiles")
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q must resolve inside %s", ErrInvalidProfileDir, override, base)
	}
	return abs, nil
}

// selectCommand returns the first installed way to start the requested engine.
func (p BrowserProfileProvider) selectCommand(engine string) (browserCommand, error) {
	all, err := browserCommands(p.goos())
	if err != nil {
		return browserCommand{}, err
	}
	var wanted []browserCommand
	for _, cmd := range all {
		if engine == EngineAuto || cmd.Engine == engine {
			wanted = append(wanted, cmd)
		}
	}
	if len(wanted) == 0 {
		return browserCommand{}, fmt.Errorf(
			"browser-profile: unknown engine %q: want %q, %q or %q",
			engine, EngineFirefox, EngineChrome, EngineAuto)
	}
	launcher := p.launcher()
	for _, cmd := range wanted {
		if launcher.Available(cmd.Probe) {
			return cmd, nil
		}
	}
	probes := make([]string, 0, len(wanted))
	for _, cmd := range wanted {
		probes = append(probes, cmd.Probe)
	}
	return browserCommand{}, fmt.Errorf(
		"browser-profile: no %s browser found on this %s host (looked for %s)",
		engine, p.goos(), strings.Join(probes, ", "))
}

// browserConfig reads the engine and the profile directory override, applying
// the "auto" default.
func browserConfig(cfg Config) (engine, profileDir string, err error) {
	engine, err = cfg.String("engine")
	if err != nil {
		return "", "", fmt.Errorf("browser-profile: %w", err)
	}
	if engine == "" {
		engine = EngineAuto
	}
	profileDir, err = cfg.String("profileDir")
	if err != nil {
		return "", "", fmt.Errorf("browser-profile: %w", err)
	}
	return engine, profileDir, nil
}

// launcher normalises a nil dependency into the real one, so the zero value of
// the provider is usable.
func (p BrowserProfileProvider) launcher() Launcher {
	if p.Launcher == nil {
		return ExecLauncher{}
	}
	return p.Launcher
}

// goos normalises an empty override into the host platform.
func (p BrowserProfileProvider) goos() string {
	if p.GOOS == "" {
		return goruntime.GOOS
	}
	return p.GOOS
}
