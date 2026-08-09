package runtime

import "fmt"

// Environment variables exported by ShellProvider.
const (
	// EnvAlterName carries the store name of the active Alter.
	EnvAlterName = "AXF_ALTER_NAME"
	// EnvPromptLabel carries capabilities[].config.promptLabel, for a prompt.
	EnvPromptLabel = "AXF_PROMPT_LABEL"
)

// ShellProvider implements the shell capability by exporting the identity of
// the active Alter so the user's own rc file can pick it up, typically in PS1.
//
// It starts no shell and rewrites no rc file: the shell that evaluated the
// output of `axf up` is already the shell being altered, and a provider that
// edited .zshrc would leave state behind that `axf down` could not undo by
// unsetting a variable.
//
// Config (capabilities[].config):
//
//	{"promptLabel": "alchemist"}
//
// promptLabel is optional; when set it is exported as AXF_PROMPT_LABEL.
//
// The zero value is ready to use.
type ShellProvider struct{}

// Capability returns "shell".
func (ShellProvider) Capability() string { return "shell" }

// Activate exports the Alter name and, when configured, the prompt label.
func (ShellProvider) Activate(t Target, cfg Config) (ActivationResult, error) {
	label, err := cfg.String("promptLabel")
	if err != nil {
		return ActivationResult{}, fmt.Errorf("shell: %w", err)
	}
	env := map[string]string{EnvAlterName: t.Name}
	if label != "" {
		env[EnvPromptLabel] = label
	}
	return ActivationResult{Env: env}, nil
}

// EnvVarNames mirrors Activate: AXF_PROMPT_LABEL is listed only when the config
// carries a label, so `axf down` never unsets a variable `axf up` did not set
// and which the user may have set themselves.
func (ShellProvider) EnvVarNames(_ Target, cfg Config) []string {
	names := []string{EnvAlterName}
	if label, err := cfg.String("promptLabel"); err == nil && label != "" {
		names = append(names, EnvPromptLabel)
	}
	return names
}

// Run treats "start", the action of the spec's own example (section 18), as a
// no-op: exporting the variables above is the whole of activating a shell that
// is already running. Any other action is reported as unimplemented.
func (ShellProvider) Run(_ Target, action string, _ Config) error {
	if action == "start" {
		return nil
	}
	return fmt.Errorf("%w: shell action %q", ErrActionNotImplemented, action)
}
