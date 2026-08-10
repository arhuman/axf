package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	alter "github.com/arhuman/axf"
)

// EnvSSHKeyPath carries the private key path of the Alter's ssh-keypair Asset.
const EnvSSHKeyPath = "AXF_SSH_KEY_PATH"

// ErrAssetNotFound reports a config.asset naming no assets[] entry of the Alter.
var ErrAssetNotFound = errors.New("runtime: no such asset")

// ErrInvalidAssetName reports an assets[].name unusable as a file name.
var ErrInvalidAssetName = errors.New("runtime: invalid asset name")

// SSHKeypairProvider implements the ssh-keypair capability by resolving one
// Asset of the Alter to a private key path and exporting it.
//
// Config (capabilities[].config), required:
//
//	{"asset": "<assets[].name>"}
//
// A ref Asset is taken as an existing private key already managed elsewhere
// (spec section 10): its uri is validated as a regular file and exported as is,
// nothing is decrypted or copied. An inline Asset is decrypted with the Alter's
// local age identity at $AXF_HOME/keys/<alter>.age and written to
// $AXF_HOME/keys/ssh/<alter>/<asset>, mode 0o600 under a 0o700 parent, the same
// per-Alter isolation BrowserProfileProvider applies to a browser profile.
// Activate is idempotent: it re-decrypts to the same path every time.
//
// AXF_SSH_KEY_PATH is informational and scriptable, like AXF_BROWSER_PROFILE:
// no tool reads it on its own. Feeding it to ssh or git (GIT_SSH_COMMAND, -i)
// is left to the caller's shell configuration or to a lifecycle hook, so that
// this capability never mutates a configuration that belongs to git-identity.
//
// The zero value is ready to use.
type SSHKeypairProvider struct{}

// Capability returns "ssh-keypair".
func (SSHKeypairProvider) Capability() string { return "ssh-keypair" }

// Activate resolves the configured Asset to a private key path on disk and
// exports it. config.asset is required: an ssh-keypair capability that silently
// exported nothing would look active while no key is in place.
func (SSHKeypairProvider) Activate(t Target, cfg Config) (ActivationResult, error) {
	asset, err := sshAsset(t, cfg)
	if err != nil {
		return ActivationResult{}, err
	}
	var path string
	switch asset.Kind {
	case alter.AssetKindRef:
		path, err = refKeyPath(asset)
	case alter.AssetKindInline:
		path, err = writeInlineKey(t, asset)
	default:
		err = fmt.Errorf("ssh-keypair: asset %q has kind %q, want %q or %q",
			asset.Name, asset.Kind, alter.AssetKindInline, alter.AssetKindRef)
	}
	if err != nil {
		return ActivationResult{}, err
	}
	return ActivationResult{Env: map[string]string{EnvSSHKeyPath: path}}, nil
}

// EnvVarNames returns the single key path variable, and nothing when the config
// names no Asset, which is exactly when Activate exports nothing. It resolves no
// Asset and touches no file, so `axf down` clears the variable whether or not
// the key can still be decrypted.
func (SSHKeypairProvider) EnvVarNames(_ Target, cfg Config) []string {
	name, err := cfg.String("asset")
	if err != nil || name == "" {
		return nil
	}
	return []string{EnvSSHKeyPath}
}

// Run implements no action: the key is entirely expressed by the path Activate
// exports, and loading it into an agent is the caller's decision, not a side
// effect this provider imposes.
func (SSHKeypairProvider) Run(_ Target, action string, _ Config) error {
	return fmt.Errorf("%w: ssh-keypair action %q", ErrActionNotImplemented, action)
}

// sshAsset resolves the assets[] entry config.asset names.
func sshAsset(t Target, cfg Config) (alter.Asset, error) {
	name, err := cfg.String("asset")
	if err != nil {
		return alter.Asset{}, fmt.Errorf("ssh-keypair: %w", err)
	}
	if name == "" {
		return alter.Asset{}, errors.New(
			"ssh-keypair: config.asset is required and names the assets[] entry holding the private key")
	}
	for _, asset := range t.Assets {
		if asset.Name == name {
			return asset, nil
		}
	}
	return alter.Asset{}, fmt.Errorf("ssh-keypair: %w: Alter %q declares no asset named %q",
		ErrAssetNotFound, t.Name, name)
}

// refKeyPath validates the uri of a ref Asset as a private key already on disk.
func refKeyPath(asset alter.Asset) (string, error) {
	if asset.URI == "" {
		return "", fmt.Errorf("ssh-keypair: ref asset %q has no uri", asset.Name)
	}
	info, err := os.Stat(asset.URI)
	if err != nil {
		return "", fmt.Errorf("ssh-keypair: ref asset %q: %w", asset.Name, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("ssh-keypair: ref asset %q: %s is not a regular file", asset.Name, asset.URI)
	}
	return asset.URI, nil
}

// writeInlineKey decrypts an inline Asset with the Alter's local identity and
// writes the private key under $AXF_HOME/keys/ssh/<alter>/.
func writeInlineKey(t Target, asset alter.Asset) (string, error) {
	if err := checkFileNameStem(ErrInvalidAssetName, asset.Name); err != nil {
		return "", fmt.Errorf("ssh-keypair: %w", err)
	}
	identityPath, err := IdentityPath(t.Home, t.Name)
	if err != nil {
		return "", fmt.Errorf("ssh-keypair: %w", err)
	}
	identities, err := LoadIdentities(identityPath)
	if err != nil {
		return "", fmt.Errorf("ssh-keypair: inline asset %q of Alter %q: %w", asset.Name, t.Name, err)
	}
	plaintext, err := DecryptAsset(asset, identities, identityPath)
	if err != nil {
		return "", fmt.Errorf("ssh-keypair: %w", err)
	}
	defer zero(plaintext)
	path := filepath.Join(t.Home, "keys", "ssh", t.Name, asset.Name)
	if err := writeSecret(path, plaintext, os.O_TRUNC); err != nil {
		return "", fmt.Errorf("ssh-keypair: inline asset %q: %w", asset.Name, err)
	}
	return path, nil
}
