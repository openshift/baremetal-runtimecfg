package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/openshift/baremetal-runtimecfg/pkg/config"
)

func TestRender(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Render Suite")
}

func TestRenderDNSAddresses(t *testing.T) {
	dir := t.TempDir()
	templatePath := filepath.Join(dir, "Corefile.tmpl")
	outputPath := filepath.Join(dir, "Corefile")
	template := `{{range .DNSAddresses}}
template IN {{.RecordType}} cluster.example {
    answer "{{"{{ .Name }}"}} 60 IN {{.RecordType}} {{.Address}}"
}

{{end}}`
	if err := os.WriteFile(templatePath, []byte(template), 0600); err != nil {
		t.Fatal(err)
	}

	renderConfig := config.Node{DNSAddresses: []config.DNSAddress{
		{Address: "192.0.2.10", RecordType: "A"},
		{Address: "2001:db8::10", RecordType: "AAAA"},
	}}
	if err := RenderFile(outputPath, templatePath, renderConfig); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"A 192.0.2.10", "AAAA 2001:db8::10"} {
		if !strings.Contains(string(content), expected) {
			t.Fatalf("rendered Corefile does not contain %q:\n%s", expected, content)
		}
	}
}

func TestRenderFilePreservesUnchangedOutputAndRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	templatePath := filepath.Join(dir, "Corefile.tmpl")
	outputPath := filepath.Join(dir, "Corefile")
	if err := os.WriteFile(templatePath, []byte("{{.}}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RenderFile(outputPath, templatePath, "first"); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := RenderFile(outputPath, templatePath, "first"); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("unchanged atomic output replaced its inode")
	}
	link := filepath.Join(dir, "Corefile-link")
	if err := os.Symlink(outputPath, link); err != nil {
		t.Fatal(err)
	}
	if err := RenderFile(link, templatePath, "second"); err == nil {
		t.Fatal("atomic renderer accepted a symlink destination")
	}
}

func TestRenderDirectory(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	for _, name := range []string{"Corefile.tmpl", "other.tmpl", "ignored.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{{.}}\n"), 0640); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "ignored.tmpl"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := Render(out, []string{dir}, "rendered"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(out)
	if err != nil || len(entries) != 2 {
		t.Fatalf("outputs: %v, error: %v", entries, err)
	}
	for _, name := range []string{"Corefile", "other"} {
		path := filepath.Join(out, name)
		content, err := os.ReadFile(path)
		if err != nil || string(content) != "rendered\n" {
			t.Fatalf("%s: content %q, error: %v", name, content, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0640 {
			t.Fatalf("%s: incorrect permissions: %v, %v", name, info, err)
		}
	}
}

func TestRenderFileErrorsPreserveOutput(t *testing.T) {
	for _, failure := range []string{"missing template", "parse error", "execution error", "missing output directory"} {
		t.Run(failure, func(t *testing.T) {
			dir, out := t.TempDir(), t.TempDir()
			templatePath := filepath.Join(dir, "Corefile.tmpl")
			outputPath := filepath.Join(out, "Corefile")
			if err := os.WriteFile(outputPath, []byte("previous"), 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			template := "{{.}}"
			switch failure {
			case "parse error":
				template = "{{"
			case "execution error":
				template = "{{.MissingField}}"
			case "missing output directory":
				out = filepath.Join(out, "missing")
			}
			if failure != "missing template" {
				if err := os.WriteFile(templatePath, []byte(template), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := RenderFile(filepath.Join(out, "Corefile"), templatePath, "new"); err == nil {
				t.Fatal("expected render error")
			}
			content, err := os.ReadFile(outputPath)
			if err != nil || string(content) != "previous" {
				t.Fatalf("previous output changed: %q, %v", content, err)
			}
			after, err := os.Stat(outputPath)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("previous output replaced: %v", err)
			}
		})
	}
}

func TestRenderFileReplacesChangedOutput(t *testing.T) {
	dir := t.TempDir()
	templatePath, outputPath := filepath.Join(dir, "Corefile.tmpl"), filepath.Join(dir, "Corefile")
	if err := os.WriteFile(templatePath, []byte("{{.}}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RenderFile(outputPath, templatePath, "first"); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0600, 0640} {
		if err := os.Chmod(templatePath, mode); err != nil {
			t.Fatal(err)
		}
		if err := RenderFile(outputPath, templatePath, "second"); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(outputPath)
		if err != nil || os.SameFile(before, after) || after.Mode().Perm() != mode {
			t.Fatalf("changed output was not replaced with template permissions: %v, %v", after, err)
		}
		content, err := os.ReadFile(outputPath)
		if err != nil || string(content) != "second" {
			t.Fatalf("content %q, error: %v", content, err)
		}
		before = after
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
}
