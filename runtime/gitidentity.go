package runtime

import (
	"fmt"
	"slices"
)

// Environment variables exported by GitIdentityProvider. git reads all four and
// they take precedence over any .gitconfig.
const (
	EnvGitAuthorName     = "GIT_AUTHOR_NAME"
	EnvGitAuthorEmail    = "GIT_AUTHOR_EMAIL"
	EnvGitCommitterName  = "GIT_COMMITTER_NAME"
	EnvGitCommitterEmail = "GIT_COMMITTER_EMAIL"
)

// gitIdentityVars is the exact set GitIdentityProvider exports, so that
// EnvVarNames cannot drift from Activate.
var gitIdentityVars = []string{
	EnvGitAuthorName,
	EnvGitAuthorEmail,
	EnvGitCommitterName,
	EnvGitCommitterEmail,
}

// GitIdentityProvider implements the git-identity capability through the four
// environment variables git reads for authorship.
//
// Environment variables are used rather than a .gitconfig rewrite on purpose:
// they override the user's configuration for the processes started from the
// activated shell, they cannot leak into another shell, and deactivating is a
// plain unset that leaves nothing on disk to clean up later.
//
// Config (capabilities[].config), both members required:
//
//	{"name": "Alchemist", "email": "alchemist@example.com"}
//
// The zero value is ready to use.
type GitIdentityProvider struct{}

// Capability returns "git-identity".
func (GitIdentityProvider) Capability() string { return "git-identity" }

// Activate exports the author and committer identity. Both config members are
// required: a git identity missing either half would silently fall back to the
// ambient .gitconfig, which is precisely the leak this capability prevents.
func (GitIdentityProvider) Activate(_ Target, cfg Config) (ActivationResult, error) {
	name, err := cfg.String("name")
	if err != nil {
		return ActivationResult{}, fmt.Errorf("git-identity: %w", err)
	}
	email, err := cfg.String("email")
	if err != nil {
		return ActivationResult{}, fmt.Errorf("git-identity: %w", err)
	}
	if name == "" || email == "" {
		return ActivationResult{}, fmt.Errorf(
			"git-identity: config.name and config.email are both required, got name=%q email=%q", name, email)
	}
	return ActivationResult{Env: map[string]string{
		EnvGitAuthorName:     name,
		EnvGitAuthorEmail:    email,
		EnvGitCommitterName:  name,
		EnvGitCommitterEmail: email,
	}}, nil
}

// EnvVarNames returns the four git variables, which do not depend on the config.
func (GitIdentityProvider) EnvVarNames(_ Target, _ Config) []string {
	return slices.Clone(gitIdentityVars)
}

// Run implements no action: a git identity is entirely expressed by the
// variables Activate exports, so there is nothing left for a hook to trigger.
func (GitIdentityProvider) Run(_ Target, action string, _ Config) error {
	return fmt.Errorf("%w: git-identity action %q", ErrActionNotImplemented, action)
}
