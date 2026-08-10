package runtime_test

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/arhuman/axf/runtime"
)

// up activates name against a home holding the named fixtures and returns the
// script the calling shell would evaluate.
func up(t *testing.T, home, active, name string, reg runtime.Registry) (string, error) {
	t.Helper()
	rt := runtime.New(home, active, reg)
	var out bytes.Buffer
	err := rt.Up(name, &out)
	return out.String(), err
}

// down deactivates the active Alter and returns the script.
func down(t *testing.T, home, active string, reg runtime.Registry) (string, error) {
	t.Helper()
	rt := runtime.New(home, active, reg)
	var out bytes.Buffer
	err := rt.Down(&out)
	return out.String(), err
}

// noBrowser is a registry whose browser provider can never launch anything, so
// a test that does not care about browsers cannot spawn one.
func noBrowser() runtime.Registry {
	return runtime.DefaultRegistry(&fakeLauncher{})
}

// The printed script is evaluated verbatim by another shell, so its exact text
// is part of the contract: order, quoting and the trailing active marker.
func TestUp(t *testing.T) {
	tests := []struct {
		name     string
		fixtures []string
		active   string
		activate string
		want     string
	}{
		{
			name:     "exports every capability then the active marker",
			fixtures: []string{"alchemist"},
			activate: "alchemist",
			want: "export GIT_AUTHOR_EMAIL='alchemist@example.com'\n" +
				"export GIT_AUTHOR_NAME='Alchemist O'\\''Brien'\n" +
				"export GIT_COMMITTER_EMAIL='alchemist@example.com'\n" +
				"export GIT_COMMITTER_NAME='Alchemist O'\\''Brien'\n" +
				"export AXF_ALTER_NAME='alchemist'\n" +
				"export AXF_PROMPT_LABEL='alchemist prime'\n" +
				"export AXF_ACTIVE_ALTER='alchemist'\n",
		},
		{
			name:     "re-activating the active Alter deactivates nothing",
			fixtures: []string{"researcher"},
			active:   "researcher",
			activate: "researcher",
			want: "export AXF_ALTER_NAME='researcher'\n" +
				"export AXF_PROMPT_LABEL='researcher'\n" +
				"export AXF_ACTIVE_ALTER='researcher'\n",
		},
		{
			name:     "switching Alters unsets the previous one first",
			fixtures: []string{"alchemist", "researcher"},
			active:   "alchemist",
			activate: "researcher",
			want: "unset GIT_AUTHOR_EMAIL\n" +
				"unset GIT_AUTHOR_NAME\n" +
				"unset GIT_COMMITTER_EMAIL\n" +
				"unset GIT_COMMITTER_NAME\n" +
				"unset AXF_ALTER_NAME\n" +
				"unset AXF_PROMPT_LABEL\n" +
				"unset AXF_ACTIVE_ALTER\n" +
				"export AXF_ALTER_NAME='researcher'\n" +
				"export AXF_PROMPT_LABEL='researcher'\n" +
				"export AXF_ACTIVE_ALTER='researcher'\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := newHome(t, tt.fixtures...)
			got, err := up(t, home, tt.active, tt.activate, noBrowser())
			if err != nil {
				t.Fatalf("Up() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("script =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// A capability with no provider must stop the activation before anything is
// printed: a half applied identity would let the user believe a capability is
// in place when it is not.
func TestUpFailsClosedOnAnUnimplementedCapability(t *testing.T) {
	home := newHome(t, "ai-only")
	got, err := up(t, home, "", "ai-only", noBrowser())
	if !errors.Is(err, runtime.ErrProviderNotImplemented) {
		t.Fatalf("Up() error = %v, want it to wrap ErrProviderNotImplemented", err)
	}
	if !strings.Contains(err.Error(), "requires asset decryption") {
		t.Errorf("Up() error = %v, want it to say why ai-account has no provider", err)
	}
	if got != "" {
		t.Errorf("script = %q, want nothing printed when activation fails", got)
	}
}

// A hook the runtime cannot honour is reported but must not prevent the rest of
// the activation: the printed script stays correct and safe to evaluate.
func TestUpDegradesOnAnUnimplementedHook(t *testing.T) {
	home := newHome(t, "hooked")
	got, err := up(t, home, "", "hooked", noBrowser())
	if !errors.Is(err, runtime.ErrProviderNotImplemented) {
		t.Fatalf("Up() error = %v, want it to wrap ErrProviderNotImplemented", err)
	}
	if !strings.Contains(err.Error(), "ai-account") {
		t.Errorf("Up() error = %v, want it to name the failing hook", err)
	}
	want := "export AXF_ALTER_NAME='hooked'\n" +
		"export AXF_PROMPT_LABEL='hooked'\n" +
		"export AXF_ACTIVE_ALTER='hooked'\n"
	if got != want {
		t.Errorf("script =\n%s\nwant\n%s", got, want)
	}
}

// The runtime activates no document the v0 SDK would reject.
func TestUpRefusesADocumentFailingValidation(t *testing.T) {
	home := newHome(t, "duplicate-assets")
	got, err := up(t, home, "", "duplicate-assets", noBrowser())
	if err == nil || !strings.Contains(err.Error(), "duplicate asset name") {
		t.Fatalf("Up() error = %v, want the conformance violation", err)
	}
	if got != "" {
		t.Errorf("script = %q, want nothing printed", got)
	}
}

func TestUpReportsAMissingAlter(t *testing.T) {
	home := newHome(t)
	_, err := up(t, home, "", "ghost", noBrowser())
	if !errors.Is(err, runtime.ErrAlterNotFound) {
		t.Fatalf("Up() error = %v, want it to wrap ErrAlterNotFound", err)
	}
	if !strings.Contains(err.Error(), "looked for") {
		t.Errorf("Up() error = %v, want the path it looked at", err)
	}
}

// A hook may target a capability the Alter does not declare, the same latitude
// policies[] have. Here the browser launch hook does declare one, and must
// reach the launcher with the isolated profile directory.
func TestUpRunsTheBrowserLaunchHook(t *testing.T) {
	home := newHome(t, "browser")
	launcher := &fakeLauncher{installed: []string{"firefox"}}
	reg := runtime.Registry{
		"browser-profile": runtime.BrowserProfileProvider{Launcher: launcher, GOOS: "linux"},
	}
	got, err := up(t, home, "", "browser", reg)
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	if len(launcher.launches) != 1 {
		t.Fatalf("launches = %v, want exactly one", launcher.launches)
	}
	if launcher.launches[0].Name != "firefox" {
		t.Errorf("launched %q, want firefox", launcher.launches[0].Name)
	}
	if !strings.Contains(got, "export AXF_BROWSER_PROFILE=") {
		t.Errorf("script = %q, want the profile directory exported", got)
	}
}

func TestDown(t *testing.T) {
	tests := []struct {
		name     string
		fixtures []string
		active   string
		want     string
		wantErr  bool
	}{
		{
			name:     "unsets every capability variable and the active marker",
			fixtures: []string{"alchemist"},
			active:   "alchemist",
			want: "unset GIT_AUTHOR_EMAIL\n" +
				"unset GIT_AUTHOR_NAME\n" +
				"unset GIT_COMMITTER_EMAIL\n" +
				"unset GIT_COMMITTER_NAME\n" +
				"unset AXF_ALTER_NAME\n" +
				"unset AXF_PROMPT_LABEL\n" +
				"unset AXF_ACTIVE_ALTER\n",
		},
		{
			// down must never be the command that leaves a shell stuck, so a
			// capability it cannot resolve is reported and skipped.
			name:     "skips a capability with no provider but still clears the rest",
			fixtures: []string{"ai-only"},
			active:   "ai-only",
			want:     "unset AXF_ALTER_NAME\nunset AXF_ACTIVE_ALTER\n",
			wantErr:  true,
		},
		{
			name:     "still clears the active marker when the document is gone",
			fixtures: nil,
			active:   "vanished",
			want:     "unset AXF_ACTIVE_ALTER\n",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := newHome(t, tt.fixtures...)
			got, err := down(t, home, tt.active, noBrowser())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Down() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("script =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// Deactivating with nothing active is a no-op, not a failure: a shell rc may
// call it unconditionally.
func TestDownWithNothingActive(t *testing.T) {
	got, err := down(t, newHome(t), "", noBrowser())
	if !errors.Is(err, runtime.ErrNoActiveAlter) {
		t.Fatalf("Down() error = %v, want ErrNoActiveAlter", err)
	}
	if got != "" {
		t.Errorf("script = %q, want nothing printed", got)
	}
}

// A hook may target a capability absent from capabilities[], in which case the
// provider receives a nil config rather than being skipped.
func TestPreDeactivationHooksRunOnDown(t *testing.T) {
	home := newHome(t, "hooked")
	_, err := down(t, home, "hooked", noBrowser())
	if !errors.Is(err, runtime.ErrProviderNotImplemented) {
		t.Fatalf("Down() error = %v, want the ai-account hook reported", err)
	}
}

// rogueProvider returns a variable name that is not a shell identifier.
type rogueProvider struct{}

func (rogueProvider) Capability() string { return "shell" }

func (rogueProvider) Activate(_ runtime.Target, _ runtime.Config) (runtime.ActivationResult, error) {
	return runtime.ActivationResult{Env: map[string]string{"BAD NAME; rm -rf /": "x"}}, nil
}

func (rogueProvider) EnvVarNames(_ runtime.Target, _ runtime.Config) []string {
	return []string{"BAD NAME; rm -rf /"}
}

func (rogueProvider) Run(_ runtime.Target, _ string, _ runtime.Config) error { return nil }

// The output of up is evaluated by the caller's shell, so a provider must not
// be able to smuggle a command through a variable name.
func TestUpRejectsAnInvalidEnvironmentVariableName(t *testing.T) {
	home := newHome(t, "researcher")
	got, err := up(t, home, "", "researcher", runtime.Registry{"shell": rogueProvider{}})
	if err == nil || !strings.Contains(err.Error(), "not a valid environment variable name") {
		t.Fatalf("Up() error = %v, want the name rejected", err)
	}
	if got != "" {
		t.Errorf("script = %q, want nothing printed", got)
	}
}

// A capability the Alter Guard refuses must stop the activation before anything
// is printed, exactly like a capability with no provider: a policy denial is a
// deliberate block, not a gap this build degrades around.
func TestUpFailsClosedOnADeniedCapability(t *testing.T) {
	home := newHome(t, "guarded")
	got, err := up(t, home, "", "guarded", noBrowser())
	if !errors.Is(err, runtime.ErrDeniedByPolicy) {
		t.Fatalf("Up() error = %v, want it to wrap ErrDeniedByPolicy", err)
	}
	if !strings.Contains(err.Error(), "policies[0]") {
		t.Errorf("Up() error = %v, want it to name the policy that refused", err)
	}
	if got != "" {
		t.Errorf("script = %q, want nothing printed when the Guard denies", got)
	}
	if want := []runtime.AuditResult{runtime.ResultDenied}; !slices.Equal(results(t, home), want) {
		t.Errorf("audit results = %v, want %v", results(t, home), want)
	}
}

// A denied hook fails closed too. An unimplemented hook degrades gracefully
// (see TestUpDegradesOnAnUnimplementedHook), but that latitude is for a missing
// provider, never for a policy that says no.
func TestUpFailsClosedOnADeniedHook(t *testing.T) {
	home := newHome(t, "guarded-hook")
	got, err := up(t, home, "", "guarded-hook", noBrowser())
	if !errors.Is(err, runtime.ErrDeniedByPolicy) {
		t.Fatalf("Up() error = %v, want it to wrap ErrDeniedByPolicy", err)
	}
	if got != "" {
		t.Errorf("script = %q, want nothing printed when a hook is denied", got)
	}
	want := []runtime.AuditResult{runtime.ResultAllowed, runtime.ResultDenied}
	if !slices.Equal(results(t, home), want) {
		t.Errorf("audit results = %v, want %v", results(t, home), want)
	}
}

// A guard-scoped deny observes without enforcing: the activation runs in full
// and the trail still records what a runtime-scoped policy would have refused.
func TestUpProceedsUnderAGuardScopedDeny(t *testing.T) {
	home := newHome(t, "observed")
	got, err := up(t, home, "", "observed", noBrowser())
	if err != nil {
		t.Fatalf("Up() error = %v, want scope guard never to block", err)
	}
	want := "export AXF_ALTER_NAME='observed'\n" +
		"export AXF_PROMPT_LABEL='observed'\n" +
		"export AXF_ACTIVE_ALTER='observed'\n"
	if got != want {
		t.Errorf("script =\n%s\nwant\n%s", got, want)
	}
	if wantResults := []runtime.AuditResult{runtime.ResultDenied}; !slices.Equal(results(t, home), wantResults) {
		t.Errorf("audit results = %v, want %v", results(t, home), wantResults)
	}
}

// Deactivation is audited but never blocked. The guarded fixture denies its own
// deactivate action and its preDeactivation hook with scope runtime; both must
// be ignored, or a deny policy could strand a shell inside an Alter.
func TestDownIsNeverBlockedByAPolicy(t *testing.T) {
	home := newHome(t, "guarded")
	got, err := down(t, home, "guarded", noBrowser())
	if err != nil {
		t.Fatalf("Down() error = %v, want deactivation never refused", err)
	}
	want := "unset AXF_ALTER_NAME\n" +
		"unset AXF_PROMPT_LABEL\n" +
		"unset AXF_ACTIVE_ALTER\n"
	if got != want {
		t.Errorf("script =\n%s\nwant\n%s", got, want)
	}
	wantResults := []runtime.AuditResult{runtime.ResultAllowed, runtime.ResultAllowed}
	if !slices.Equal(results(t, home), wantResults) {
		t.Errorf("audit results = %v, want %v", results(t, home), wantResults)
	}
	for _, e := range auditTrail(t, home) {
		if e.Action != "start" && e.Action != runtime.ActionDeactivate {
			t.Errorf("audit action = %q, want the hook and the deactivation", e.Action)
		}
	}
}

// The trail covers capability actions in general, not only the ones a policy
// touched: an Alter with no policies[] at all still audits every action.
func TestUpAuditsEveryActionExactlyOnce(t *testing.T) {
	home := newHome(t, "alchemist")
	if _, err := up(t, home, "", "alchemist", noBrowser()); err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	events := auditTrail(t, home)
	if len(events) != 2 {
		t.Fatalf("audit events = %d, want one per capability", len(events))
	}
	for i, want := range []string{"git-identity", "shell"} {
		if events[i].Capability != want || events[i].Action != runtime.ActionActivate {
			t.Errorf("event %d = {%s, %s}, want {%s, %s}",
				i, events[i].Capability, events[i].Action, want, runtime.ActionActivate)
		}
		if events[i].Result != runtime.ResultAllowed || events[i].Provider != want {
			t.Errorf("event %d = %+v, want it allowed and handled by %s", i, events[i], want)
		}
		if events[i].AlterID != "urn:axf:alter:6f9a1e0e-2e0a-4a7b-9b2e-6b6a2a2e6a2e" {
			t.Errorf("event %d alterId = %q, want the document id", i, events[i].AlterID)
		}
	}
}

// The actor ties together the events of one invocation and nothing more: two
// runs of the same Alter must not be correlatable through it.
func TestActorIsPerInvocation(t *testing.T) {
	home := newHome(t, "alchemist")
	for range 2 {
		if _, err := up(t, home, "", "alchemist", noBrowser()); err != nil {
			t.Fatalf("Up() error = %v", err)
		}
	}
	events := auditTrail(t, home)
	if len(events) != 4 {
		t.Fatalf("audit events = %d, want two per invocation", len(events))
	}
	first, second := events[0].Actor, events[2].Actor
	if first == "" || events[1].Actor != first {
		t.Errorf("actors %q and %q, want one opaque actor per invocation", first, events[1].Actor)
	}
	if second == "" || events[3].Actor != second {
		t.Errorf("actors %q and %q, want one opaque actor per invocation", second, events[3].Actor)
	}
	if first == second {
		t.Errorf("both invocations recorded actor %q, want them uncorrelatable", first)
	}
}

// Switching Alters audits both documents under the same actor, each event
// carrying the metadata.id of the Alter it belongs to.
func TestUpAuditsTheAlterItTearsDownToo(t *testing.T) {
	home := newHome(t, "alchemist", "researcher")
	if _, err := up(t, home, "alchemist", "researcher", noBrowser()); err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	events := auditTrail(t, home)
	if len(events) != 3 {
		t.Fatalf("audit events = %d, want one activation and two deactivations", len(events))
	}
	ids := make(map[string]bool, 2)
	for _, e := range events {
		ids[e.AlterID] = true
		if e.Actor != events[0].Actor {
			t.Errorf("actor = %q, want the whole invocation under %q", e.Actor, events[0].Actor)
		}
	}
	if len(ids) != 2 {
		t.Errorf("alterIds = %v, want the activated and the deactivated Alter", ids)
	}
}

// blockAuditLog makes the audit trail of a home unwritable by putting a
// directory where the file belongs.
func blockAuditLog(t *testing.T, home string) {
	t.Helper()
	if err := os.Mkdir(runtime.Store{Home: home}.AuditPath(), 0o750); err != nil {
		t.Fatalf("blocking the audit log: %v", err)
	}
}

// An activation that cannot be recorded is an activation that does not happen:
// enforcement without a trail is not what an Alter Guard promises, and the
// runtime root is already required to be writable.
func TestUpFailsClosedWhenTheTrailCannotBeWritten(t *testing.T) {
	home := newHome(t, "alchemist")
	blockAuditLog(t, home)

	got, err := up(t, home, "", "alchemist", noBrowser())
	if err == nil || !strings.Contains(err.Error(), "audit log") {
		t.Fatalf("Up() error = %v, want the unwritable trail reported", err)
	}
	if got != "" {
		t.Errorf("script = %q, want nothing printed", got)
	}
}

// Deactivation makes the opposite trade: an unwritable trail is reported, but
// the shell still gets its unsets. Nothing may keep a user inside an Alter.
func TestDownStillClearsWhenTheTrailCannotBeWritten(t *testing.T) {
	home := newHome(t, "alchemist")
	blockAuditLog(t, home)

	got, err := down(t, home, "alchemist", noBrowser())
	if err == nil || !strings.Contains(err.Error(), "audit log") {
		t.Fatalf("Down() error = %v, want the unwritable trail reported", err)
	}
	if !strings.HasSuffix(got, "unset AXF_ACTIVE_ALTER\n") {
		t.Errorf("script = %q, want the deactivation printed anyway", got)
	}
}

// New must produce a usable runtime without a registry, which is what the CLI
// relies on for its default wiring.
func TestNewDefaultsToTheShippedProviders(t *testing.T) {
	rt := runtime.New(t.TempDir(), "", nil)
	for _, capability := range []string{"shell", "git-identity", "browser-profile"} {
		if _, err := rt.Registry.Lookup(capability); err != nil {
			t.Errorf("Lookup(%q) = %v, want the default provider", capability, err)
		}
	}
}
