package monitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
)

func TestLocalCorednsMonitorRefreshAndRecovery(t *testing.T) {
	dir := t.TempDir()
	templatePath, outputPath := filepath.Join(dir, "Corefile.tmpl"), filepath.Join(dir, "Corefile")
	fallback, noStub := filepath.Join(dir, "resolv.conf"), filepath.Join(dir, "no-stub-resolv.conf")
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	remove := func(path string) {
		t.Helper()
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	write(templatePath, "{{.NonVirtualIP}} {{index .DNSUpstreams 0}}")
	write(fallback, "192.0.2.53")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var firstInfo os.FileInfo
	nodeIP := "192.0.2.10"
	steps := []struct {
		wantPrevious string
		wantSource   string
		before       func()
		buildErr     error
	}{
		{wantSource: fallback},
		{wantPrevious: "192.0.2.10 192.0.2.53", wantSource: fallback, before: func() {
			var err error
			firstInfo, err = os.Stat(outputPath)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{wantPrevious: "192.0.2.10 192.0.2.53", wantSource: fallback, before: func() {
			info, err := os.Stat(outputPath)
			if err != nil || !os.SameFile(firstInfo, info) {
				t.Fatalf("unchanged output replaced: %v", err)
			}
			write(fallback+".new", "192.0.2.54")
			if err := os.Rename(fallback+".new", fallback); err != nil {
				t.Fatal(err)
			}
		}},
		{wantPrevious: "192.0.2.10 192.0.2.54", wantSource: fallback, before: func() {
			nodeIP = "2001:db8::10"
			write(templatePath, "updated {{.NonVirtualIP}} {{index .DNSUpstreams 0}}")
			write(noStub, "2001:db8::53")
		}},
		{wantPrevious: "updated 2001:db8::10 192.0.2.54", wantSource: noStub},
		{wantPrevious: "updated 2001:db8::10 2001:db8::53", wantSource: noStub, before: func() { remove(noStub) }},
		{wantPrevious: "updated 2001:db8::10 2001:db8::53", wantSource: fallback, before: func() { write(templatePath, "{{.MissingField}}") }},
		{wantPrevious: "updated 2001:db8::10 2001:db8::53", wantSource: fallback, before: func() { write(templatePath, "{{.NonVirtualIP}} {{index .DNSUpstreams 0}}") }},
		{wantPrevious: "2001:db8::10 192.0.2.54", wantSource: fallback, buildErr: errors.New("temporary discovery failure")},
		{wantPrevious: "2001:db8::10 192.0.2.54", wantSource: fallback, before: func() {
			if err := os.Rename(outputPath, outputPath+".saved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outputPath+".saved", outputPath); err != nil {
				t.Fatal(err)
			}
			nodeIP = "192.0.2.12"
		}},
		{wantPrevious: "2001:db8::10 192.0.2.54", wantSource: fallback, before: func() {
			remove(outputPath)
			if err := os.Rename(outputPath+".saved", outputPath); err != nil {
				t.Fatal(err)
			}
		}},
		{wantPrevious: "192.0.2.12 192.0.2.54", wantSource: fallback, before: cancel},
	}
	attempt := 0
	getConfig := func(ctx context.Context, kubeconfig, clusterConfig, path string) (config.Node, error) {
		if path != "" {
			t.Fatalf("default resolver path was pinned: %q", path)
		}
		path, err := config.GetNetworkManagerResolvConfPath(dir)
		if err != nil {
			return config.Node{}, err
		}
		if attempt >= len(steps) {
			t.Fatal("monitor did not stop")
		}
		step := steps[attempt]
		attempt++
		if path != step.wantSource {
			t.Fatalf("attempt %d source: got %s, want %s", attempt, path, step.wantSource)
		}
		if kubeconfig != "unreachable-api-kubeconfig" || clusterConfig != "" {
			t.Fatal("wrong configuration inputs")
		}
		if step.wantPrevious == "" {
			if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
				t.Fatalf("unexpected initial output: %v", err)
			}
		} else {
			content, err := os.ReadFile(outputPath)
			if err != nil || string(content) != step.wantPrevious {
				t.Fatalf("attempt %d previous output: %q, %v", attempt, content, err)
			}
		}
		if step.before != nil {
			step.before()
		}
		if step.buildErr != nil {
			return config.Node{}, step.buildErr
		}
		if err := ctx.Err(); err != nil {
			return config.Node{}, err
		}
		upstream, err := os.ReadFile(path)
		if err != nil {
			return config.Node{}, err
		}
		return config.Node{NonVirtualIP: nodeIP, DNSUpstreams: []string{strings.TrimSpace(string(upstream))}}, nil
	}
	if err := corednsWatchWithNodeIPDiscovery(ctx, "unreachable-api-kubeconfig", "", templatePath, outputPath, "", time.Millisecond, getConfig); err != nil {
		t.Fatal(err)
	}
	if attempt != len(steps) {
		t.Fatalf("only completed %d/%d attempts", attempt, len(steps))
	}
}

func TestLocalCorednsMonitorCancellation(t *testing.T) {
	for _, duringDiscovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "polling", true: "discovery"}[duringDiscovery], func(t *testing.T) {
			dir := t.TempDir()
			templatePath, outputPath := filepath.Join(dir, "Corefile.tmpl"), filepath.Join(dir, "Corefile")
			if err := os.WriteFile(templatePath, []byte("{{.NonVirtualIP}}"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			done := make(chan error, 1)
			getConfig := func(attempt context.Context, _, _, path string) (config.Node, error) {
				if path != "resolv.conf" {
					return config.Node{}, errors.New("explicit resolver path was not preserved")
				}
				close(started)
				if duringDiscovery {
					<-attempt.Done()
					return config.Node{}, attempt.Err()
				}
				return config.Node{NonVirtualIP: "192.0.2.10"}, nil
			}
			go func() {
				done <- corednsWatchWithNodeIPDiscovery(ctx, "", "", templatePath, outputPath, "resolv.conf", time.Hour, getConfig)
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("monitor did not render immediately")
			}
			if !duringDiscovery {
				deadline := time.Now().Add(time.Second)
				for {
					if _, err := os.Stat(outputPath); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("initial render not published")
					}
					time.Sleep(time.Millisecond)
				}
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("monitor did not stop promptly")
			}
		})
	}
}

func TestLocalCorednsMonitorRejectsInvalidOptions(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		if err := CorednsWatchWithNodeIPDiscovery(context.Background(), "", "", "", "", "", interval); err == nil {
			t.Fatal("accepted invalid monitor interval")
		}
	}
}
