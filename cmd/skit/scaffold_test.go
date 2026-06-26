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

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
