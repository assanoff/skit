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
	Full       bool   // render the full HTTP service bootstrap instead of the minimal starter
	Replace    string // local skit path for a go.mod replace directive (co-developing the SDK)
}

type templateData struct {
	Module     string
	Name       string // module's last element, e.g. bonus-service
	Snake      string // Name with '-' -> '_', e.g. bonus_service (db name, metrics namespace)
	SDKVersion string
	SDKRequire bool
	Replace    string // when set, go.mod gets a replace => Replace directive
}

// scaffold creates a new project. With Template set it delegates to gonew; with
// Full it renders the full HTTP service bootstrap; otherwise it renders the
// minimal embedded starter into Dir.
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

	// --replace only has an effect on the full bootstrap (its go.mod carries the
	// replace directive); the minimal starter and the gonew passthrough ignore
	// it. Fail loudly rather than scaffold a go.mod silently missing the replace.
	if opts.Replace != "" && !opts.Full {
		return fmt.Errorf("--replace requires --full: the minimal starter has no replace directive (re-run with --full, or drop --replace)")
	}

	if opts.Template != "" {
		return runGonew(out, opts.Template, opts.Module, dir)
	}
	if opts.Full {
		return renderFullStarter(out, opts, name, dir)
	}
	return renderStarter(out, opts, name, dir)
}

// newData builds the template payload shared by the starter renderers.
func newData(opts scaffoldOpts, name string) templateData {
	return templateData{
		Module:     opts.Module,
		Name:       name,
		Snake:      strings.ReplaceAll(name, "-", "_"),
		SDKVersion: opts.SDKVersion,
		SDKRequire: opts.SDKVersion != "" && opts.SDKVersion != "latest",
		Replace:    opts.Replace,
	}
}

// renderStarter writes the embedded minimal starter templates into dir.
func renderStarter(out io.Writer, opts scaffoldOpts, name, dir string) error {
	if err := ensureEmptyDir(dir); err != nil {
		return err
	}

	data := newData(opts, name)

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

// renderFullStarter renders the full HTTP service bootstrap (templates/full)
// into dir. Unlike renderStarter it tolerates a non-empty directory (e.g. an
// existing git repo with docs): it writes only files that do not yet exist and
// refuses on any per-file collision, so existing work is never overwritten.
func renderFullStarter(out io.Writer, opts scaffoldOpts, name, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	data := newData(opts, name)

	const root = "templates/full"
	type job struct{ dest, tmpl string }
	var jobs []job
	walkErr := fs.WalkDir(templatesFS, root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() || !strings.HasSuffix(p, ".tmpl") {
			return nil
		}
		rel := strings.TrimPrefix(p, root+"/")
		jobs = append(jobs, job{dest: filepath.Join(dir, fullOutputPath(rel)), tmpl: p})
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("walk templates: %w", walkErr)
	}

	// Refuse before writing anything if a target already exists.
	for _, j := range jobs {
		if _, err := os.Stat(j.dest); err == nil {
			return fmt.Errorf("%s already exists — refusing to overwrite", j.dest)
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	for _, j := range jobs {
		if err := renderFile(j.dest, j.tmpl, data); err != nil {
			return err
		}
		fmt.Fprintf(out, "  created %s\n", j.dest)
	}

	printFullNextSteps(out, opts, dir)
	return nil
}

// fullOutputPath maps a template's path (relative to templates/full, with the
// .tmpl suffix) to its on-disk name, restoring dotfiles that go:embed cannot
// carry with a leading dot.
func fullOutputPath(rel string) string {
	dest := strings.TrimSuffix(rel, ".tmpl")
	switch filepath.Base(dest) {
	case "gitignore":
		dest = filepath.Join(filepath.Dir(dest), ".gitignore")
	case "env.example":
		dest = filepath.Join(filepath.Dir(dest), ".env.example")
	}
	return dest
}

func printFullNextSteps(out io.Writer, opts scaffoldOpts, dir string) {
	fmt.Fprintf(out, `
Scaffolded the full HTTP service %q in %s/

Next:
  cd %s
  go mod tidy
  docker compose up -d            # postgres (+ rabbitmq)
  go run . migrate up
  go run . serve                  # GET /healthz, /readyz, /hello

Add a domain module with:
  skit add rest <name>
`, opts.Module, dir, dir)
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
