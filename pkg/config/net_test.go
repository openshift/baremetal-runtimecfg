package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetIPFromFileTrimsWhitespace(t *testing.T) {
	path := t.TempDir() + "/primary-ip"
	if err := os.WriteFile(path, []byte("2001:db8::1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	ip, err := GetIpFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ip.String(), "2001:db8::1"; got != want {
		t.Fatalf("IP: got %q, want %q", got, want)
	}
}

func TestGetIPFromFileFailsWhenMissing(t *testing.T) {
	_, err := GetIpFromFile(t.TempDir() + "/primary-ip")
	if err == nil {
		t.Fatal("expected missing primary IP error")
	}
}

func TestNetworkManagerResolvConfSelection(t *testing.T) {
	dir := t.TempDir()
	fallback, noStub := filepath.Join(dir, "resolv.conf"), filepath.Join(dir, "no-stub-resolv.conf")
	assertPath := func(want string) {
		t.Helper()
		got, err := GetNetworkManagerResolvConfPath(dir)
		if err != nil || got != want {
			t.Fatalf("source: got %q, want %q, error: %v", got, want, err)
		}
	}
	assertPath(fallback)
	writeDNSFixture(t, noStub, "")
	assertPath(noStub)
	if err := os.Remove(noStub); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), noStub); err != nil {
		t.Fatal(err)
	}
	assertPath(noStub)
	if err := os.Remove(noStub); err != nil {
		t.Fatal(err)
	}
	assertPath(fallback)
}
