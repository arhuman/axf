package version_test

import (
	"strings"
	"testing"

	"github.com/arhuman/axf/internal/version"
)

func TestString(t *testing.T) {
	got := version.String()
	for _, want := range []string{"axf", version.Version, version.GitCommit, version.BuildDate} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
}
