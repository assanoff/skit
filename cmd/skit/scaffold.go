package main

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed templates
var templatesFS embed.FS

// scaffoldOpts configures a `skit new` run.
type scaffoldOpts struct {
	Module     string // new module path, e.g. github.com/you/svc
	Dir        string // target directory (defaults to the module's last element)
	SDKVersion string // skit version for go.mod ("" / "latest" omits the require)
	Template   string // gonew template module; when set, delegates to gonew
}

type templateData struct {
	Module     string
	Name       string
	SDKVersion string
	SDKRequire bool
}

// scaffold creates a new project. With Template set it delegates to gonew;
// otherwise it renders the embedded starter templates into Dir.
func scaffold(out io.Writer, opts scaffoldOpts) error {
	if opts.Module == "" {
		return fmt.Errorf("module path is required")
	}
	name := opts.Module[strings.LastIndex(opts.Module, "/")+1:]
	if name == "" {
		return fmt.Errorf("could not derive a name from module path %q", opts.Module)
	}
	dir := opts.Dir
	if dir == "" {
		dir = name
	}

	if opts.Template != "" {
		return runGonew(out, opts.Template, opts.Module, dir)
	}
	return renderStarter(out, opts, name, dir)
}

// renderStarter writes the embedded starter templates into dir.
func renderStarter(out io.Writer, opts scaffoldOpts, name, dir string) error {
	if err := ensureEmptyDir(dir); err != nil {
		return err
	}

	data := templateData{
		Module:     opts.Module,
		Name:       name,
		SDKVersion: opts.SDKVersion,
		SDKRequire: opts.SDKVersion != "" && opts.SDKVersion != "latest",
	}

	entries, err := fs.ReadDir(templatesFS, "templates")
	if err != nil {
		return fmt.Errorf("read templates: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tmpl") {
			continue
		}
		outName := strings.TrimSuffix(e.Name(), ".tmpl")
		if err := renderFile(filepath.Join(dir, outName), "templates/"+e.Name(), data); err != nil {
			return err
		}
		fmt.Fprintf(out, "  created %s\n", filepath.Join(dir, outName))
	}

	fmt.Fprintf(out, "\nScaffolded %q in %s/\n\nNext:\n  cd %s && go mod tidy && go run .\n", opts.Module, dir, dir)
	return nil
}

func renderFile(dest, tmplPath string, data any) error {
	tmpl, err := template.ParseFS(templatesFS, tmplPath)
	if err != nil {
		return fmt.Errorf("parse %s: %w", tmplPath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("render %s: %w", tmplPath, err)
	}

	out := buf.Bytes()
	// gofmt generated Go so templates need not worry about spacing (e.g. the
	// protobuf builder literals); also catches a malformed template early.
	if strings.HasSuffix(dest, ".go") {
		formatted, ferr := format.Source(out)
		if ferr != nil {
			return fmt.Errorf("format %s: %w", dest, ferr)
		}
		out = formatted
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
	}
	if err := os.WriteFile(dest, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	return nil
}

// ensureEmptyDir creates dir if missing and errors if it exists with content,
// so we never overwrite an existing project.
func ensureEmptyDir(dir string) error {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return os.MkdirAll(dir, 0o755)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s exists and is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s is not empty", dir)
	}
	return nil
}

// runGonew delegates to the gonew tool, preferring an installed binary and
// falling back to `go run golang.org/x/tools/cmd/gonew@latest`.
func runGonew(out io.Writer, templateModule, module, dir string) error {
	var cmd *exec.Cmd
	if path, err := exec.LookPath("gonew"); err == nil {
		cmd = exec.Command(path, templateModule, module, dir)
	} else {
		fmt.Fprintln(out, "gonew not found in PATH; using `go run golang.org/x/tools/cmd/gonew@latest`")
		cmd = exec.Command("go", "run", "golang.org/x/tools/cmd/gonew@latest", templateModule, module, dir)
	}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gonew: %w", err)
	}
	fmt.Fprintf(out, "\nScaffolded %q from template %q in %s/\n", module, templateModule, dir)
	return nil
}
