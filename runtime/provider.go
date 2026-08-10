// Package runtime activates an AXF Alter in the calling shell: it resolves a
// document from the local store, maps its capabilities[] onto concrete
// providers and emits the shell commands that put the identity in place.
//
// The runtime keeps no persistent state. Which Alter is active is carried by
// the AXF_ACTIVE_ALTER environment variable of the shell that evaluated the
// output of `axf up`, per spec section 12: activation is a runtime fact, never
// a field of the Alter document.
//
// This is the v1 and v1.1 layer of the roadmap (spec section 23). The Alter
// Guard applies policies[] to the activation path and appends the audit trail
// of spec section 15 to $AXF_HOME/audit.log; see Guard for its decision model
// and for what it deliberately does not police. Capabilities whose provider
// would have to decrypt an Asset, ssh-keypair and ai-account, have no
// implementation here because this SDK performs no cryptography yet.
package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	alter "github.com/arhuman/axf"
)

// ErrProviderNotImplemented reports a capability that no provider in this build
// implements.
var ErrProviderNotImplemented = errors.New("runtime: provider not implemented in v1")

// ErrActionNotImplemented reports a lifecycle hook action a provider does not
// implement.
var ErrActionNotImplemented = errors.New("runtime: action not implemented in v1")

// Target is the per-activation context passed to every Provider call. Providers
// deriving a per-Alter path or label use it; the others ignore it.
type Target struct {
	// Name is the store name of the Alter, the argument to `axf up`, which is
	// also the value carried by AXF_ACTIVE_ALTER. It is not required to equal
	// metadata.name.
	Name string
	// Home is the AXF runtime root: $AXF_HOME, or ~/.axf by default.
	Home string
}

// Config is the provider-defined configuration of one capabilities[] entry,
// carried verbatim from the document. It is nil when the Alter declares no
// entry for the capability, which a lifecycle hook is allowed to target.
type Config map[string]json.RawMessage

// String decodes a string member of the config.
//
// An absent member yields "" and no error: to a provider, an optional field and
// an empty one are the same thing. A member present but not a string is an
// error, because that is a mistake in the document rather than an omission.
func (c Config) String(key string) (string, error) {
	raw, ok := c[key]
	if !ok {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("runtime: config %q must be a string: %w", key, err)
	}
	return s, nil
}

// ActivationResult is what a Provider contributes to the activated shell.
type ActivationResult struct {
	// Env maps environment variable names to the values to export. Names must
	// be POSIX environment variable names; the runtime refuses to emit anything
	// else rather than hand the caller a line it would eval.
	Env map[string]string
}

// Provider implements one capability of the AXF registry (spec section 14) for
// a concrete platform or tool. The format names the capability, the runtime
// picks the provider (spec section 3, Provider-based).
//
// Activate and Run may touch the host; EnvVarNames must not. `axf down` calls
// only EnvVarNames, which is what makes tearing an Alter down independent of a
// secret, a browser or a network still being reachable.
type Provider interface {
	// Capability returns the canonical capability name this provider
	// implements, for instance "shell".
	Capability() string

	// Activate prepares the capability and returns the environment it
	// contributes. It must be idempotent: `axf up` on the already-active Alter
	// runs it again.
	Activate(t Target, cfg Config) (ActivationResult, error)

	// EnvVarNames returns the names Activate would export for this config, in
	// any order. It is pure: no host access, no secret, no error. This is what
	// lets `axf down` unset exactly what `axf up` set without persisting state.
	EnvVarNames(t Target, cfg Config) []string

	// Run executes a lifecycle hook action (spec section 12) that is not the
	// plain activation path, such as {"capability": "browser-profile",
	// "action": "launch"}. It returns an error wrapping ErrActionNotImplemented
	// when it does not implement the action, unless the action is one it
	// legitimately treats as a no-op.
	Run(t Target, action string, cfg Config) error
}

// Registry maps canonical capability names to the provider implementing them.
type Registry map[string]Provider

// DefaultRegistry returns the providers shipped with axf v1: shell,
// git-identity and browser-profile, the latter launching browsers through l.
//
// The other names of the v0 capability registry are absent on purpose.
// ssh-keypair and ai-account would have to decrypt an Asset, and this SDK
// performs no cryptography yet; locale has no v1 provider.
func DefaultRegistry(l Launcher) Registry {
	providers := []Provider{
		ShellProvider{},
		GitIdentityProvider{},
		BrowserProfileProvider{Launcher: l},
	}
	reg := make(Registry, len(providers))
	for _, p := range providers {
		reg[p.Capability()] = p
	}
	return reg
}

// Lookup returns the provider implementing a capability.
//
// The error wraps ErrProviderNotImplemented and says why the capability has no
// provider, so a canonical name deliberately left out of v1 reads as a known
// gap rather than as a typo.
func (r Registry) Lookup(capability string) (Provider, error) {
	if p, ok := r[capability]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("%w: %s (%s)", ErrProviderNotImplemented, capability, missingReason(capability))
}

// missingReason explains why a capability has no provider in this build.
func missingReason(capability string) string {
	switch capability {
	case "ssh-keypair", "ai-account":
		return "requires asset decryption, which this SDK does not perform yet"
	case "":
		return "empty capability name"
	default:
		if slices.Contains(alter.CapabilityRegistry, capability) {
			return "declared in the v0 capability registry, no provider shipped in v1"
		}
		return "not a capability of the v0 registry; a third-party capability must ship its own provider"
	}
}
