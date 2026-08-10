package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AuditResult is the decision one audit event records.
type AuditResult string

// Audit results. These are the only two values spec section 15 defines.
const (
	ResultAllowed AuditResult = "allowed"
	ResultDenied  AuditResult = "denied"
)

// AuditEvent is one entry of the Alter Guard audit trail, in the shape fixed by
// spec section 15. The trail is a JSON Lines file: one marshalled AuditEvent per
// line, appended and never rewritten.
//
// An event records a decision, not an outcome: Result says what the Guard
// concluded about the action, whether or not the action then succeeded. Spec
// section 15 gives Result no third value, so an action that was authorised and
// later failed on its own is an allowed event.
//
// The event carries capability, action and provider names only, never a value
// read from an Asset. That is what makes policies[].audit.redactSecrets (spec
// section 11) structurally satisfied rather than a filter the Guard has to run:
// there is no field a decrypted secret could reach.
type AuditEvent struct {
	Timestamp string `json:"timestamp"`
	AlterID   string `json:"alterId"`
	// Actor is the opaque per-invocation identifier of NewActor, never a real
	// process identifier of the host.
	Actor      string `json:"actor"`
	Capability string `json:"capability"`
	Action     string `json:"action"`
	// Provider names the provider that handled the action, empty when none did.
	Provider string      `json:"provider"`
	Result   AuditResult `json:"result"`
}

// NewActor returns the opaque identifier the Guard records as the actor of every
// audit event of one axf invocation.
//
// Spec section 15 requires actor to reference a virtual process identifier and
// never the real PID of the host: an audit trail carrying real system
// identifiers would itself leak the pseudonymity the Alter is meant to protect.
// The token is random, scoped to a single invocation and never persisted, so two
// runs of the same Alter cannot be correlated through it either.
func NewActor() string {
	var b [8]byte
	// crypto/rand.Read fills b entirely or panics on a broken system entropy
	// source; it never returns an error, so there is no fallback to write here.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// appendAudit appends one event, as a single JSON line, to the audit log at
// path, creating the file and its directory if needed.
//
// The file is opened with O_APPEND and the line is written in one call, so two
// axf invocations racing on the same $AXF_HOME interleave whole lines rather
// than tearing one. There is no daemon holding the log open, which is the only
// reason a per-write open is affordable here.
func appendAudit(path string, e AuditEvent) error {
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("runtime: encoding the audit event: %w", err)
	}
	line = append(line, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("runtime: creating %s: %w", dir, err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G304: path is Home+"audit.log", no variable component
	if err != nil {
		return fmt.Errorf("runtime: opening the audit log %s: %w", path, err)
	}
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("runtime: appending to the audit log %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("runtime: closing the audit log %s: %w", path, err)
	}
	return nil
}
