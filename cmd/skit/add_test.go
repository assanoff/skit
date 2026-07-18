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
		"core/widget/filter.go",
		"core/widget/order.go",
		"core/widget/widgetdb/widgetdb.go",
		"core/widget/widgetdb/model.go",
		"core/widget/widgetdb/order.go",
		"core/widget/widgetdb/filter.go",
		"api/widget/widget.go",
		"api/widget/model.go",
		"api/widget/order.go",
		"api/widget/filter.go",
		// Tests are generated alongside the module (no --no-tests): the mocked
		// API test lives with the code, the integration suites in tests/.
		"api/widget/widget_test.go",
		"tests/widget_store_test.go",
		"tests/widget_test.go",
		"tests/harness_test.go",
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
	// The core splits filter and ordering into their own files (symmetric with
	// the api and db layers); the store builds the keyset query.
	coreFilter := readFile(t, filepath.Join(dir, "core/widget/filter.go"))
	if !strings.Contains(coreFilter, "type QueryFilter struct") || !strings.Contains(coreFilter, "func (f QueryFilter) Validate()") {
		t.Errorf("core filter.go missing QueryFilter/Validate:\n%s", coreFilter)
	}
	coreOrder := readFile(t, filepath.Join(dir, "core/widget/order.go"))
	if !strings.Contains(coreOrder, "DefaultOrder") || !strings.Contains(coreOrder, "OrderByName") {
		t.Errorf("core order.go missing DefaultOrder/OrderByName:\n%s", coreOrder)
	}
	// The api layer owns the ?order_by allowlist (orderByFields) and parses the
	// listing params into a queryParams struct.
	apiOrder := readFile(t, filepath.Join(dir, "api/widget/order.go"))
	if !strings.Contains(apiOrder, "var orderByFields = map[string]string{") {
		t.Errorf("api order.go missing orderByFields allowlist:\n%s", apiOrder)
	}
	apiFilter := readFile(t, filepath.Join(dir, "api/widget/filter.go"))
	for _, want := range []string{"type queryParams struct", "func parseQueryParams(", "func parseFilter("} {
		if !strings.Contains(apiFilter, want) {
			t.Errorf("api filter.go missing %q:\n%s", want, apiFilter)
		}
	}
	db := readFile(t, filepath.Join(dir, "core/widget/widgetdb/widgetdb.go"))
	if !strings.Contains(db, "func (s *Store) QueryByCursor(") {
		t.Errorf("store missing QueryByCursor:\n%s", db)
	}
	// The store integration test lives in tests/ (not beside the code) and
	// exercises the listing set against a dbtest Postgres container.
	storeTest := readFile(t, filepath.Join(dir, "tests/widget_store_test.go"))
	if !strings.Contains(storeTest, "package tests") {
		t.Errorf("store test should be in package tests:\n%s", storeTest)
	}
	for _, want := range []string{
		"dbtest.NewPostgres",
		"func TestWidgetStoreCursor(",
		"func TestWidgetStoreFilter(",
		"func TestWidgetStoreOrdering(",
	} {
		if !strings.Contains(storeTest, want) {
			t.Errorf("store test missing %q:\n%s", want, storeTest)
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

func TestAddConsumerGeneratesBrokerAgnosticModule(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "github.com/me/svc")

	var out bytes.Buffer
	if err := addConsumer(&out, addRESTOpts{Dir: dir, Name: "order-refund"}); err != nil {
		t.Fatalf("addConsumer: %v", err)
	}

	want := []string{
		"internal/app/consumers/orderrefund/orderrefund.go",
		"internal/app/consumers/orderrefund/orderrefund_test.go",
		"internal/app/config/consumer.go",
	}
	fset := token.NewFileSet()
	for _, rel := range want {
		path := filepath.Join(dir, rel)
		src := readFile(t, path)
		if _, err := parser.ParseFile(fset, path, src, parser.AllErrors); err != nil {
			t.Errorf("%s does not parse: %v", rel, err)
		}
	}

	// The consumer handler is broker-agnostic: it depends on skit/broker and must
	// NOT name a concrete transport (rabbitmq/kafka/nats) — that is a wiring choice.
	handler := readFile(t, filepath.Join(dir, "internal/app/consumers/orderrefund/orderrefund.go"))
	if !strings.Contains(handler, `"github.com/assanoff/skit/broker"`) {
		t.Errorf("consumer should depend on skit/broker:\n%s", handler)
	}
	for _, transport := range []string{"rabbitmq", "kafka", "nats", "amqp"} {
		if strings.Contains(handler, transport) {
			t.Errorf("consumer must be transport-agnostic but mentions %q:\n%s", transport, handler)
		}
	}
	if !strings.Contains(handler, "func (c *Consumer) Handle(ctx context.Context, m broker.Message) broker.Action") {
		t.Errorf("consumer missing the broker.Handler method:\n%s", handler)
	}

	// The shared config type carries neutral field names (no exchange/routing-key).
	cfg := readFile(t, filepath.Join(dir, "internal/app/config/consumer.go"))
	for _, field := range []string{"Topic", "Group", "Filters", "Concurrency"} {
		if !strings.Contains(cfg, field) {
			t.Errorf("ConsumerOpts missing neutral field %q:\n%s", field, cfg)
		}
	}
}

func TestAddWorkerTickVariant(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "github.com/me/svc")

	if err := addWorker(&bytes.Buffer{}, addRESTOpts{Dir: dir, Name: "sweeper"}); err != nil {
		t.Fatalf("addWorker: %v", err)
	}

	src := readFile(t, filepath.Join(dir, "internal/app/workers/sweeper/sweeper.go"))
	if _, err := parser.ParseFile(token.NewFileSet(), "sweeper.go", src, parser.AllErrors); err != nil {
		t.Errorf("sweeper.go does not parse: %v", err)
	}
	for _, want := range []string{"func (w *Worker) Tick(", "func (w *Worker) Loop(", "worker.NewLoop("} {
		if !strings.Contains(src, want) {
			t.Errorf("tick worker missing %q:\n%s", want, src)
		}
	}
	if strings.Contains(src, "queue.") {
		t.Errorf("tick worker must not depend on queue:\n%s", src)
	}
}

func TestAddWorkerClaimVariant(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "github.com/me/svc")

	if err := addWorker(&bytes.Buffer{}, addRESTOpts{Dir: dir, Name: "photo-import", Claim: true}); err != nil {
		t.Fatalf("addWorker --claim: %v", err)
	}

	src := readFile(t, filepath.Join(dir, "internal/app/workers/photoimport/photoimport.go"))
	if _, err := parser.ParseFile(token.NewFileSet(), "photoimport.go", src, parser.AllErrors); err != nil {
		t.Errorf("photoimport.go does not parse: %v", err)
	}
	for _, want := range []string{"const Kind =", "func (w *Worker) Handle(ctx context.Context, t queue.Task) error", "PhotoImportTask"} {
		if !strings.Contains(src, want) {
			t.Errorf("claim worker missing %q:\n%s", want, src)
		}
	}
}

// TestAddRESTIdempotent verifies re-running addREST does not error and does not
// clobber existing files: a second run skips what is present. It also covers the
// --no-tests-then-fill-in flow: the first run without tests, the second run adds
// only the missing test files.
func TestAddRESTIdempotent(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "github.com/me/svc")

	// First run without tests: production code only.
	if err := addREST(&bytes.Buffer{}, addRESTOpts{Dir: dir, Name: "widget", NoTests: true}); err != nil {
		t.Fatalf("first addREST: %v", err)
	}
	storeTest := filepath.Join(dir, "tests/widget_store_test.go")
	if _, err := os.Stat(storeTest); !os.IsNotExist(err) {
		t.Fatalf("expected no test file after --no-tests, stat err = %v", err)
	}

	// Mark a production file so we can prove the re-run does not overwrite it.
	coreFile := filepath.Join(dir, "core/widget/widget.go")
	if err := os.WriteFile(coreFile, []byte("// edited by hand\npackage widget\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second run with tests: must not error, must fill in the missing tests, and
	// must leave the edited production file untouched.
	if err := addREST(&bytes.Buffer{}, addRESTOpts{Dir: dir, Name: "widget"}); err != nil {
		t.Fatalf("second addREST: %v", err)
	}
	if _, err := os.Stat(storeTest); err != nil {
		t.Errorf("expected the store test to be created on the second run: %v", err)
	}
	if got := readFile(t, coreFile); !strings.Contains(got, "edited by hand") {
		t.Errorf("re-run overwrote an existing file:\n%s", got)
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
