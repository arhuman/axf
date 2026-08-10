package runtime_test

import (
	"encoding/json"
	"errors"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	alter "github.com/arhuman/axf"
	"github.com/arhuman/axf/runtime"
)

// guardedAlterID is the metadata.id the Guards built here record.
const guardedAlterID = "urn:axf:alter:6f9a1e0e-2e0a-4a7b-9b2e-6b6a2a2e6a2e"

// guardActor is the fixed opaque token the Guards built here record, so a test
// can assert on the exact event a Check produced.
const guardActor = "0123456789abcdef"

// newGuard builds a Guard over the given policies, writing its trail into a
// temporary home, and returns it with that home.
func newGuard(t *testing.T, policies ...alter.Policy) (*runtime.Guard, string) {
	t.Helper()
	home := t.TempDir()
	doc := &alter.Alter{
		APIVersion: alter.APIVersion,
		Kind:       alter.Kind,
		Metadata:   alter.Metadata{ID: guardedAlterID, Name: "guarded"},
		Policies:   policies,
	}
	return runtime.NewGuard(doc, guardActor, runtime.Store{Home: home}.AuditPath()), home
}

// denyOn returns a deny policy with scope runtime on {shell, activate},
// carrying cond.
func denyOn(cond *alter.Condition) alter.Policy {
	return alter.Policy{
		Capability: "shell",
		Action:     runtime.ActionActivate,
		Effect:     alter.EffectDeny,
		Scope:      alter.ScopeRuntime,
		Condition:  cond,
	}
}

// condition builds a policy condition with a raw JSON value.
func condition(t alter.ConditionType, op alter.Operator, value string) *alter.Condition {
	return &alter.Condition{Type: t, Operator: op, Value: json.RawMessage(value)}
}

// results reads back the results recorded in the trail of a home, in order.
func results(t *testing.T, home string) []runtime.AuditResult {
	t.Helper()
	events := auditTrail(t, home)
	out := make([]runtime.AuditResult, 0, len(events))
	for _, e := range events {
		out = append(out, e.Result)
	}
	return out
}

// The condition grammar is the whole of what a policy can say about when it
// applies, so every type, operator and malformed value is part of the contract.
func TestGuardEvaluatesConditions(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatalf("os.Hostname() error = %v", err)
	}

	tests := []struct {
		name string
		// active is the AXF_ACTIVE_ALTER the condition reads.
		active string
		// env is set via t.Setenv before the condition is evaluated, for
		// ConditionEnv cases naming a variable via Condition.Name.
		env        map[string]string
		cond       *alter.Condition
		wantDenied bool
		wantErr    error
		wantMsg    string
	}{
		{
			name:       "no condition always matches",
			cond:       nil,
			wantDenied: true,
		},
		{
			name:       "os equals the host",
			cond:       condition(alter.ConditionOS, alter.OperatorEquals, `"`+goruntime.GOOS+`"`),
			wantDenied: true,
		},
		{
			name:       "os equals another platform",
			cond:       condition(alter.ConditionOS, alter.OperatorEquals, `"plan9"`),
			wantDenied: false,
		},
		{
			name:       "os notEquals another platform",
			cond:       condition(alter.ConditionOS, alter.OperatorNotEquals, `"plan9"`),
			wantDenied: true,
		},
		{
			name:       "os in a list holding the host",
			cond:       condition(alter.ConditionOS, alter.OperatorIn, `["plan9","`+goruntime.GOOS+`"]`),
			wantDenied: true,
		},
		{
			name:       "os in a list without the host",
			cond:       condition(alter.ConditionOS, alter.OperatorIn, `["plan9"]`),
			wantDenied: false,
		},
		{
			name:       "os notIn a list holding the host",
			cond:       condition(alter.ConditionOS, alter.OperatorNotIn, `["`+goruntime.GOOS+`"]`),
			wantDenied: false,
		},
		{
			name:       "os notIn a list without the host",
			cond:       condition(alter.ConditionOS, alter.OperatorNotIn, `["plan9"]`),
			wantDenied: true,
		},
		{
			name:       "hostname equals the host",
			cond:       condition(alter.ConditionHostname, alter.OperatorEquals, `"`+hostname+`"`),
			wantDenied: true,
		},
		{
			name:       "hostname in a list without the host",
			cond:       condition(alter.ConditionHostname, alter.OperatorIn, `["someone-elses-laptop"]`),
			wantDenied: false,
		},
		{
			name:       "activeAlterName equals what the shell reported",
			active:     "researcher",
			cond:       condition(alter.ConditionActiveAlterName, alter.OperatorEquals, `"researcher"`),
			wantDenied: true,
		},
		{
			name:       "activeAlterName notEquals what the shell reported",
			active:     "researcher",
			cond:       condition(alter.ConditionActiveAlterName, alter.OperatorNotEquals, `"researcher"`),
			wantDenied: false,
		},
		{
			name:       "activeAlterName with nothing active",
			active:     "",
			cond:       condition(alter.ConditionActiveAlterName, alter.OperatorEquals, `""`),
			wantDenied: true,
		},
		{
			// Condition.Name absent (the zero value): the schema's conditional
			// "required" already rejects a document missing it, this exercises
			// the Guard's own defence-in-depth check.
			name:    "env with no name set is unevaluable",
			cond:    condition(alter.ConditionEnv, alter.OperatorEquals, `"production"`),
			wantErr: runtime.ErrConditionNotEvaluable,
			wantMsg: `missing "name"`,
		},
		{
			name: "env equals the named variable",
			cond: &alter.Condition{
				Type: alter.ConditionEnv, Operator: alter.OperatorEquals,
				Value: json.RawMessage(`"production"`), Name: "AXF_TEST_STAGE",
			},
			env:        map[string]string{"AXF_TEST_STAGE": "production"},
			wantDenied: true,
		},
		{
			name: "env notEquals the named variable",
			cond: &alter.Condition{
				Type: alter.ConditionEnv, Operator: alter.OperatorNotEquals,
				Value: json.RawMessage(`"production"`), Name: "AXF_TEST_STAGE",
			},
			env:        map[string]string{"AXF_TEST_STAGE": "staging"},
			wantDenied: true,
		},
		{
			name: "env reads an unset variable as empty",
			cond: &alter.Condition{
				Type: alter.ConditionEnv, Operator: alter.OperatorEquals,
				Value: json.RawMessage(`""`), Name: "AXF_TEST_STAGE_UNSET",
			},
			wantDenied: true,
		},
		{
			name:    "in with a scalar value",
			cond:    condition(alter.ConditionOS, alter.OperatorIn, `"linux"`),
			wantErr: runtime.ErrConditionNotEvaluable,
			wantMsg: "in/notIn need an array of strings",
		},
		{
			name:    "notIn with a number",
			cond:    condition(alter.ConditionOS, alter.OperatorNotIn, `42`),
			wantErr: runtime.ErrConditionNotEvaluable,
			wantMsg: "in/notIn need an array of strings",
		},
		{
			name:    "equals with an array value",
			cond:    condition(alter.ConditionOS, alter.OperatorEquals, `["linux"]`),
			wantErr: runtime.ErrConditionNotEvaluable,
			wantMsg: "equals/notEquals need a string value",
		},
		{
			name:    "an unknown condition type",
			cond:    condition("moon-phase", alter.OperatorEquals, `"waxing"`),
			wantErr: runtime.ErrConditionNotEvaluable,
			wantMsg: "unknown condition type",
		},
		{
			name:    "an unknown operator",
			cond:    condition(alter.ConditionOS, "matches", `"linux"`),
			wantErr: runtime.ErrConditionNotEvaluable,
			wantMsg: "unknown operator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(runtime.AXFActiveAlter, tt.active)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			guard, home := newGuard(t, denyOn(tt.cond))

			err := guard.Check("shell", runtime.ActionActivate, "shell")
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Check() error = %v, want it to wrap %v", err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantMsg) {
					t.Errorf("Check() error = %v, want it to contain %q", err, tt.wantMsg)
				}
			case tt.wantDenied:
				if !errors.Is(err, runtime.ErrDeniedByPolicy) {
					t.Fatalf("Check() error = %v, want it to wrap ErrDeniedByPolicy", err)
				}
			default:
				if err != nil {
					t.Fatalf("Check() error = %v, want the action allowed", err)
				}
			}

			want := runtime.ResultAllowed
			if tt.wantDenied || tt.wantErr != nil {
				want = runtime.ResultDenied
			}
			if got := results(t, home); len(got) != 1 || got[0] != want {
				t.Errorf("audit results = %v, want exactly one %q", got, want)
			}
		})
	}
}

// Deny overrides: an allow entry never clears a matching deny, whatever their
// order in policies[].
func TestGuardDenyOverridesAllow(t *testing.T) {
	allow := alter.Policy{
		Capability: "shell",
		Action:     runtime.ActionActivate,
		Effect:     alter.EffectAllow,
		Scope:      alter.ScopeRuntime,
	}
	tests := []struct {
		name     string
		policies []alter.Policy
	}{
		{name: "allow before deny", policies: []alter.Policy{allow, denyOn(nil)}},
		{name: "deny before allow", policies: []alter.Policy{denyOn(nil), allow}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard, home := newGuard(t, tt.policies...)
			err := guard.Check("shell", runtime.ActionActivate, "shell")
			if !errors.Is(err, runtime.ErrDeniedByPolicy) {
				t.Fatalf("Check() error = %v, want it to wrap ErrDeniedByPolicy", err)
			}
			if got := results(t, home); len(got) != 1 || got[0] != runtime.ResultDenied {
				t.Errorf("audit results = %v, want exactly one denied", got)
			}
		})
	}
}

// An allow-only policy set decides nothing: AXF v0 has no default-deny
// baseline, so the action proceeds exactly as it would with no policy at all.
func TestGuardAllowAloneDoesNotDecide(t *testing.T) {
	allow := alter.Policy{
		Capability: "shell",
		Action:     runtime.ActionActivate,
		Effect:     alter.EffectAllow,
		Scope:      alter.ScopeRuntime,
	}
	guard, home := newGuard(t, allow)
	if err := guard.Check("shell", runtime.ActionActivate, "shell"); err != nil {
		t.Fatalf("Check() error = %v, want the action allowed", err)
	}
	if got := results(t, home); len(got) != 1 || got[0] != runtime.ResultAllowed {
		t.Errorf("audit results = %v, want exactly one allowed", got)
	}
}

// scope separates enforcement from observability: a guard-scoped deny records
// the denial it would have enforced, and lets the action run anyway.
func TestGuardScopeGuardObservesWithoutBlocking(t *testing.T) {
	observe := denyOn(nil)
	observe.Scope = alter.ScopeGuard
	guard, home := newGuard(t, observe)

	if err := guard.Check("shell", runtime.ActionActivate, "shell"); err != nil {
		t.Fatalf("Check() error = %v, want scope guard never to block", err)
	}
	if got := results(t, home); len(got) != 1 || got[0] != runtime.ResultDenied {
		t.Errorf("audit results = %v, want exactly one denied", got)
	}
}

// The schema default for an absent scope is guard, so a deny that forgets to
// say scope observes rather than blocks.
func TestGuardDefaultScopeDoesNotBlock(t *testing.T) {
	implicit := denyOn(nil)
	implicit.Scope = ""
	guard, home := newGuard(t, implicit)

	if err := guard.Check("shell", runtime.ActionActivate, "shell"); err != nil {
		t.Fatalf("Check() error = %v, want the schema default scope not to block", err)
	}
	if got := results(t, home); len(got) != 1 || got[0] != runtime.ResultDenied {
		t.Errorf("audit results = %v, want exactly one denied", got)
	}
}

// A policy targeting another capability or another action is not a decision
// about this one.
func TestGuardIgnoresPoliciesTargetingSomethingElse(t *testing.T) {
	otherCapability := denyOn(nil)
	otherCapability.Capability = "browser-profile"
	otherAction := denyOn(nil)
	otherAction.Action = "launch"

	guard, home := newGuard(t, otherCapability, otherAction)
	if err := guard.Check("shell", runtime.ActionActivate, "shell"); err != nil {
		t.Fatalf("Check() error = %v, want an untargeted action allowed", err)
	}
	if got := results(t, home); len(got) != 1 || got[0] != runtime.ResultAllowed {
		t.Errorf("audit results = %v, want exactly one allowed", got)
	}
}

// Record is the deactivation path: it never blocks and never evaluates a
// condition, so a deny that would have matched cannot strand a shell.
func TestGuardRecordNeverBlocks(t *testing.T) {
	deny := denyOn(condition(alter.ConditionEnv, alter.OperatorEquals, `"anything"`))
	deny.Action = runtime.ActionDeactivate
	guard, home := newGuard(t, deny)

	if err := guard.Record("shell", runtime.ActionDeactivate, "shell"); err != nil {
		t.Fatalf("Record() error = %v, want deactivation never refused", err)
	}
	if got := results(t, home); len(got) != 1 || got[0] != runtime.ResultAllowed {
		t.Errorf("audit results = %v, want exactly one allowed", got)
	}
}

// The event shape is fixed by spec section 15, down to the field names another
// implementation reads. actor carries the opaque token, never the real PID.
func TestGuardWritesTheSpecEventShape(t *testing.T) {
	deny := denyOn(nil)
	deny.Capability = "browser-profile"
	deny.Action = "launch"
	guard, home := newGuard(t, deny)

	if err := guard.Check("browser-profile", "launch", "firefox"); !errors.Is(err, runtime.ErrDeniedByPolicy) {
		t.Fatalf("Check() error = %v, want it to wrap ErrDeniedByPolicy", err)
	}
	events := auditTrail(t, home)
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want exactly one", len(events))
	}
	got := events[0]
	want := runtime.AuditEvent{
		Timestamp:  got.Timestamp,
		AlterID:    guardedAlterID,
		Actor:      guardActor,
		Capability: "browser-profile",
		Action:     "launch",
		Provider:   "firefox",
		Result:     runtime.ResultDenied,
	}
	if got != want {
		t.Errorf("event = %+v, want %+v", got, want)
	}
	if _, err := time.Parse(time.RFC3339, got.Timestamp); err != nil {
		t.Errorf("timestamp %q is not RFC3339: %v", got.Timestamp, err)
	}
	if strings.Contains(got.Actor, strconv.Itoa(os.Getpid())) {
		t.Errorf("actor %q leaks the real PID", got.Actor)
	}
}

// NewActor must not hand out the same token twice: an audit trail whose actor
// were stable across invocations would correlate them.
func TestNewActorIsUnpredictable(t *testing.T) {
	seen := make(map[string]bool, 64)
	for range 64 {
		actor := runtime.NewActor()
		if len(actor) != 16 {
			t.Fatalf("NewActor() = %q, want 16 hex characters", actor)
		}
		if seen[actor] {
			t.Fatalf("NewActor() returned %q twice", actor)
		}
		seen[actor] = true
	}
}
