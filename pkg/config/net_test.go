package config

import (
	"os"
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
