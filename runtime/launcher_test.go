package runtime_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/arhuman/axf/runtime"
)

func TestExecLauncherAvailable(t *testing.T) {
	launcher := runtime.ExecLauncher{}
	dir := t.TempDir()

	tests := []struct {
		name  string
		probe string
		want  bool
	}{
		{name: "existing absolute path", probe: dir, want: true},
		{name: "missing absolute path", probe: filepath.Join(dir, "absent")},
		{name: "empty probe", probe: ""},
		{name: "binary not in PATH", probe: "axf-no-such-browser-binary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := launcher.Available(tt.probe); got != tt.want {
				t.Errorf("Available(%q) = %v, want %v", tt.probe, got, tt.want)
			}
		})
	}

	t.Run("binary in PATH", func(t *testing.T) {
		if _, err := exec.LookPath("go"); err != nil {
			t.Skip("no go binary in PATH")
		}
		if !launcher.Available("go") {
			t.Error(`Available("go") = false, want true`)
		}
	})
}

func TestExecLauncherLaunch(t *testing.T) {
	launcher := runtime.ExecLauncher{}

	t.Run("reports a missing binary", func(t *testing.T) {
		if err := launcher.Launch("axf-no-such-browser-binary", nil); err == nil {
			t.Error("Launch() error = nil, want the process failing to start")
		}
	})

	t.Run("returns without waiting for the process", func(t *testing.T) {
		path, err := exec.LookPath("true")
		if err != nil {
			t.Skip("no true binary in PATH")
		}
		if err := launcher.Launch(path, nil); err != nil {
			t.Errorf("Launch(%q) error = %v", path, err)
		}
	})
}
