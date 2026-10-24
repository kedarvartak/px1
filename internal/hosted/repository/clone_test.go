package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type captureRunner struct {
	args []string
	env  []string
	err  error
}

func (r *captureRunner) Run(_ context.Context, _ string, args, env []string) error {
	r.args, r.env = append([]string(nil), args...), append([]string(nil), env...)
	return r.err
}

func TestCloneUsesFilteredBareShallowClone(t *testing.T) {
	runner := &captureRunner{}
	cloner := Cloner{TempRoot: t.TempDir(), Runner: runner}
	clone, err := cloner.Clone(context.Background(), "https://github.com/acme/service.git", "main")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clone.Remove() })
	wantPrefix := []string{"-c", "protocol.file.allow=never", "-c", "protocol.ext.allow=never", "-c", "credential.helper=", "clone", "--bare", "--depth=1", "--filter=blob:limit=1m", "--no-tags", "--single-branch", "--branch=main", "--", "https://github.com/acme/service.git"}
	if len(runner.args) != len(wantPrefix)+1 || !reflect.DeepEqual(runner.args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("clone args = %#v", runner.args)
	}
	if filepath.Base(clone.Dir) != "objects.git" {
		t.Fatalf("clone dir = %q", clone.Dir)
	}
}

func TestCloneRejectsUnsafeRemoteBeforeCreatingDirectory(t *testing.T) {
	root := t.TempDir()
	cloner := Cloner{TempRoot: root, Runner: &captureRunner{}}
	for _, remote := range []string{"file:///tmp/repo", "https://token@github.com/acme/repo", "https://evil.example/repo"} {
		if _, err := cloner.Clone(context.Background(), remote, "main"); err == nil {
			t.Fatalf("expected %q to be rejected", remote)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary root contains %#v (err %v)", entries, err)
	}
}

func TestFailedCloneRemovesTemporaryDirectory(t *testing.T) {
	root := t.TempDir()
	cloner := Cloner{TempRoot: root, Runner: &captureRunner{err: errors.New("network unavailable")}}
	if _, err := cloner.Clone(context.Background(), "https://github.com/acme/repo", "main"); err == nil {
		t.Fatal("expected clone failure")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary clone was not removed: %#v (err %v)", entries, err)
	}
}
