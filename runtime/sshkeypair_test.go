package runtime_test

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	alter "github.com/arhuman/axf"
	"github.com/arhuman/axf/runtime"
)

const sshSecret = "-----BEGIN OPENSSH PRIVATE KEY-----\nnot really a key\n"

// assetConfig is the ssh-keypair config naming one assets[] entry.
func assetConfig(name string) runtime.Config {
	return runtime.Config{"asset": json.RawMessage(`"` + name + `"`)}
}

// sshTarget builds a Target on a fresh $AXF_HOME holding the decryption
// identity of the Alter, and returns it with that identity's recipient.
func sshTarget(t *testing.T, name string) (runtime.Target, string) {
	t.Helper()
	home := t.TempDir()
	_, recipient, err := runtime.GenerateIdentity(home, name)
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}
	return runtime.Target{Name: name, Home: home}, recipient
}

func TestSSHKeypairProviderCapability(t *testing.T) {
	if got := (runtime.SSHKeypairProvider{}).Capability(); got != "ssh-keypair" {
		t.Errorf("Capability() = %q, want %q", got, "ssh-keypair")
	}
}

// An inline Asset is decrypted with the Alter's own identity and lands beside
// the browser profiles, under the same per-Alter isolation and file modes.
func TestSSHKeypairProviderActivateInline(t *testing.T) {
	target, recipient := sshTarget(t, "alchemist")
	target.Assets = []alter.Asset{inlineAsset("ssh-key", encryptFor(t, sshSecret, recipient), recipient)}

	got, err := runtime.SSHKeypairProvider{}.Activate(target, assetConfig("ssh-key"))
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	want := filepath.Join(target.Home, "keys", "ssh", "alchemist", "ssh-key")
	if got.Env[runtime.EnvSSHKeyPath] != want {
		t.Errorf("AXF_SSH_KEY_PATH = %q, want %q", got.Env[runtime.EnvSSHKeyPath], want)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("reading the decrypted key: %v", err)
	}
	if string(data) != sshSecret {
		t.Errorf("key file = %q, want %q", data, sshSecret)
	}
	if m := mode(t, want); m != 0o600 {
		t.Errorf("key mode = %v, want 0600", m)
	}
	if m := mode(t, filepath.Dir(want)); m != 0o700 {
		t.Errorf("key directory mode = %v, want 0700", m)
	}
}

// `axf up` on the already active Alter runs Activate again, so a second pass
// must overwrite the same path rather than fail on the file being there.
func TestSSHKeypairProviderActivateIsIdempotent(t *testing.T) {
	target, recipient := sshTarget(t, "alchemist")
	target.Assets = []alter.Asset{inlineAsset("ssh-key", encryptFor(t, sshSecret, recipient), recipient)}

	first, err := runtime.SSHKeypairProvider{}.Activate(target, assetConfig("ssh-key"))
	if err != nil {
		t.Fatalf("first Activate() error = %v", err)
	}
	second, err := runtime.SSHKeypairProvider{}.Activate(target, assetConfig("ssh-key"))
	if err != nil {
		t.Fatalf("second Activate() error = %v", err)
	}
	if first.Env[runtime.EnvSSHKeyPath] != second.Env[runtime.EnvSSHKeyPath] {
		t.Errorf("path moved between activations: %q then %q",
			first.Env[runtime.EnvSSHKeyPath], second.Env[runtime.EnvSSHKeyPath])
	}
	data, err := os.ReadFile(second.Env[runtime.EnvSSHKeyPath])
	if err != nil {
		t.Fatalf("reading the decrypted key: %v", err)
	}
	if string(data) != sshSecret {
		t.Errorf("key file = %q, want %q", data, sshSecret)
	}
	if m := mode(t, second.Env[runtime.EnvSSHKeyPath]); m != 0o600 {
		t.Errorf("key mode = %v, want 0600", m)
	}
}

// A ref Asset points at a key the user already manages: it is exported as is,
// never copied into $AXF_HOME and never decrypted.
func TestSSHKeypairProviderActivateRef(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(key, []byte(sshSecret), 0o600); err != nil {
		t.Fatalf("writing %s: %v", key, err)
	}
	target := runtime.Target{
		Name:   "alchemist",
		Home:   t.TempDir(),
		Assets: []alter.Asset{{Name: "ssh-key", Kind: alter.AssetKindRef, URI: key}},
	}

	got, err := runtime.SSHKeypairProvider{}.Activate(target, assetConfig("ssh-key"))
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if got.Env[runtime.EnvSSHKeyPath] != key {
		t.Errorf("AXF_SSH_KEY_PATH = %q, want %q", got.Env[runtime.EnvSSHKeyPath], key)
	}
	if entries, err := os.ReadDir(target.Home); err != nil || len(entries) != 0 {
		t.Errorf("$AXF_HOME = %v (err %v), want a ref asset to write nothing", entries, err)
	}
}

func TestSSHKeypairProviderActivateErrors(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name   string
		assets []alter.Asset
		cfg    runtime.Config
		// identity tells whether the Alter has a local decryption identity.
		identity bool
		want     string
	}{
		{
			name: "no config at all",
			cfg:  nil,
			want: "config.asset is required",
		},
		{
			name: "config.asset of the wrong type",
			cfg:  runtime.Config{"asset": json.RawMessage(`["ssh-key"]`)},
			want: `config "asset" must be a string`,
		},
		{
			name:   "config names an asset the Alter does not declare",
			assets: []alter.Asset{{Name: "other", Kind: alter.AssetKindRef, URI: dir}},
			cfg:    assetConfig("ssh-key"),
			want:   `Alter "alchemist" declares no asset named "ssh-key"`,
		},
		{
			name:   "an asset of neither kind",
			assets: []alter.Asset{{Name: "ssh-key", Kind: "other"}},
			cfg:    assetConfig("ssh-key"),
			want:   `has kind "other"`,
		},
		{
			name:   "a ref asset with no uri",
			assets: []alter.Asset{{Name: "ssh-key", Kind: alter.AssetKindRef}},
			cfg:    assetConfig("ssh-key"),
			want:   "has no uri",
		},
		{
			name:   "a ref asset whose uri does not exist",
			assets: []alter.Asset{{Name: "ssh-key", Kind: alter.AssetKindRef, URI: filepath.Join(dir, "ghost")}},
			cfg:    assetConfig("ssh-key"),
			want:   "no such file or directory",
		},
		{
			name:   "a ref asset pointing at a directory",
			assets: []alter.Asset{{Name: "ssh-key", Kind: alter.AssetKindRef, URI: dir}},
			cfg:    assetConfig("ssh-key"),
			want:   "is not a regular file",
		},
		{
			// The name reaches the filesystem, so it must stay a bare stem for
			// the same reason a store name does.
			name:     "an inline asset whose name would escape the keys directory",
			assets:   []alter.Asset{inlineAsset("../../escape", "", "")},
			cfg:      assetConfig("../../escape"),
			identity: true,
			want:     "contains a path separator",
		},
		{
			name:   "an inline asset with no local identity",
			assets: []alter.Asset{inlineAsset("ssh-key", "", "")},
			cfg:    assetConfig("ssh-key"),
			want:   "no decryption identity",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := runtime.Target{Name: "alchemist", Home: t.TempDir(), Assets: tt.assets}
			if tt.identity {
				if _, _, err := runtime.GenerateIdentity(target.Home, target.Name); err != nil {
					t.Fatalf("GenerateIdentity() error = %v", err)
				}
			}
			_, err := runtime.SSHKeypairProvider{}.Activate(target, tt.cfg)
			if err == nil {
				t.Fatalf("Activate() error = nil, want %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// The missing-identity error must be actionable on its own: it names the Asset
// that could not be opened and the file that would have opened it.
func TestSSHKeypairProviderActivateWithoutAnIdentity(t *testing.T) {
	target := runtime.Target{
		Name:   "alchemist",
		Home:   t.TempDir(),
		Assets: []alter.Asset{inlineAsset("ssh-key", "", "")},
	}
	_, err := runtime.SSHKeypairProvider{}.Activate(target, assetConfig("ssh-key"))
	if !errors.Is(err, runtime.ErrNoIdentity) {
		t.Fatalf("Activate() error = %v, want it to wrap ErrNoIdentity", err)
	}
	for _, want := range []string{`"ssh-key"`, filepath.Join(target.Home, "keys", "alchemist.age")} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	}
}

func TestSSHKeypairProviderActivateAssetNotFound(t *testing.T) {
	target := runtime.Target{Name: "alchemist", Home: t.TempDir()}
	_, err := runtime.SSHKeypairProvider{}.Activate(target, assetConfig("ssh-key"))
	if !errors.Is(err, runtime.ErrAssetNotFound) {
		t.Errorf("Activate() error = %v, want it to wrap ErrAssetNotFound", err)
	}
}

// EnvVarNames is what `axf down` calls: it must match what Activate exports
// without reading a key, an identity or the config's target Asset.
func TestSSHKeypairProviderEnvVarNames(t *testing.T) {
	tests := []struct {
		name string
		cfg  runtime.Config
		want []string
	}{
		{name: "config naming an asset", cfg: assetConfig("ssh-key"), want: []string{runtime.EnvSSHKeyPath}},
		{name: "no config at all", cfg: nil},
		{name: "config naming no asset", cfg: runtime.Config{}},
		{name: "asset of the wrong type", cfg: runtime.Config{"asset": json.RawMessage(`3`)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			got := runtime.SSHKeypairProvider{}.EnvVarNames(runtime.Target{Name: "alchemist", Home: home}, tt.cfg)
			if !slices.Equal(got, tt.want) {
				t.Errorf("EnvVarNames() = %v, want %v", got, tt.want)
			}
			entries, err := os.ReadDir(home)
			if err != nil {
				t.Fatalf("reading %s: %v", home, err)
			}
			if len(entries) != 0 {
				t.Errorf("$AXF_HOME = %v, want EnvVarNames to touch nothing", entries)
			}
		})
	}
}

// What Activate exports and what EnvVarNames announces must not drift.
func TestSSHKeypairProviderEnvVarNamesMatchActivate(t *testing.T) {
	target, recipient := sshTarget(t, "alchemist")
	target.Assets = []alter.Asset{inlineAsset("ssh-key", encryptFor(t, sshSecret, recipient), recipient)}

	got, err := runtime.SSHKeypairProvider{}.Activate(target, assetConfig("ssh-key"))
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	exported := slices.Sorted(maps.Keys(got.Env))
	announced := slices.Sorted(slices.Values(
		runtime.SSHKeypairProvider{}.EnvVarNames(target, assetConfig("ssh-key"))))
	if !slices.Equal(exported, announced) {
		t.Errorf("Activate exports %v, EnvVarNames announces %v", exported, announced)
	}
}

func TestSSHKeypairProviderRunImplementsNoAction(t *testing.T) {
	err := runtime.SSHKeypairProvider{}.Run(runtime.Target{}, "load", nil)
	if !errors.Is(err, runtime.ErrActionNotImplemented) {
		t.Errorf("Run() = %v, want it to wrap ErrActionNotImplemented", err)
	}
}

// newSSHHome builds an AXF_HOME holding the ssh-only fixture, the decryption
// identity of that Alter, and an inline Asset encrypted for it. The fixture
// carries placeholders rather than a ciphertext: an envelope only the identity
// generated here can open cannot be checked in.
func newSSHHome(t *testing.T, plaintext string) string {
	t.Helper()
	home := t.TempDir()
	_, recipient, err := runtime.GenerateIdentity(home, "ssh-only")
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(alterFixtures, "ssh-only.json"))
	if err != nil {
		t.Fatalf("reading the ssh-only fixture: %v", err)
	}
	doc := strings.NewReplacer(
		"{{recipient}}", recipient,
		"{{ciphertext}}", encryptFor(t, plaintext, recipient),
	).Replace(string(data))
	dir := filepath.Join(home, "alters")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ssh-only.json"), []byte(doc), 0o600); err != nil {
		t.Fatalf("writing the ssh-only document: %v", err)
	}
	return home
}

// End to end: an Alter declaring ssh-keypair over an inline Asset activates,
// exports the key path and leaves the decrypted key on disk.
func TestUpActivatesAnInlineSSHKey(t *testing.T) {
	home := newSSHHome(t, sshSecret)
	got, err := up(t, home, "", "ssh-only", noBrowser())
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	key := filepath.Join(home, "keys", "ssh", "ssh-only", "ssh-key")
	want := "export AXF_SSH_KEY_PATH='" + key + "'\n" +
		"export AXF_ALTER_NAME='ssh-only'\n" +
		"export AXF_ACTIVE_ALTER='ssh-only'\n"
	if got != want {
		t.Errorf("script =\n%s\nwant\n%s", got, want)
	}
	data, err := os.ReadFile(key)
	if err != nil {
		t.Fatalf("reading the decrypted key: %v", err)
	}
	if string(data) != sshSecret {
		t.Errorf("key file = %q, want %q", data, sshSecret)
	}
	if m := mode(t, key); m != 0o600 {
		t.Errorf("key mode = %v, want 0600", m)
	}
}

// down must clear the key path variable without needing the identity that
// decrypted it: EnvVarNames reads the config and nothing else.
func TestDownClearsTheSSHKeyPath(t *testing.T) {
	home := newSSHHome(t, sshSecret)
	if err := os.RemoveAll(filepath.Join(home, "keys")); err != nil {
		t.Fatalf("removing the keys directory: %v", err)
	}
	got, err := down(t, home, "ssh-only", noBrowser())
	if err != nil {
		t.Fatalf("Down() error = %v", err)
	}
	want := "unset AXF_SSH_KEY_PATH\nunset AXF_ALTER_NAME\nunset AXF_ACTIVE_ALTER\n"
	if got != want {
		t.Errorf("script =\n%s\nwant\n%s", got, want)
	}
}

// A decrypted key that cannot be written names the Asset it belongs to, so the
// failure is not mistaken for a decryption problem.
func TestSSHKeypairProviderActivateReportsAnUnwritableKeyPath(t *testing.T) {
	target, recipient := sshTarget(t, "alchemist")
	target.Assets = []alter.Asset{inlineAsset("ssh-key", encryptFor(t, sshSecret, recipient), recipient)}
	blocked := filepath.Join(target.Home, "keys", "ssh", "alchemist")
	if err := os.MkdirAll(filepath.Dir(blocked), 0o700); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(blocked), err)
	}
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatalf("writing the blocking file: %v", err)
	}
	_, err := runtime.SSHKeypairProvider{}.Activate(target, assetConfig("ssh-key"))
	if err == nil {
		t.Fatal("Activate() error = nil, want the write failure reported")
	}
	if !strings.Contains(err.Error(), `inline asset "ssh-key"`) {
		t.Errorf("error = %v, want it to name the asset", err)
	}
}
