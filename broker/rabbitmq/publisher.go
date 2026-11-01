package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"
	"uuid"

	rmq "github.com/wagslane/go-rabbitmq"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/broker/internal/brokertrace"
	"github.com/assanoff/skit/otel"
)

// scope names the tracer of this package.
const scope = "github.com/assanoff/skit/broker/rabbitmq"

// contentTypeHeader is the header an Envelope puts the wire content type in;
// AMQP carries it in the content_type property.
const contentTypeHeader = "content-type"

// typeKey routes a message by its event type, the default routing key.
func typeKey(m broker.Message) string { return m.Type }

// Publisher publishes broker.Messages to RabbitMQ packed by its envelope,
// CloudEvents v1.0 structured-mode JSON by default. It runs in confirm mode
// and waits for the broker's publisher confirmation per publish, giving
// at-least-once delivery: Publish returns nil only after the broker has
// durably accepted the message.
type Publisher struct {
	pub        *rmq.Publisher
	source     string
	routingKey func(m broker.Message) string
	env        broker.Envelope
	tracer     trace.Tracer
}

// Compile-time check.
var _ broker.Publisher = (*Publisher)(nil)

// PublisherConfig configures a Publisher.
type PublisherConfig struct {
	// Source is the CloudEvents source stamped on messages that don't carry
	// their own (required).
	Source string
	// TracerProvider records the span of every publish; nil → the global
	// provider.
	TracerProvider trace.TracerProvider
}

// WithRoutingKey sets how the routing key of a message is derived; the default
// is its Type. The exchange is always the message's Topic. A key of "" fails
// the publish.
func WithRoutingKey(fn func(m broker.Message) string) PublisherOption {
	return publisherFunc(func(p *Publisher) {
		if fn != nil {
			p.routingKey = fn
		}
	})
}

// NewPublisher builds a confirm-mode publisher.
func NewPublisher(conn *Conn, cfg PublisherConfig, opts ...PublisherOption) (*Publisher, error) {
	if cfg.Source == "" {
		return nil, errors.New("rabbitmq: new publisher: source is required")
	}
	p, err := rmq.NewPublisher(
		conn.conn,
		rmq.WithPublisherOptionsLogging,
		rmq.WithPublisherOptionsConfirm,
	)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: new publisher: %w", err)
	}
	pub := &Publisher{
		pub:        p,
		source:     cfg.Source,
		routingKey: typeKey,
		env:        broker.CloudEvents{},
		tracer:     otel.Tracer(cfg.TracerProvider, scope),
	}
	for _, opt := range opts {
		opt.applyPublisher(pub)
	}
	return pub, nil
}

// Publish sends m to the exchange m.Topic under its routing key as a
// persistent message packed by the envelope, then blocks on the broker's
// publisher confirmation. The span "send <exchange>" covers the send and the
// confirmation; without a trace context in m.Headers it is the creation
// context of the message and goes out in its headers.
func (p *Publisher) Publish(ctx context.Context, m broker.Message) error {
	if m.Topic == "" {
		return errors.New("rabbitmq publish: topic is required")
	}

	// Fill CloudEvents defaults the producer left blank.
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	if m.Source == "" {
		m.Source = p.source
	}
	if m.Time.IsZero() {
		m.Time = time.Now().UTC()
	}
	routingKey := p.routingKey(m)
	if routingKey == "" {
		return fmt.Errorf("rabbitmq publish: no routing key for %s on %s", m.Type, m.Topic)
	}

	ctx, span, headers := brokertrace.StartSend(ctx, p.tracer, m.Topic, m.Headers,
		semconv.MessagingSystemRabbitMQ,
		semconv.MessagingMessageID(m.ID),
		semconv.MessagingRabbitMQDestinationRoutingKey(routingKey),
	)
	m.Headers = headers
	err := p.send(ctx, m, routingKey)
	brokertrace.End(span, err)
	return err
}

// send packs m with the envelope, publishes it to the exchange m.Topic under
// routingKey and waits for the broker's confirmation. The headers of the
// envelope go over the headers of m.
func (p *Publisher) send(ctx context.Context, m broker.Message, routingKey string) error {
	body, envHeaders, err := p.env.Marshal(m)
	if err != nil {
		return fmt.Errorf("rabbitmq publish: %w", err)
	}
	wire := make(map[string]string, len(m.Headers)+len(envHeaders))
	maps.Copy(wire, m.Headers)
	maps.Copy(wire, envHeaders)
	table, contentType := amqpHeaders(wire)

	confirms, err := p.pub.PublishWithDeferredConfirmWithContext(
		ctx, body,
		[]string{routingKey},
		rmq.WithPublishOptionsExchange(m.Topic),
		rmq.WithPublishOptionsContentType(contentType),
		rmq.WithPublishOptionsPersistentDelivery,
		rmq.WithPublishOptionsHeaders(table),
		rmq.WithPublishOptionsMessageID(m.ID),
		rmq.WithPublishOptionsTimestamp(m.Time),
		rmq.WithPublishOptionsType(m.Type),
		rmq.WithPublishOptionsAppID(m.Source),
	)
	if err != nil {
		return fmt.Errorf("rabbitmq publish: %w", err)
	}

	// Exactly one routing key → exactly one confirmation. A missing channel
	// means confirms are not actually enabled; treat as a hard error rather
	// than silently dropping the at-least-once guarantee.
	if len(confirms) == 0 || confirms[0] == nil {
		return errors.New("rabbitmq publish: broker returned no confirmation channel")
	}
	ok, err := confirms[0].WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("rabbitmq publish: wait confirm: %w", err)
	}
	if !ok {
		return errors.New("rabbitmq publish: broker nacked the message")
	}
	return nil
}

// amqpHeaders splits the wire headers of a message into the AMQP header table
// and the content_type property, which carries the header content-type.
func amqpHeaders(wire map[string]string) (rmq.Table, string) {
	table := make(rmq.Table, len(wire))
	for k, v := range wire {
		table[k] = v
	}
	delete(table, contentTypeHeader)
	return table, wire[contentTypeHeader]
}

// Close closes the underlying publisher.
func (p *Publisher) Close() error {
	p.pub.Close()
	return nil
}
