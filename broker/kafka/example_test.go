package kafka_test

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/kafka"
	"github.com/assanoff/skit/metrics"
	"github.com/assanoff/skit/worker"
)

// The publisher and the consumer dial the brokers of one Config. The replicas
// of a consumer group split the partitions of the topic. The example needs
// Kafka, so go test builds it but does not run it.
func Example() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.Default()
	cfg := kafka.Config{Brokers: []string{"localhost:9092"}}

	pub, err := kafka.NewPublisher(cfg, kafka.PublisherConfig{Source: "shop"})
	if err != nil {
		log.ErrorContext(ctx, "publisher", "err", err)
		return
	}
	defer func() { _ = pub.Close() }()
	err = pub.Publish(ctx, broker.Message{
		Type:        "widget.created",
		ContentType: "application/json",
		Data:        []byte(`{"id":"w-1"}`),
		Topic:       "widgets",
	})
	if err != nil {
		log.ErrorContext(ctx, "publish", "err", err)
		return
	}

	cons, err := kafka.NewConsumer(log, cfg, kafka.ConsumerConfig{
		Topic:   "widgets",
		Group:   "billing",
		Metrics: metrics.New(),
	}, func(ctx context.Context, m broker.Message) broker.Action {
		log.InfoContext(ctx, "widget event", "type", m.Type, "data", string(m.Data))
		return broker.Ack
	})
	if err != nil {
		log.ErrorContext(ctx, "consumer", "err", err)
		return
	}

	g := worker.NewGroup(log, worker.GroupConfig{ShutdownTimeout: 10 * time.Second})
	g.Add(cons)
	if err := g.Run(ctx); err != nil {
		log.ErrorContext(ctx, "group stopped", "err", err)
	}
}

// WithPartitionKey keys a record by the id of its aggregate, read from Data:
// the records of one widget go to one partition, in the order they are
// written. A message with no id never has one, so its error is Permanent.
func ExampleWithPartitionKey() {
	ctx := context.Background()
	log := slog.Default()
	cfg := kafka.Config{Brokers: []string{"localhost:9092"}}

	pub, err := kafka.NewPublisher(cfg, kafka.PublisherConfig{Source: "shop"},
		kafka.WithPartitionKey(func(m broker.Message) (string, error) {
			var v struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(m.Data, &v); err != nil {
				return "", broker.Permanent(err)
			}
			return v.ID, nil
		}))
	if err != nil {
		log.ErrorContext(ctx, "publisher", "err", err)
		return
	}
	defer func() { _ = pub.Close() }()

	for _, typ := range []string{"widget.created", "widget.renamed"} {
		err := pub.Publish(ctx, broker.Message{
			Type:  typ,
			Data:  []byte(`{"id":"w-1"}`),
			Topic: "widgets",
		})
		if err != nil {
			log.ErrorContext(ctx, "publish", "err", err)
			return
		}
	}
}
