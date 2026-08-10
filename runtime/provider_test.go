package runtime_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/arhuman/axf/runtime"
)

func TestConfigString(t *testing.T) {
	cfg := runtime.Config{
		"name":  json.RawMessage(`"Alchemist"`),
		"count": json.RawMessage(`3`),
	}
	tests := []struct {
		name    string
		key     string
		want    string
		wantErr bool
	}{
		{name: "present string", key: "name", want: "Alchemist"},
		{name: "absent member is empty, not an error", key: "missing"},
		{name: "present but not a string", key: "count", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cfg.String(tt.key)
			if (err != nil) != tt.wantErr {
				t.Fatalf("String(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("String(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// A canonical capability left out of v1 must read as a deliberate gap, an
// unknown name as an unknown name.
func TestRegistryLookup(t *testing.T) {
	reg := runtime.DefaultRegistry(&fakeLauncher{})
	tests := []struct {
		name       string
		capability string
		wantFound  bool
		wantReason string
	}{
		{name: "shell", capability: "shell", wantFound: true},
		{name: "git-identity", capability: "git-identity", wantFound: true},
		{name: "browser-profile", capability: "browser-profile", wantFound: true},
		{name: "ssh-keypair", capability: "ssh-keypair", wantFound: true},
		{name: "ai-account", capability: "ai-account", wantReason: "requires asset decryption"},
		{name: "locale", capability: "locale", wantReason: "no provider shipped in v1"},
		{name: "vendor extension", capability: "x-doolta-vpn", wantReason: "not a capability of the v0 registry"},
		{name: "empty", capability: "", wantReason: "empty capability name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := reg.Lookup(tt.capability)
			if tt.wantFound {
				if err != nil {
					t.Fatalf("Lookup(%q) error = %v", tt.capability, err)
				}
				if provider.Capability() != tt.capability {
					t.Errorf("provider.Capability() = %q, want %q", provider.Capability(), tt.capability)
				}
				return
			}
			if !errors.Is(err, runtime.ErrProviderNotImplemented) {
				t.Fatalf("Lookup(%q) error = %v, want it to wrap ErrProviderNotImplemented", tt.capability, err)
			}
			if !strings.Contains(err.Error(), tt.wantReason) {
				t.Errorf("Lookup(%q) error = %v, want it to explain %q", tt.capability, err, tt.wantReason)
			}
		})
	}
}
