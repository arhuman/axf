package runtime_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	alter "github.com/arhuman/axf"
	"github.com/arhuman/axf/runtime"
)

// encryptFor returns what an inline Asset carries in encryption.ciphertext: the
// age binary envelope, base64 encoded. Tests build their own ciphertext rather
// than hold a fixture blob, because only the identity a test just generated can
// open it.
func encryptFor(t *testing.T, plaintext string, recipients ...string) string {
	t.Helper()
	parsed := make([]age.Recipient, 0, len(recipients))
	for _, r := range recipients {
		x, err := age.ParseX25519Recipient(r)
		if err != nil {
			t.Fatalf("parsing recipient %q: %v", r, err)
		}
		parsed = append(parsed, x)
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, parsed...)
	if err != nil {
		t.Fatalf("age.Encrypt() error = %v", err)
	}
	if _, err := io.WriteString(w, plaintext); err != nil {
		t.Fatalf("writing the plaintext: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing the age writer: %v", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// inlineAsset builds an inline Asset carrying ciphertext.
func inlineAsset(name, ciphertext string, recipients ...string) alter.Asset {
	return alter.Asset{
		Name: name,
		Kind: alter.AssetKindInline,
		Encryption: &alter.Encryption{
			Algorithm:  alter.EncryptionAgeX25519,
			Recipients: recipients,
			Ciphertext: ciphertext,
		},
	}
}

// mode reports the permission bits of a path.
func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

// A decryption identity is key material: it lands in a 0o700 directory as a
// 0o600 file, in the format any age tool already reads.
func TestGenerateIdentity(t *testing.T) {
	home := t.TempDir()
	path, recipient, err := runtime.GenerateIdentity(home, "alchemist")
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}
	if want := filepath.Join(home, "keys", "alchemist.age"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if !strings.HasPrefix(recipient, "age1") {
		t.Errorf("recipient = %q, want an age1... public key", recipient)
	}
	if got := mode(t, path); got != 0o600 {
		t.Errorf("identity mode = %v, want 0600", got)
	}
	if got := mode(t, filepath.Dir(path)); got != 0o700 {
		t.Errorf("keys directory mode = %v, want 0700", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the identity: %v", err)
	}
	for _, want := range []string{"# public key: " + recipient, "AGE-SECRET-KEY-1"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("identity file = %q, want it to contain %q", data, want)
		}
	}
}

// Overwriting an identity would make every Asset already encrypted for it
// unreadable, with nothing on screen saying so.
func TestGenerateIdentityRefusesToOverwrite(t *testing.T) {
	home := t.TempDir()
	path, _, err := runtime.GenerateIdentity(home, "alchemist")
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the identity: %v", err)
	}
	if _, _, err := runtime.GenerateIdentity(home, "alchemist"); !errors.Is(err, runtime.ErrIdentityExists) {
		t.Fatalf("second GenerateIdentity() error = %v, want it to wrap ErrIdentityExists", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the identity again: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the identity file changed, want the original key left untouched")
	}
}

// The identity path is built from a store name, which must stay a bare file
// name stem for the same reason the Alter document path does.
func TestIdentityPathRejectsTraversal(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../escape", "sub/alter", `back\slash`, "nul\x00byte"} {
		t.Run(name, func(t *testing.T) {
			if _, err := runtime.IdentityPath("/tmp/axf-home", name); !errors.Is(err, runtime.ErrInvalidAlterName) {
				t.Errorf("IdentityPath(%q) error = %v, want it to wrap ErrInvalidAlterName", name, err)
			}
			if _, _, err := runtime.GenerateIdentity(t.TempDir(), name); !errors.Is(err, runtime.ErrInvalidAlterName) {
				t.Errorf("GenerateIdentity(%q) error = %v, want it to wrap ErrInvalidAlterName", name, err)
			}
		})
	}
}

func TestLoadIdentities(t *testing.T) {
	home := t.TempDir()
	path, _, err := runtime.GenerateIdentity(home, "alchemist")
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}

	t.Run("reads back a generated identity", func(t *testing.T) {
		identities, err := runtime.LoadIdentities(path)
		if err != nil {
			t.Fatalf("LoadIdentities() error = %v", err)
		}
		if len(identities) != 1 {
			t.Errorf("got %d identities, want 1", len(identities))
		}
	})

	t.Run("a missing file names the path looked at", func(t *testing.T) {
		missing := filepath.Join(home, "keys", "ghost.age")
		_, err := runtime.LoadIdentities(missing)
		if !errors.Is(err, runtime.ErrNoIdentity) {
			t.Fatalf("LoadIdentities() error = %v, want it to wrap ErrNoIdentity", err)
		}
		if !strings.Contains(err.Error(), missing) {
			t.Errorf("error = %v, want it to name %s", err, missing)
		}
	})

	// A file holding no usable key is reported against its path, whether it is
	// empty of keys or not an identity file at all.
	for _, tt := range []struct {
		name    string
		content string
	}{
		{name: "only comments", content: "# created: whenever\n"},
		{name: "not an identity at all", content: "not a key\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(home, "keys", tt.name+".age")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("writing %s: %v", path, err)
			}
			_, err := runtime.LoadIdentities(path)
			if err == nil || !strings.Contains(err.Error(), "not an age identity file") {
				t.Errorf("LoadIdentities() error = %v, want it to say the file is not an identity", err)
			}
			if err != nil && !strings.Contains(err.Error(), path) {
				t.Errorf("error = %v, want it to name %s", err, path)
			}
		})
	}
}

func TestDecryptAsset(t *testing.T) {
	home := t.TempDir()
	identityPath, recipient, err := runtime.GenerateIdentity(home, "alchemist")
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}
	identities, err := runtime.LoadIdentities(identityPath)
	if err != nil {
		t.Fatalf("LoadIdentities() error = %v", err)
	}
	_, other, err := runtime.GenerateIdentity(home, "researcher")
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}

	const secret = "-----BEGIN OPENSSH PRIVATE KEY-----\nnot really a key\n"

	t.Run("round trip", func(t *testing.T) {
		asset := inlineAsset("ssh-key", encryptFor(t, secret, recipient), recipient)
		got, err := runtime.DecryptAsset(asset, identities, identityPath)
		if err != nil {
			t.Fatalf("DecryptAsset() error = %v", err)
		}
		if string(got) != secret {
			t.Errorf("plaintext = %q, want %q", got, secret)
		}
	})

	t.Run("an absent algorithm is read as age-x25519", func(t *testing.T) {
		asset := inlineAsset("ssh-key", encryptFor(t, secret, recipient), recipient)
		asset.Encryption.Algorithm = ""
		if _, err := runtime.DecryptAsset(asset, identities, identityPath); err != nil {
			t.Errorf("DecryptAsset() error = %v", err)
		}
	})

	tests := []struct {
		name  string
		asset alter.Asset
		want  string
	}{
		{
			// age answers a recipient mismatch with a message naming neither the
			// Asset nor the key that was tried.
			name:  "encrypted for someone else",
			asset: inlineAsset("ssh-key", encryptFor(t, secret, other), other),
			want:  "no identity in " + identityPath + " matches",
		},
		{
			name:  "ciphertext is not base64",
			asset: inlineAsset("ssh-key", "not base64 at all!", recipient),
			want:  "not base64",
		},
		{
			name:  "base64 of something that is not an age envelope",
			asset: inlineAsset("ssh-key", base64.StdEncoding.EncodeToString([]byte("junk")), recipient),
			want:  `"ssh-key" with the identities in ` + identityPath,
		},
		{
			name:  "no ciphertext at all",
			asset: alter.Asset{Name: "ssh-key", Kind: alter.AssetKindInline, Encryption: &alter.Encryption{}},
			want:  "no encryption.ciphertext",
		},
		{
			name:  "no encryption block at all",
			asset: alter.Asset{Name: "ssh-key", Kind: alter.AssetKindInline},
			want:  "no encryption.ciphertext",
		},
		{
			// jwe is a valid v0 algorithm this build cannot open. Saying so beats
			// failing as though the envelope were corrupt.
			name: "an algorithm this build does not implement",
			asset: func() alter.Asset {
				a := inlineAsset("ssh-key", encryptFor(t, secret, recipient), recipient)
				a.Encryption.Algorithm = alter.EncryptionJWE
				return a
			}(),
			want: `algorithm "jwe" is not supported`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runtime.DecryptAsset(tt.asset, identities, identityPath)
			if !errors.Is(err, runtime.ErrDecrypt) {
				t.Fatalf("DecryptAsset() error = %v, want it to wrap ErrDecrypt", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), tt.asset.Name) {
				t.Errorf("error = %v, want it to name the asset", err)
			}
		})
	}
}

// A keys directory that cannot be created is reported as the filesystem problem
// it is, not as an existing identity.
func TestGenerateIdentityReportsAnUnusableKeysDirectory(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "keys"), nil, 0o600); err != nil {
		t.Fatalf("writing the blocking file: %v", err)
	}
	_, _, err := runtime.GenerateIdentity(home, "alchemist")
	if err == nil || errors.Is(err, runtime.ErrIdentityExists) {
		t.Fatalf("GenerateIdentity() error = %v, want the directory failure reported", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(home, "keys")) {
		t.Errorf("error = %v, want it to name the keys directory", err)
	}
}
