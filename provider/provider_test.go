package provider

import (
	"io"
	"testing"

	"github.com/assanoff/skit/broker/rabbitmq"
	"github.com/assanoff/skit/dbx"
	"github.com/assanoff/skit/logger"
	"github.com/assanoff/skit/otel"
)

// The providers wrap connect/dial/init paths that need live infrastructure
// (exercised by integration/e2e tests), so here we only assert each constructor
// returns a ready factory — guarding the public signatures.
func TestConstructorsReturnFactories(t *testing.T) {
	if Postgres(dbx.Config{Host: "localhost:5432", Name: "x"}) == nil {
		t.Fatal("Postgres returned a nil factory")
	}
	if RabbitMQ(logger.New(io.Discard, logger.Config{}), rabbitmq.Config{Host: "localhost", Port: "5672"}) == nil {
		t.Fatal("RabbitMQ returned a nil factory")
	}
	if Tracer(otel.Config{ServiceName: "svc", Endpoint: "localhost:4317"}) == nil {
		t.Fatal("Tracer returned a nil factory")
	}
}
