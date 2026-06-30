package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldStarter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mysvc")
	var out bytes.Buffer

	err := scaffold(&out, scaffoldOpts{
		Module:     "github.com/me/mysvc",
		Dir:        dir,
		SDKVersion: "latest",
	})
	if err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	// go.mod carries the rewritten module path; "latest" omits the require.
	gomod := readFile(t, filepath.Join(dir, "go.mod"))
	if !strings.Contains(gomod, "module github.com/me/mysvc") {
		t.Errorf("go.mod missing module path:\n%s", gomod)
	}
	if strings.Contains(gomod, "require github.com/assanoff/skit") {
		t.Errorf("latest should omit the require:\n%s", gomod)
	}

	// main.go is valid Go and references the derived service name.
	mainPath := filepath.Join(dir, "main.go")
	src := readFile(t, mainPath)
	if !strings.Contains(src, "hello from mysvc") {
		t.Errorf("main.go missing service name:\n%s", src)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), mainPath, nil, parser.AllErrors); err != nil {
		t.Errorf("generated main.go does not parse: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("README.md not generated: %v", err)
	}
}

func TestScaffoldPinnedVersion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "svc")
	if err := scaffold(&bytes.Buffer{}, scaffoldOpts{
		Module:     "github.com/me/svc",
		Dir:        dir,
		SDKVersion: "v1.2.3",
	}); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	gomod := readFile(t, filepath.Join(dir, "go.mod"))
	if !strings.Contains(gomod, "require github.com/assanoff/skit v1.2.3") {
		t.Errorf("pinned version not written:\n%s", gomod)
	}
}

func TestScaffoldRefusesNonEmptyDir(t *testing.T) {
	dir := t.TempDir() // already exists and non-empty after we drop a file
	if err := os.WriteFile(filepath.Join(dir, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := scaffold(&bytes.Buffer{}, scaffoldOpts{Module: "github.com/me/svc", Dir: dir})
	if err == nil {
		t.Fatal("expected scaffold to refuse a non-empty directory")
	}
}

func TestScaffoldReplaceRequiresFull(t *testing.T) {
	// --replace without --full must fail before writing anything: the minimal
	// go.mod has no replace directive, so silently succeeding would mislead.
	dir := filepath.Join(t.TempDir(), "svc")
	err := scaffold(&bytes.Buffer{}, scaffoldOpts{
		Module:  "github.com/me/svc",
		Dir:     dir,
		Replace: "../skit",
	})
	if err == nil {
		t.Fatal("expected --replace without --full to be rejected")
	}
	if !strings.Contains(err.Error(), "--replace requires --full") {
		t.Errorf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("validation should fail before creating %s", dir)
	}
}

func TestScaffoldFullWithReplace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "svc")
	if err := scaffold(&bytes.Buffer{}, scaffoldOpts{
		Module:  "github.com/me/svc",
		Dir:     dir,
		Full:    true,
		Replace: "../skit",
	}); err != nil {
		t.Fatalf("scaffold --full --replace: %v", err)
	}

	// go.mod carries the replace directive pointing at the local SDK path.
	gomod := readFile(t, filepath.Join(dir, "go.mod"))
	if !strings.Contains(gomod, "replace github.com/assanoff/skit => ../skit") {
		t.Errorf("go.mod missing replace directive:\n%s", gomod)
	}

	// The cmd entrypoint and its subcommands are scaffolded and parse as Go.
	for _, rel := range []string{
		"main.go",
		filepath.Join("internal", "cmd", "root.go"),
		filepath.Join("internal", "cmd", "serve.go"),
		filepath.Join("internal", "cmd", "migrate.go"),
		filepath.Join("internal", "cmd", "version.go"),
	} {
		p := filepath.Join(dir, rel)
		if _, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.AllErrors); err != nil {
			t.Errorf("generated %s does not parse: %v", rel, err)
		}
	}

	// Dotfiles that go:embed cannot carry with a leading dot are restored.
	for _, dotfile := range []string{".gitignore", ".env.example"} {
		if _, err := os.Stat(filepath.Join(dir, dotfile)); err != nil {
			t.Errorf("%s not generated: %v", dotfile, err)
		}
	}
}

func TestScaffoldFullRefusesCollision(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "svc")
	// Pre-create a file the full bootstrap would write.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := scaffold(&bytes.Buffer{}, scaffoldOpts{
		Module: "github.com/me/svc",
		Dir:    dir,
		Full:   true,
	})
	if err == nil {
		t.Fatal("expected full scaffold to refuse overwriting an existing file")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
