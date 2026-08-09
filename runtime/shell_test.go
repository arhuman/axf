package runtime_test

import (
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/arhuman/axf/runtime"
)

func TestShellProviderActivate(t *testing.T) {
	target := runtime.Target{Name: "alchemist", Home: "/tmp/axf-home"}
	tests := []struct {
		name    string
		cfg     runtime.Config
		want    map[string]string
		wantErr bool
	}{
		{
			name: "prompt label",
			cfg:  runtime.Config{"promptLabel": json.RawMessage(`"alchemist"`)},
			want: map[string]string{"AXF_ALTER_NAME": "alchemist", "AXF_PROMPT_LABEL": "alchemist"},
		},
		{
			name: "no config still names the Alter",
			cfg:  nil,
			want: map[string]string{"AXF_ALTER_NAME": "alchemist"},
		},
		{
			name:    "prompt label of the wrong type",
			cfg:     runtime.Config{"promptLabel": json.RawMessage(`42`)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runtime.ShellProvider{}.Activate(target, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Activate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got.Env, tt.want) {
				t.Errorf("Env = %v, want %v", got.Env, tt.want)
			}
		})
	}
}

// EnvVarNames drives the unsets of axf down, so it must list exactly what
// Activate exported and nothing the user may have set themselves.
func TestShellProviderEnvVarNamesMirrorActivate(t *testing.T) {
	target := runtime.Target{Name: "alchemist"}
	for _, cfg := range []runtime.Config{
		nil,
		{"promptLabel": json.RawMessage(`"alchemist"`)},
	} {
		result, err := runtime.ShellProvider{}.Activate(target, cfg)
		if err != nil {
			t.Fatalf("Activate() error = %v", err)
		}
		want := slices.Sorted(maps.Keys(result.Env))
		got := slices.Sorted(slices.Values(runtime.ShellProvider{}.EnvVarNames(target, cfg)))
		if !slices.Equal(got, want) {
			t.Errorf("EnvVarNames(%v) = %v, want %v", cfg, got, want)
		}
	}
}

func TestShellProviderRun(t *testing.T) {
	provider := runtime.ShellProvider{}
	if err := provider.Run(runtime.Target{}, "start", nil); err != nil {
		t.Errorf(`Run("start") = %v, want it treated as a no-op`, err)
	}
	err := provider.Run(runtime.Target{}, "restart", nil)
	if !errors.Is(err, runtime.ErrActionNotImplemented) {
		t.Errorf(`Run("restart") = %v, want it to wrap ErrActionNotImplemented`, err)
	}
}
