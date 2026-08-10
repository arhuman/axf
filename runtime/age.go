package runtime

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"filippo.io/age"

	alter "github.com/arhuman/axf"
)

// ErrIdentityExists reports a decryption identity that already exists on disk.
var ErrIdentityExists = errors.New("runtime: decryption identity already exists")

// ErrNoIdentity reports that no local decryption identity is available for an
// Alter, so its inline Assets cannot be opened.
var ErrNoIdentity = errors.New("runtime: no decryption identity")

// ErrDecrypt reports an inline Asset that could not be decrypted.
var ErrDecrypt = errors.New("runtime: cannot decrypt asset")

// maxAssetPlaintext bounds how much decrypted content DecryptAsset reads into
// memory. Every inline Asset this build produces a provider for (ssh-keypair)
// holds a private key, at most a few KB; 1 MiB is generous headroom for that
// while still bounding a malformed or oversized ciphertext.
const maxAssetPlaintext = 1 << 20

// IdentityPath returns the age identity file of one Alter,
// $AXF_HOME/keys/<name>.age.
//
// Identities are scoped per Alter, never shared: a single decryption key across
// Alters would let whoever holds it correlate them, which is exactly what the
// per-Alter owner key of spec section 3 exists to prevent.
func IdentityPath(home, name string) (string, error) {
	if err := checkAlterName(name); err != nil {
		return "", err
	}
	return filepath.Join(home, "keys", name+".age"), nil
}

// GenerateIdentity creates the decryption identity of the named Alter and
// returns the file it was written to together with its recipient, the age1...
// public key to add by hand to assets[].encryption.recipients[].
//
// It never overwrites an existing identity and returns an error wrapping
// ErrIdentityExists instead: replacing one silently would make every Asset
// already encrypted for it permanently unreadable. Rotating is therefore a
// deliberate act, removing the file first.
func GenerateIdentity(home, name string) (path, recipient string, err error) {
	path, err = IdentityPath(home, name)
	if err != nil {
		return "", "", err
	}
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", fmt.Errorf("runtime: generating an age identity: %w", err)
	}
	recipient = identity.Recipient().String()
	// The age-keygen file format: comment lines, then the secret key, which is
	// what ParseIdentities reads back and what any age tool already understands.
	content := fmt.Sprintf("# created: %s\n# public key: %s\n%s\n",
		time.Now().UTC().Format(time.RFC3339), recipient, identity)
	if err := writeSecret(path, []byte(content), os.O_EXCL); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", "", fmt.Errorf("%w: %s (remove it first to rotate)", ErrIdentityExists, path)
		}
		return "", "", err
	}
	return path, recipient, nil
}

// LoadIdentities reads the age identities held at path. A missing or empty file
// yields an error wrapping ErrNoIdentity that names the path looked at.
func LoadIdentities(path string) ([]age.Identity, error) {
	// path is always IdentityPath(home, name/t.Name), and name has already
	// passed checkAlterName before reaching here (see Store.Load's ordering).
	f, err := os.Open(path) //nolint:gosec // G304: name is traversal-checked upstream
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w at %s", ErrNoIdentity, path)
		}
		return nil, fmt.Errorf("runtime: opening %s: %w", path, err)
	}
	defer f.Close()
	identities, err := age.ParseIdentities(f)
	if err != nil {
		return nil, fmt.Errorf("runtime: %s is not an age identity file: %w", path, err)
	}
	return identities, nil
}

// DecryptAsset returns the plaintext of an inline Asset. source names where the
// identities came from, for the error message only.
//
// encryption.ciphertext is base64 wrapping an age binary envelope (spec section
// 10), so it is decoded before being handed to age. Every failure is reported
// against the Asset name and the identity source: age reports a recipient
// mismatch as a bare "no identity matched any of the recipients", which says
// neither which Asset failed nor which key was tried.
func DecryptAsset(asset alter.Asset, identities []age.Identity, source string) ([]byte, error) {
	if asset.Encryption == nil || asset.Encryption.Ciphertext == "" {
		return nil, fmt.Errorf("%w %q: it carries no encryption.ciphertext", ErrDecrypt, asset.Name)
	}
	// An absent algorithm is read as the recommended one; jwe is a valid v0
	// value this build cannot open, and must say so rather than fail as corrupt.
	if alg := asset.Encryption.Algorithm; alg != "" && alg != alter.EncryptionAgeX25519 {
		return nil, fmt.Errorf("%w %q: algorithm %q is not supported, want %q",
			ErrDecrypt, asset.Name, alg, alter.EncryptionAgeX25519)
	}
	envelope, err := base64.StdEncoding.DecodeString(asset.Encryption.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("%w %q: encryption.ciphertext is not base64: %w", ErrDecrypt, asset.Name, err)
	}
	r, err := age.Decrypt(bytes.NewReader(envelope), identities...)
	if err != nil {
		var noMatch *age.NoIdentityMatchError
		if errors.As(err, &noMatch) {
			return nil, fmt.Errorf("%w %q: no identity in %s matches any of its %d recipients",
				ErrDecrypt, asset.Name, source, len(asset.Encryption.Recipients))
		}
		return nil, fmt.Errorf("%w %q with the identities in %s: %w", ErrDecrypt, asset.Name, source, err)
	}
	plaintext, err := io.ReadAll(io.LimitReader(r, maxAssetPlaintext+1))
	if err != nil {
		return nil, fmt.Errorf("%w %q with the identities in %s: %w", ErrDecrypt, asset.Name, source, err)
	}
	if len(plaintext) > maxAssetPlaintext {
		return nil, fmt.Errorf("%w %q: decrypted content exceeds %d bytes, larger than any AXF v0 asset is expected to be",
			ErrDecrypt, asset.Name, maxAssetPlaintext)
	}
	return plaintext, nil
}

// zero overwrites b with zero bytes: best-effort defense in depth so a
// decrypted secret does not linger in memory longer than necessary. It is not
// a guarantee: the Go runtime may already have copied b during garbage
// collection, and this process's memory can still be swapped to disk or
// captured by a core dump regardless.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// writeSecret writes data at path with mode 0o600 under a 0o700 parent, the
// modes every file and directory below $AXF_HOME/keys uses. flag is os.O_EXCL to
// refuse an existing file or os.O_TRUNC to replace one.
func writeSecret(path string, data []byte, flag int) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("runtime: creating %s: %w", dir, err)
	}
	// Every caller of writeSecret has already validated path's variable
	// component (IdentityPath's name via checkAlterName, an Asset name via
	// checkFileNameStem in sshkeypair.go) before reaching here.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|flag, 0o600) //nolint:gosec // G304: name is traversal-checked upstream
	if err != nil {
		return fmt.Errorf("runtime: writing %s: %w", path, err)
	}
	// O_CREATE applies the mode only to a file it creates: one replaced through
	// O_TRUNC would otherwise keep whatever mode it already had.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("runtime: securing %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("runtime: writing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("runtime: closing %s: %w", path, err)
	}
	return nil
}
