package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"slices"
	"time"

	alter "github.com/arhuman/axf"
)

// Actions the Guard evaluates policies[] against on the plain activation and
// deactivation paths.
//
// The AXF v0 schema never names them. policies[].action is a free string, and
// the only actions the format itself spells out are those a lifecycle hook
// carries (spec section 12); activating a capability has no action name at all.
// "activate" and "deactivate" are therefore a convention of this runtime: a
// policy targeting them, such as {"capability": "shell", "action": "activate",
// "effect": "deny"}, is written against this runtime rather than against the
// format, and another Alter Guard may well spell them differently.
const (
	ActionActivate   = "activate"
	ActionDeactivate = "deactivate"
)

// ErrDeniedByPolicy reports an action a policies[] entry with scope runtime
// refused.
var ErrDeniedByPolicy = errors.New("runtime: denied by an Alter Guard policy")

// ErrConditionNotEvaluable reports a policy condition this build cannot decide.
// The Guard fails closed on it: the truth of a security-relevant condition being
// unknown must never be read as false.
var ErrConditionNotEvaluable = errors.New("runtime: policy condition cannot be evaluated")

// Guard is the Alter Guard of spec section 4: the runtime component that applies
// policies[] and produces the audit trail. It belongs to the runtime, not to the
// format, and enforces nothing outside the actions axf itself performs. Nothing
// here watches the host for a browser or a shell started outside axf; that would
// need OS-level integration a CLI with no daemon cannot have.
//
// # Decision model
//
// For one (capability, action) pair, every policies[] entry whose capability and
// action both match is evaluated. An entry with no condition always matches; an
// entry whose condition holds contributes its effect. Deny overrides: a single
// matching deny is enough to deny, and no allow can clear it.
//
// There is no implicit default-deny baseline in AXF v0, so an allow entry never
// decides anything by itself. A policy set holding only allow entries changes
// nothing about whether an action proceeds; it exists to put an explicit
// "allowed" line in the trail. An action no policy targets proceeds too.
//
// scope decides blocking, not auditing. A matching deny with scope runtime
// blocks the action before it runs; a matching deny with scope guard, the schema
// default, records the same denied result and lets the action proceed. Exactly
// one audit event is written per evaluation either way, including when no policy
// matched at all: the trail of spec section 15 covers capability actions in
// general, not only the ones a policy touched.
//
// # Enforcement perimeter
//
// Check is the activation path and may block. Record is the deactivation path
// and never can: `axf down` must always be able to return a shell to a clean
// state, so a deny that would have matched, or a condition that cannot be
// evaluated, must not be able to strand a user inside an Alter. Deactivation is
// still audited, always as allowed.
type Guard struct {
	policies  []alter.Policy
	alterID   string
	actor     string
	auditPath string
}

// NewGuard builds the Alter Guard applying the policies of doc, recording actor
// as the origin of every event it writes and appending its trail to auditPath.
//
// actor must be an opaque per-invocation token from NewActor, shared by every
// Guard of one axf invocation and never derived from a real process identifier
// (spec section 15).
func NewGuard(doc *alter.Alter, actor, auditPath string) *Guard {
	return &Guard{
		policies:  doc.Policies,
		alterID:   doc.Metadata.ID,
		actor:     actor,
		auditPath: auditPath,
	}
}

// Check evaluates policies[] for one activation-path action, appends exactly one
// audit event and reports whether the action may proceed.
//
// A nil error means the caller must run the action. A non-nil error means it
// must not, and every reason fails closed: the error wraps ErrDeniedByPolicy
// when a matching deny with scope runtime refused the action, or
// ErrConditionNotEvaluable when a matching policy carries a condition this build
// cannot decide. The audit event is written in both cases, with result denied.
//
// A failure to write the trail is an error too, and therefore also stops the
// action: enforcement whose decisions leave no record is not what an Alter Guard
// promises, and $AXF_HOME has to be writable for the runtime to work at all.
// Record makes the opposite trade for deactivation.
//
// provider names the provider handling the action and is empty when this build
// has none, which is what a lifecycle hook targeting an unimplemented capability
// produces.
func (g *Guard) Check(capability, action, provider string) error {
	d := g.evaluate(capability, action)
	logErr := g.record(capability, action, provider, d.result)
	switch {
	case d.err != nil:
		return errors.Join(d.err, logErr)
	case d.blockedBy >= 0:
		return errors.Join(fmt.Errorf("%w: {capability: %s, action: %s} matched policies[%d]",
			ErrDeniedByPolicy, capability, action, d.blockedBy), logErr)
	}
	return logErr
}

// Record appends the audit event of one deactivation-path action, which is
// always allowed.
//
// It evaluates no policy at all, deliberately: deactivation can never be blocked
// (see Guard), so there is nothing a deny entry could decide here, and a
// condition the Guard cannot evaluate must not turn `axf down` into a failure.
// The returned error only ever reports a failure to write the trail, which a
// caller should report without stopping the deactivation.
func (g *Guard) Record(capability, action, provider string) error {
	return g.record(capability, action, provider, ResultAllowed)
}

// decision is what evaluating policies[] concluded about one action.
type decision struct {
	result AuditResult
	// blockedBy is the policies[] index of the deny entry with scope runtime
	// that refused the action, or -1 when none did.
	blockedBy int
	// err is a condition that could not be evaluated, which fails closed.
	err error
}

// evaluate applies the deny-overrides model of Guard to one action.
//
// An unevaluable condition stops the walk immediately, whatever the effect or
// the scope of the entry carrying it: it means the policy set cannot be applied
// at all, which is a defect of the document rather than a decision about the
// action, and a Guard that guessed past it would be reading an unknown condition
// as false.
func (g *Guard) evaluate(capability, action string) decision {
	d := decision{result: ResultAllowed, blockedBy: -1}
	for i := range g.policies {
		p := &g.policies[i]
		if p.Capability != capability || p.Action != action {
			continue
		}
		matches, err := evaluateCondition(p.Condition)
		if err != nil {
			return decision{
				result:    ResultDenied,
				blockedBy: -1,
				err: fmt.Errorf("%w (policies[%d]: {capability: %s, action: %s})",
					err, i, capability, action),
			}
		}
		if !matches || p.Effect != alter.EffectDeny {
			continue
		}
		d.result = ResultDenied
		if d.blockedBy < 0 && p.EffectiveScope() == alter.ScopeRuntime {
			d.blockedBy = i
		}
	}
	return d
}

// record appends one audit event to the trail.
func (g *Guard) record(capability, action, provider string, result AuditResult) error {
	return appendAudit(g.auditPath, AuditEvent{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		AlterID:    g.alterID,
		Actor:      g.actor,
		Capability: capability,
		Action:     action,
		Provider:   provider,
		Result:     result,
	})
}

// evaluateCondition reports whether a policy condition holds.
//
// An absent condition always holds: the policy then targets its capability and
// action unconditionally. An error means the condition could not be decided and
// wraps ErrConditionNotEvaluable.
func evaluateCondition(c *alter.Condition) (bool, error) {
	if c == nil {
		return true, nil
	}
	fact, err := conditionFact(c)
	if err != nil {
		return false, err
	}
	switch c.Operator {
	case alter.OperatorEquals, alter.OperatorNotEquals:
		want, err := conditionScalar(c.Value)
		if err != nil {
			return false, err
		}
		return (fact == want) == (c.Operator == alter.OperatorEquals), nil
	case alter.OperatorIn, alter.OperatorNotIn:
		set, err := conditionList(c.Value)
		if err != nil {
			return false, err
		}
		return slices.Contains(set, fact) == (c.Operator == alter.OperatorIn), nil
	default:
		return false, fmt.Errorf("%w: unknown operator %q", ErrConditionNotEvaluable, c.Operator)
	}
}

// conditionFact reads the host fact a condition is compared against.
func conditionFact(c *alter.Condition) (string, error) {
	switch c.Type {
	case alter.ConditionOS:
		return goruntime.GOOS, nil
	case alter.ConditionHostname:
		name, err := os.Hostname()
		if err != nil {
			return "", fmt.Errorf("%w: reading the hostname: %w", ErrConditionNotEvaluable, err)
		}
		return name, nil
	case alter.ConditionActiveAlterName:
		// Read literally from the process environment. During `axf up <name>`
		// this is still the previously active Alter: AXF_ACTIVE_ALTER is only
		// updated by the export line the calling shell has not evaluated yet.
		// That is a quirk inherited from activation being carried by the shell
		// (spec section 12), not something the Guard compensates for.
		return os.Getenv(AXFActiveAlter), nil
	case alter.ConditionEnv:
		// Requires Condition.Name (spec sections 11 and 19: "name" is required
		// when type is "env", naming the variable to inspect). An empty Name
		// means either an absent field or an explicitly empty string, and the
		// schema's conditional "required" already rejects a document that
		// omits it, so this is a defence-in-depth check, not the primary gate.
		if c.Name == "" {
			return "", fmt.Errorf(`%w: condition type "env" is missing "name": it must name the environment variable to inspect (spec sections 11 and 19)`,
				ErrConditionNotEvaluable)
		}
		return os.Getenv(c.Name), nil
	default:
		return "", fmt.Errorf("%w: unknown condition type %q", ErrConditionNotEvaluable, c.Type)
	}
}

// conditionScalar decodes the string an equals/notEquals condition compares
// against.
//
// A value that is not a JSON string is a malformed condition, not a false one:
// every fact the Guard reads is a string, so nothing else could ever match, and
// silently answering false would let a typo disable a deny policy.
func conditionScalar(value json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(value, &s); err != nil {
		return "", fmt.Errorf("%w: equals/notEquals need a string value, got %s",
			ErrConditionNotEvaluable, value)
	}
	return s, nil
}

// conditionList decodes the array an in/notIn condition tests membership in. A
// value that is not an array of strings is malformed for the same reason a
// non-string scalar is.
func conditionList(value json.RawMessage) ([]string, error) {
	var list []string
	if err := json.Unmarshal(value, &list); err != nil {
		return nil, fmt.Errorf("%w: in/notIn need an array of strings, got %s",
			ErrConditionNotEvaluable, value)
	}
	return list, nil
}
