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

// writeGoMod drops a minimal go.mod so addREST can resolve the module path.
func writeGoMod(t *testing.T, dir, module string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAddRESTGeneratesParsableModule(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "github.com/me/svc")

	var out bytes.Buffer
	if err := addREST(&out, addRESTOpts{Dir: dir, Name: "widget"}); err != nil {
		t.Fatalf("addREST: %v", err)
	}

	want := []string{
		"core/widget/widget.go",
		"core/widget/model.go",
		"core/widget/widgetdb/widgetdb.go",
		"core/widget/widgetdb/model.go",
		"core/widget/widgetdb/order.go",
		"core/widget/widgetdb/listing_test.go",
		"api/widget/widget.go",
		"api/widget/model.go",
	}
	fset := token.NewFileSet()
	for _, rel := range want {
		path := filepath.Join(dir, rel)
		src := readFile(t, path)
		if _, err := parser.ParseFile(fset, path, src, parser.AllErrors); err != nil {
			t.Errorf("%s does not parse: %v", rel, err)
		}
	}

	// The generated code references the module path read from go.mod.
	api := readFile(t, filepath.Join(dir, "api/widget/widget.go"))
	if !strings.Contains(api, `widgetcore "github.com/me/svc/core/widget"`) {
		t.Errorf("api import does not use the module path:\n%s", api)
	}
	// Routes are pluralized, and the listing set is scaffolded: offset + cursor.
	if !strings.Contains(api, "GET /widgets") {
		t.Errorf("expected pluralized route /widgets:\n%s", api)
	}
	if !strings.Contains(api, "GET /widgets/cursor") {
		t.Errorf("expected scaffolded cursor route /widgets/cursor:\n%s", api)
	}
	// The core declares the filter and ordering allowlist; the store builds the
	// keyset query.
	model := readFile(t, filepath.Join(dir, "core/widget/model.go"))
	if !strings.Contains(model, "type QueryFilter struct") || !strings.Contains(model, "SortableFields") {
		t.Errorf("core model.go missing QueryFilter/SortableFields:\n%s", model)
	}
	db := readFile(t, filepath.Join(dir, "core/widget/widgetdb/widgetdb.go"))
	if !strings.Contains(db, "func (s *Store) QueryByCursor(") {
		t.Errorf("store missing QueryByCursor:\n%s", db)
	}
	// The generated store test is an external test package that exercises the
	// listing set against a dbtest Postgres container.
	listingTest := readFile(t, filepath.Join(dir, "core/widget/widgetdb/listing_test.go"))
	if !strings.Contains(listingTest, "package widgetdb_test") {
		t.Errorf("listing_test.go should be an external test package:\n%s", listingTest)
	}
	for _, want := range []string{
		"dbtest.NewPostgres",
		"func TestWidgetStoreCursor(",
		"func TestWidgetStoreFilter(",
		"func TestWidgetStoreOrdering(",
	} {
		if !strings.Contains(listingTest, want) {
			t.Errorf("listing_test.go missing %q:\n%s", want, listingTest)
		}
	}
}

func TestAddRESTCamelAndKebabNames(t *testing.T) {
	cases := []struct {
		name      string
		wantPkg   string // package decl
		wantType  string // exported type
		wantRoute string // plural route
	}{
		{"order-line", "package orderline", "type OrderLine struct", "GET /orderlines"},
		{"orderLine", "package orderline", "type OrderLine struct", "GET /orderlines"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeGoMod(t, dir, "github.com/me/svc")
			if err := addREST(&bytes.Buffer{}, addRESTOpts{Dir: dir, Name: tc.name}); err != nil {
				t.Fatalf("addREST: %v", err)
			}
			model := readFile(t, filepath.Join(dir, "core/orderline/model.go"))
			if !strings.Contains(model, tc.wantPkg) || !strings.Contains(model, tc.wantType) {
				t.Errorf("model.go missing %q/%q:\n%s", tc.wantPkg, tc.wantType, model)
			}
			api := readFile(t, filepath.Join(dir, "api/orderline/orderline.go"))
			if !strings.Contains(api, tc.wantRoute) {
				t.Errorf("api missing route %q:\n%s", tc.wantRoute, api)
			}
		})
	}
}

func TestAddRESTPluralOverride(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "github.com/me/svc")
	if err := addREST(&bytes.Buffer{}, addRESTOpts{Dir: dir, Name: "category", Plural: "categories"}); err != nil {
		t.Fatalf("addREST: %v", err)
	}
	api := readFile(t, filepath.Join(dir, "api/category/category.go"))
	if !strings.Contains(api, "GET /categories") {
		t.Errorf("expected overridden plural /categories:\n%s", api)
	}
}

func TestAddRESTRefusesExistingFiles(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "github.com/me/svc")
	if err := addREST(&bytes.Buffer{}, addRESTOpts{Dir: dir, Name: "widget"}); err != nil {
		t.Fatalf("first addREST: %v", err)
	}
	if err := addREST(&bytes.Buffer{}, addRESTOpts{Dir: dir, Name: "widget"}); err == nil {
		t.Fatal("expected addREST to refuse overwriting an existing module")
	}
}

func TestAddRESTRejectsBadName(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "github.com/me/svc")
	if err := addREST(&bytes.Buffer{}, addRESTOpts{Dir: dir, Name: "1widget"}); err == nil {
		t.Fatal("expected rejection of a name starting with a digit")
	}
}

func TestAddRESTMissingGoMod(t *testing.T) {
	dir := t.TempDir() // no go.mod
	if err := addREST(&bytes.Buffer{}, addRESTOpts{Dir: dir, Name: "widget"}); err == nil {
		t.Fatal("expected error when go.mod is absent and --module is unset")
	}
}

func TestAddGRPCGeneratesProtoAndParsableHandler(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "github.com/me/svc")

	var out bytes.Buffer
	if err := addGRPC(&out, addRESTOpts{Dir: dir, Name: "widget"}); err != nil {
		t.Fatalf("addGRPC: %v", err)
	}

	// The handler is valid, gofmt-clean Go (renderFile runs go/format).
	handlerPath := filepath.Join(dir, "internal/app/handlers/widgetgrpc/widgetgrpc.go")
	src := readFile(t, handlerPath)
	if _, err := parser.ParseFile(token.NewFileSet(), handlerPath, src, parser.AllErrors); err != nil {
		t.Errorf("handler does not parse: %v", err)
	}
	for _, want := range []string{
		"package widgetgrpc",
		`widgetv1 "github.com/me/svc/gen/widget/v1"`,
		`"github.com/me/svc/core/widget"`,
		"widgetv1.UnimplementedWidgetServiceServer",
		"func (h *Handler) CreateWidget(",
		"func (h *Handler) ListWidgets(",
		"widgetv1.CreateWidgetResponse_builder{Widget: toProto(w)}.Build()",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("handler missing %q:\n%s", want, src)
		}
	}

	// The proto carries the editions contract and the service.
	proto := readFile(t, filepath.Join(dir, "proto/widget/v1/widget.proto"))
	for _, want := range []string{
		"edition = \"2023\";",
		"package widget.v1;",
		`option go_package = "github.com/me/svc/gen/widget/v1;widgetv1";`,
		"service WidgetService {",
		"rpc CreateWidget(CreateWidgetRequest) returns (CreateWidgetResponse);",
		"rpc ListWidgets(ListWidgetsRequest) returns (ListWidgetsResponse);",
		"repeated Widget widgets = 1;",
	} {
		if !strings.Contains(proto, want) {
			t.Errorf("proto missing %q:\n%s", want, proto)
		}
	}
}

func TestAddGRPCMultiwordNaming(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "github.com/me/svc")
	if err := addGRPC(&bytes.Buffer{}, addRESTOpts{Dir: dir, Name: "order-line"}); err != nil {
		t.Fatalf("addGRPC: %v", err)
	}
	proto := readFile(t, filepath.Join(dir, "proto/orderline/v1/orderline.proto"))
	// snake field pascalizes back to the Go type OrderLine in the generated code.
	if !strings.Contains(proto, "OrderLine order_line = 1;") {
		t.Errorf("proto singular field not snake_case:\n%s", proto)
	}
	if !strings.Contains(proto, "service OrderLineService {") {
		t.Errorf("proto service name wrong:\n%s", proto)
	}
}

func TestAddGRPCRefusesExistingFiles(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "github.com/me/svc")
	if err := addGRPC(&bytes.Buffer{}, addRESTOpts{Dir: dir, Name: "widget"}); err != nil {
		t.Fatalf("first addGRPC: %v", err)
	}
	if err := addGRPC(&bytes.Buffer{}, addRESTOpts{Dir: dir, Name: "widget"}); err == nil {
		t.Fatal("expected addGRPC to refuse overwriting existing files")
	}
}
