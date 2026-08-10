package runtime

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"

	alter "github.com/arhuman/axf"
)

// ErrNoActiveAlter reports that the calling shell has no Alter active. It is
// what `axf down` returns when there is nothing to undo, which is a no-op
// rather than a failure.
var ErrNoActiveAlter = errors.New("runtime: no Alter is active")

// Runtime activates and deactivates Alters for one caller.
//
// It holds no persistent state: Active is what the calling shell reported
// through AXF_ACTIVE_ALTER, so two consecutive axf invocations share nothing
// but that variable (spec section 12). The audit trail the Alter Guard appends
// to $AXF_HOME/audit.log is a record, never state the runtime reads back.
//
// A Runtime is one invocation and is not safe for concurrent use.
type Runtime struct {
	// Store resolves a name to a document.
	Store Store
	// Registry maps capabilities to providers.
	Registry Registry
	// Active is the store name of the Alter the calling shell currently has in
	// place, empty when none is.
	Active string

	// actor is the opaque identifier the Alter Guard records for every audit
	// event of this invocation. It is generated on first use and never
	// persisted, so no two invocations can be correlated through it.
	actor string
}

// New builds a Runtime rooted at home, told by active which Alter the calling
// shell has in place. A nil registry means the providers of DefaultRegistry
// driven by a real ExecLauncher.
func New(home, active string, reg Registry) *Runtime {
	if reg == nil {
		reg = DefaultRegistry(ExecLauncher{})
	}
	return &Runtime{Store: Store{Home: home}, Registry: reg, Active: active}
}

// Up activates the named Alter and writes to out the shell commands putting it
// in place, for the caller to evaluate.
//
// The commands are written in a single write once the whole activation is
// planned: out stays empty when the requested Alter cannot be activated, so its
// output is always safe to eval unconditionally.
//
// They come in this order: the unsets of the previously active Alter, the
// exports of every capability, then AXF_ACTIVE_ALTER. preActivation hooks run
// before the capabilities are activated and postActivation hooks after, both
// for their side effects only. When the shell already has this Alter active,
// nothing is deactivated first and the activation simply runs again.
//
// A returned error does not always mean nothing happened. A lifecycle hook
// failing, or the teardown of the previously active Alter failing, is reported
// but does not stop the activation, and the commands written to out remain
// correct: the caller should report the error and still evaluate out. Only a
// failure to resolve, validate, authorise or activate the requested Alter
// itself leaves out empty, and that includes a declared capability with no
// provider in this build and an action the Alter Guard refuses.
func (r *Runtime) Up(name string, out io.Writer) error {
	doc, err := r.Store.Load(name)
	if err != nil {
		return err
	}
	// Resolve every declared capability before emitting anything: a partially
	// applied identity is worse than none, because the user would believe a
	// capability is in place when it is not.
	providers, err := r.resolveAll(doc)
	if err != nil {
		return err
	}
	if err := r.authorize(doc); err != nil {
		return fmt.Errorf("runtime: activating %q: %w", name, err)
	}

	target := Target{Name: name, Home: r.Store.Home}
	var s script
	var soft []error
	if r.Active != "" && r.Active != name {
		soft = append(soft, r.teardown(r.Active, &s)...)
	}
	soft = append(soft, r.runHooks(target, doc, hooksOf(doc).PreActivation)...)
	for i, capability := range doc.Capabilities {
		result, err := providers[i].Activate(target, Config(capability.Config))
		if err != nil {
			return fmt.Errorf("runtime: activating capability %s of %q: %w", capability.Type, name, err)
		}
		for _, envVar := range slices.Sorted(maps.Keys(result.Env)) {
			if err := s.export(envVar, result.Env[envVar]); err != nil {
				return err
			}
		}
	}
	soft = append(soft, r.runHooks(target, doc, hooksOf(doc).PostActivation)...)
	if err := s.export(AXFActiveAlter, name); err != nil {
		return err
	}
	if _, err := io.WriteString(out, s.String()); err != nil {
		return fmt.Errorf("runtime: writing the activation script: %w", err)
	}
	return errors.Join(soft...)
}

// Down deactivates the Alter the calling shell reported through
// AXF_ACTIVE_ALTER and writes to out the shell commands undoing it.
//
// With no Alter active it writes nothing and returns ErrNoActiveAlter, which a
// caller should report as a message rather than as a failure.
//
// Down is deliberately hard to fail: a document that has since been deleted, or
// a capability whose provider is gone, is reported but never stops the unsets
// of the other capabilities, and AXF_ACTIVE_ALTER is unset in every case. A
// shell must always be able to get back to a clean state, which is also why the
// Alter Guard only audits the deactivation and never blocks it (see Guard).
func (r *Runtime) Down(out io.Writer) error {
	if r.Active == "" {
		return ErrNoActiveAlter
	}
	var s script
	soft := r.teardown(r.Active, &s)
	if _, err := io.WriteString(out, s.String()); err != nil {
		return fmt.Errorf("runtime: writing the deactivation script: %w", err)
	}
	return errors.Join(soft...)
}

// teardown appends to s the unsets undoing the named Alter and returns the
// problems met on the way, none of which is fatal. AXF_ACTIVE_ALTER is unset
// even when the document itself could not be loaded.
func (r *Runtime) teardown(name string, s *script) []error {
	var soft []error
	doc, err := r.Store.Load(name)
	if err != nil {
		soft = append(soft, fmt.Errorf("runtime: deactivating %q: %w", name, err))
	} else {
		guard := r.guardFor(doc)
		hooks := hooksOf(doc)
		target := Target{Name: name, Home: r.Store.Home}
		soft = append(soft, r.recordHooks(guard, hooks.PreDeactivation)...)
		soft = append(soft, r.runHooks(target, doc, hooks.PreDeactivation)...)
		for _, capability := range doc.Capabilities {
			if err := guard.Record(capability.Type, ActionDeactivate, r.providerName(capability.Type)); err != nil {
				soft = append(soft, err)
			}
			provider, err := r.Registry.Lookup(capability.Type)
			if err != nil {
				soft = append(soft, fmt.Errorf("runtime: deactivating capability %s of %q: %w",
					capability.Type, name, err))
				continue
			}
			names := provider.EnvVarNames(target, Config(capability.Config))
			for _, envVar := range slices.Sorted(slices.Values(names)) {
				if err := s.unset(envVar); err != nil {
					soft = append(soft, err)
				}
			}
		}
		soft = append(soft, r.recordHooks(guard, hooks.PostDeactivation)...)
		soft = append(soft, r.runHooks(target, doc, hooks.PostDeactivation)...)
	}
	if err := s.unset(AXFActiveAlter); err != nil {
		soft = append(soft, err)
	}
	return soft
}

// authorize asks the Alter Guard about every action activating doc will
// perform: each preActivation hook, then each capability, then each
// postActivation hook. It appends exactly one audit event per action and
// returns every refusal at once, so a user fixes their policies in a single
// pass.
//
// It runs before anything is torn down, activated or printed, for the same
// reason providers are resolved up front: a refused activation must leave the
// calling shell exactly as it was. That applies to a refused hook as much as to
// a refused capability. A hook whose provider is merely missing still degrades
// gracefully (see runHooks); a policy denial is a deliberate block and fails
// closed instead.
func (r *Runtime) authorize(doc *alter.Alter) error {
	guard := r.guardFor(doc)
	hooks := hooksOf(doc)
	var refused []error
	check := func(capability, action string) {
		if err := guard.Check(capability, action, r.providerName(capability)); err != nil {
			refused = append(refused, err)
		}
	}
	for _, hook := range hooks.PreActivation {
		check(hook.Capability, hook.Action)
	}
	for _, capability := range doc.Capabilities {
		check(capability.Type, ActionActivate)
	}
	for _, hook := range hooks.PostActivation {
		check(hook.Capability, hook.Action)
	}
	return errors.Join(refused...)
}

// recordHooks appends the audit events of one deactivation-path hook list.
// Failing to write the trail is reported but never stops a deactivation.
func (r *Runtime) recordHooks(guard *Guard, hooks []alter.Hook) []error {
	var soft []error
	for _, hook := range hooks {
		if err := guard.Record(hook.Capability, hook.Action, r.providerName(hook.Capability)); err != nil {
			soft = append(soft, err)
		}
	}
	return soft
}

// guardFor builds the Alter Guard applying the policies of doc, sharing the
// actor and the audit trail of this invocation. Switching Alters builds two:
// each records events under its own metadata.id, both under the same actor.
func (r *Runtime) guardFor(doc *alter.Alter) *Guard {
	return NewGuard(doc, r.actorID(), r.Store.AuditPath())
}

// actorID returns the opaque actor of this invocation, generating it on first
// use so every audit event of one axf run shares it and no two runs share one.
func (r *Runtime) actorID() string {
	if r.actor == "" {
		r.actor = NewActor()
	}
	return r.actor
}

// providerName is what the audit trail records as the provider of a capability:
// the provider implementing it in this build, empty when there is none. Spec
// section 15 shows a concrete provider name ("firefox"), but a v1 Provider has
// no identity beyond the capability it implements, so that is what is recorded.
func (r *Runtime) providerName(capability string) string {
	provider, err := r.Registry.Lookup(capability)
	if err != nil {
		return ""
	}
	return provider.Capability()
}

// resolveAll returns the provider of each declared capability, in document
// order, or every missing one at once so the user fixes the document in a
// single pass.
func (r *Runtime) resolveAll(doc *alter.Alter) ([]Provider, error) {
	providers := make([]Provider, 0, len(doc.Capabilities))
	var missing []error
	for _, capability := range doc.Capabilities {
		provider, err := r.Registry.Lookup(capability.Type)
		if err != nil {
			missing = append(missing, err)
			continue
		}
		providers = append(providers, provider)
	}
	if len(missing) > 0 {
		return nil, errors.Join(missing...)
	}
	return providers, nil
}

// runHooks executes one hook list in document order and returns one error per
// hook that did not run.
//
// A hook whose provider or action is unimplemented is reported and skipped,
// never fatal: an Alter declaring a hook this build cannot honour must still be
// activatable, and must above all still be deactivatable.
func (r *Runtime) runHooks(t Target, doc *alter.Alter, hooks []alter.Hook) []error {
	var soft []error
	for _, hook := range hooks {
		provider, err := r.Registry.Lookup(hook.Capability)
		if err == nil {
			err = provider.Run(t, hook.Action, configFor(doc, hook.Capability))
		}
		if err != nil {
			soft = append(soft, fmt.Errorf("runtime: hook {capability: %s, action: %s}: %w",
				hook.Capability, hook.Action, err))
		}
	}
	return soft
}

// configFor returns the config of the capabilities[] entry a hook targets. A
// hook may name a capability the Alter does not declare, the same latitude
// policies[] have (spec section 11); the provider then receives a nil config.
func configFor(doc *alter.Alter, capability string) Config {
	for _, declared := range doc.Capabilities {
		if declared.Type == capability {
			return Config(declared.Config)
		}
	}
	return nil
}

// hooksOf returns the four hook lists of a document, tolerating an absent
// lifecycle block.
func hooksOf(doc *alter.Alter) alter.Hooks {
	if doc.Lifecycle == nil || doc.Lifecycle.Hooks == nil {
		return alter.Hooks{}
	}
	return *doc.Lifecycle.Hooks
}
