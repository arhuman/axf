package runtime

import (
	"fmt"
	"regexp"
	"strings"
)

// AXFActiveAlter is the environment variable through which the runtime learns
// which Alter the calling shell has active. The shell that evaluated `axf up`
// carries it; nothing is written to disk (spec section 12).
const AXFActiveAlter = "AXF_ACTIVE_ALTER"

// envName is the POSIX portable environment variable name grammar.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// script accumulates the shell commands `axf up` and `axf down` print for the
// calling shell to evaluate.
type script struct {
	b strings.Builder
}

// export appends `export NAME='value'`.
func (s *script) export(name, value string) error {
	if err := checkEnvName(name); err != nil {
		return err
	}
	fmt.Fprintf(&s.b, "export %s=%s\n", name, quote(value))
	return nil
}

// unset appends `unset NAME`.
func (s *script) unset(name string) error {
	if err := checkEnvName(name); err != nil {
		return err
	}
	fmt.Fprintf(&s.b, "unset %s\n", name)
	return nil
}

// String returns the accumulated commands, one per line.
func (s *script) String() string { return s.b.String() }

// checkEnvName rejects anything that is not a POSIX environment variable name.
// The output of `axf up` is evaluated by the caller's shell, so a provider must
// never be able to smuggle a command through a variable name.
func checkEnvName(name string) error {
	if !envName.MatchString(name) {
		return fmt.Errorf("runtime: %q is not a valid environment variable name", name)
	}
	return nil
}

// quote wraps s in single quotes, the one POSIX quoting with no escape
// sequences at all: every byte but ' is literal, and ' itself is closed,
// escaped and reopened. A value holding spaces, quotes or shell metacharacters
// therefore survives the caller's eval verbatim.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
