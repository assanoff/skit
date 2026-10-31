package rabbitmq

import (
	"context"
	"errors"
	"fmt"
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

// typeKey routes a message by its event type, the default routing key.
func typeKey(m broker.Message) string { return m.Type }

// Publisher publishes broker.Messages to RabbitMQ as CloudEvents v1.0
// structured-mode JSON. It runs in confirm mode and waits for the broker's
// publisher confirmation per publish, giving at-least-once delivery: Publish
// returns nil only after the broker has durably accepted the message.
type Publisher struct {
	pub        *rmq.Publisher
	source     string
	routingKey func(m broker.Message) string
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

// PublisherOption customizes a Publisher.
type PublisherOption func(*Publisher)

// WithRoutingKey sets how the routing key of a message is derived; the default
// is its Type. The exchange is always the message's Topic. A key of "" fails
// the publish.
func WithRoutingKey(fn func(m broker.Message) string) PublisherOption {
	return func(p *Publisher) {
		if fn != nil {
			p.routingKey = fn
		}
	}
}

// NewPublisher builds a confirm-mode publisher.
func NewPublisher(conn *Conn, cfg PublisherConfig, opts ...PublisherOption) (*Publisher, error) {
	if cfg.Source == "" {
		return nil, errors.New("rabbitmq: new publisher: source is required")
	}
	p, err := rmq.NewPublisher(
		conn.Conn,
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
		tracer:     otel.Tracer(cfg.TracerProvider, scope),
	}
	for _, opt := range opts {
		opt(pub)
	}
	return pub, nil
}

// Publish sends m to the exchange m.Topic under its routing key as a
// persistent CloudEvents message, then blocks on the broker's publisher
// confirmation. The span "send <exchange>" covers the send and the
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

// send publishes m to the exchange m.Topic under routingKey and waits for the
// broker's confirmation.
func (p *Publisher) send(ctx context.Context, m broker.Message, routingKey string) error {
	body, err := broker.MarshalCloudEvent(m)
	if err != nil {
		return fmt.Errorf("rabbitmq publish: %w", err)
	}

	headers := rmq.Table{}
	for k, v := range m.Headers {
		headers[k] = v
	}

	confirms, err := p.pub.PublishWithDeferredConfirmWithContext(
		ctx, body,
		[]string{routingKey},
		rmq.WithPublishOptionsExchange(m.Topic),
		rmq.WithPublishOptionsContentType(broker.ContentTypeCloudEvents),
		rmq.WithPublishOptionsPersistentDelivery,
		rmq.WithPublishOptionsHeaders(headers),
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

// Close closes the underlying publisher.
func (p *Publisher) Close() error {
	p.pub.Close()
	return nil
}
