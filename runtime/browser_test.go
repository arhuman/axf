package runtime_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/arhuman/axf/runtime"
)

// The command table is what a real launch depends on, so it is checked for both
// supported platforms and both engines without a browser ever running.
func TestBrowserProfileProviderLaunch(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		engine    string
		installed []string
		wantName  string
		// wantEngineDir is the engine actually chosen, which names the profile
		// directory: two engines must never share one.
		wantEngineDir string
		wantArgs      func(dir string) []string
	}{
		{
			name:          "firefox on linux",
			goos:          "linux",
			engine:        "firefox",
			installed:     []string{"firefox"},
			wantName:      "firefox",
			wantEngineDir: "firefox",
			wantArgs:      func(dir string) []string { return []string{"--profile", dir} },
		},
		{
			name:          "chrome on linux",
			goos:          "linux",
			engine:        "chrome",
			installed:     []string{"google-chrome"},
			wantName:      "google-chrome",
			wantEngineDir: "chrome",
			wantArgs:      func(dir string) []string { return []string{"--user-data-dir=" + dir} },
		},
		{
			name:          "chromium stands in for chrome on linux",
			goos:          "linux",
			engine:        "chrome",
			installed:     []string{"chromium"},
			wantName:      "chromium",
			wantEngineDir: "chrome",
			wantArgs:      func(dir string) []string { return []string{"--user-data-dir=" + dir} },
		},
		{
			name:          "firefox on darwin starts a fresh instance",
			goos:          "darwin",
			engine:        "firefox",
			installed:     []string{"/Applications/Firefox.app"},
			wantName:      "open",
			wantEngineDir: "firefox",
			wantArgs: func(dir string) []string {
				return []string{"-na", "Firefox", "--args", "--profile", dir}
			},
		},
		{
			name:          "chrome on darwin",
			goos:          "darwin",
			engine:        "chrome",
			installed:     []string{"/Applications/Google Chrome.app"},
			wantName:      "open",
			wantEngineDir: "chrome",
			wantArgs: func(dir string) []string {
				return []string{"-na", "Google Chrome", "--args", "--user-data-dir=" + dir}
			},
		},
		{
			name:          "auto prefers firefox when both are installed",
			goos:          "linux",
			engine:        "auto",
			installed:     []string{"firefox", "google-chrome"},
			wantName:      "firefox",
			wantEngineDir: "firefox",
			wantArgs:      func(dir string) []string { return []string{"--profile", dir} },
		},
		{
			name:          "auto falls back to chrome",
			goos:          "linux",
			engine:        "auto",
			installed:     []string{"google-chrome"},
			wantName:      "google-chrome",
			wantEngineDir: "chrome",
			wantArgs:      func(dir string) []string { return []string{"--user-data-dir=" + dir} },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			launcher := &fakeLauncher{installed: tt.installed}
			provider := runtime.BrowserProfileProvider{Launcher: launcher, GOOS: tt.goos}
			target := runtime.Target{Name: "alchemist", Home: home}
			cfg := runtime.Config{"engine": json.RawMessage(`"` + tt.engine + `"`)}

			if err := provider.Run(target, "launch", cfg); err != nil {
				t.Fatalf("Run(launch) error = %v", err)
			}
			if len(launcher.launches) != 1 {
				t.Fatalf("launches = %v, want exactly one", launcher.launches)
			}
			got := launcher.launches[0]
			if got.Name != tt.wantName {
				t.Errorf("launched %q, want %q", got.Name, tt.wantName)
			}
			dir := filepath.Join(home, "profiles", "alchemist", tt.wantEngineDir)
			if want := tt.wantArgs(dir); !slices.Equal(got.Args, want) {
				t.Errorf("args = %v, want %v", got.Args, want)
			}
			if _, err := os.Stat(dir); err != nil {
				t.Errorf("profile directory %s: %v", dir, err)
			}
		})
	}
}

// Activate isolates the profile and exports it, but must not start a browser:
// re-activating the same Alter in a second terminal would otherwise open a
// second window.
func TestBrowserProfileProviderActivateDoesNotLaunch(t *testing.T) {
	home := t.TempDir()
	launcher := &fakeLauncher{installed: []string{"firefox"}}
	provider := runtime.BrowserProfileProvider{Launcher: launcher, GOOS: "linux"}
	target := runtime.Target{Name: "alchemist", Home: home}

	result, err := provider.Activate(target, nil)
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if len(launcher.launches) != 0 {
		t.Errorf("launches = %v, want none", launcher.launches)
	}
	want := filepath.Join(home, "profiles", "alchemist", "firefox")
	if result.Env["AXF_BROWSER_PROFILE"] != want {
		t.Errorf("AXF_BROWSER_PROFILE = %q, want %q", result.Env["AXF_BROWSER_PROFILE"], want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("profile directory %s: %v", want, err)
	}
}

func TestBrowserProfileProviderProfileDirOverride(t *testing.T) {
	home := t.TempDir()
	override := filepath.Join(home, "profiles", "elsewhere")
	provider := runtime.BrowserProfileProvider{
		Launcher: &fakeLauncher{installed: []string{"firefox"}},
		GOOS:     "linux",
	}
	cfg := runtime.Config{"profileDir": json.RawMessage(`"` + override + `"`)}

	result, err := provider.Activate(runtime.Target{Name: "alchemist", Home: home}, cfg)
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if result.Env["AXF_BROWSER_PROFILE"] != override {
		t.Errorf("AXF_BROWSER_PROFILE = %q, want the configured override %q",
			result.Env["AXF_BROWSER_PROFILE"], override)
	}
}

// profileDir renames the leaf of the default location; it must not be a way
// to point the isolated profile at a directory outside $AXF_HOME/profiles,
// such as the user's real browser profile.
func TestBrowserProfileProviderProfileDirRejectsEscape(t *testing.T) {
	home := t.TempDir()
	provider := runtime.BrowserProfileProvider{
		Launcher: &fakeLauncher{installed: []string{"firefox"}},
		GOOS:     "linux",
	}
	target := runtime.Target{Name: "alchemist", Home: home}

	for _, override := range []string{
		filepath.Join(home, "elsewhere"),           // sibling of profiles, not inside it
		home + "/profiles/../escaped",              // climbs back out via ..
		filepath.Join(t.TempDir(), "real-browser"), // unrelated directory entirely
	} {
		t.Run(override, func(t *testing.T) {
			cfg := runtime.Config{"profileDir": json.RawMessage(`"` + override + `"`)}
			_, err := provider.Activate(target, cfg)
			if !errors.Is(err, runtime.ErrInvalidProfileDir) {
				t.Errorf("Activate() error = %v, want it to wrap ErrInvalidProfileDir", err)
			}
		})
	}
}

func TestBrowserProfileProviderErrors(t *testing.T) {
	home := t.TempDir()
	target := runtime.Target{Name: "alchemist", Home: home}
	tests := []struct {
		name      string
		goos      string
		installed []string
		cfg       runtime.Config
		want      string
	}{
		{
			name: "no browser installed",
			goos: "linux",
			want: "no auto browser found",
		},
		{
			name:      "unknown engine",
			goos:      "linux",
			installed: []string{"firefox"},
			cfg:       runtime.Config{"engine": json.RawMessage(`"lynx"`)},
			want:      `unknown engine "lynx"`,
		},
		{
			name:      "unsupported platform",
			goos:      "windows",
			installed: []string{"firefox"},
			want:      "axf v1 targets darwin and linux",
		},
		{
			name: "engine of the wrong type",
			goos: "linux",
			cfg:  runtime.Config{"engine": json.RawMessage(`true`)},
			want: `config "engine" must be a string`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := runtime.BrowserProfileProvider{
				Launcher: &fakeLauncher{installed: tt.installed},
				GOOS:     tt.goos,
			}
			_, err := provider.Activate(target, tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Activate() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// down must be able to unset the profile variable without probing the host for
// a browser that may since have been uninstalled.
func TestBrowserProfileProviderEnvVarNamesTouchesNothing(t *testing.T) {
	provider := runtime.BrowserProfileProvider{Launcher: &fakeLauncher{}, GOOS: "windows"}
	got := provider.EnvVarNames(runtime.Target{}, nil)
	if !slices.Equal(got, []string{"AXF_BROWSER_PROFILE"}) {
		t.Errorf("EnvVarNames() = %v, want [AXF_BROWSER_PROFILE]", got)
	}
}

func TestBrowserProfileProviderRunRejectsOtherActions(t *testing.T) {
	provider := runtime.BrowserProfileProvider{Launcher: &fakeLauncher{}, GOOS: "linux"}
	err := provider.Run(runtime.Target{}, "close", nil)
	if !errors.Is(err, runtime.ErrActionNotImplemented) {
		t.Errorf("Run() = %v, want it to wrap ErrActionNotImplemented", err)
	}
}
