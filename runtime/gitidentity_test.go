package runtime_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/arhuman/axf/runtime"
)

func TestGitIdentityProviderActivate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     runtime.Config
		want    map[string]string
		wantErr bool
	}{
		{
			name: "author and committer share the identity",
			cfg: runtime.Config{
				"name":  json.RawMessage(`"Alchemist"`),
				"email": json.RawMessage(`"alchemist@example.com"`),
			},
			want: map[string]string{
				"GIT_AUTHOR_NAME":     "Alchemist",
				"GIT_AUTHOR_EMAIL":    "alchemist@example.com",
				"GIT_COMMITTER_NAME":  "Alchemist",
				"GIT_COMMITTER_EMAIL": "alchemist@example.com",
			},
		},
		{
			// Half an identity would silently fall back to the ambient
			// .gitconfig, which is the leak this capability prevents.
			name:    "missing email",
			cfg:     runtime.Config{"name": json.RawMessage(`"Alchemist"`)},
			wantErr: true,
		},
		{
			name:    "missing name",
			cfg:     runtime.Config{"email": json.RawMessage(`"alchemist@example.com"`)},
			wantErr: true,
		},
		{
			name:    "no config at all",
			cfg:     nil,
			wantErr: true,
		},
		{
			name:    "name of the wrong type",
			cfg:     runtime.Config{"name": json.RawMessage(`["Alchemist"]`)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runtime.GitIdentityProvider{}.Activate(runtime.Target{}, tt.cfg)
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

func TestGitIdentityProviderEnvVarNames(t *testing.T) {
	got := slices.Sorted(slices.Values(runtime.GitIdentityProvider{}.EnvVarNames(runtime.Target{}, nil)))
	want := []string{"GIT_AUTHOR_EMAIL", "GIT_AUTHOR_NAME", "GIT_COMMITTER_EMAIL", "GIT_COMMITTER_NAME"}
	if !slices.Equal(got, want) {
		t.Errorf("EnvVarNames() = %v, want %v", got, want)
	}
}

func TestGitIdentityProviderRunImplementsNoAction(t *testing.T) {
	err := runtime.GitIdentityProvider{}.Run(runtime.Target{}, "sign", nil)
	if !errors.Is(err, runtime.ErrActionNotImplemented) {
		t.Errorf("Run() = %v, want it to wrap ErrActionNotImplemented", err)
	}
}
