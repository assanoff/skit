package rabbitmq

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	rmq "github.com/wagslane/go-rabbitmq"

	"github.com/assanoff/skit/broker"
	"github.com/assanoff/skit/logger"
)

// TopicMapper maps a transport-neutral (topic, key) pair to the RabbitMQ
// (exchange, routingKey) it should publish to. Override the default via
// WithTopicMapper when your topic naming doesn't map 1:1 to exchanges.
type TopicMapper func(topic, key string) (exchange, routingKey string)

// defaultTopicMapper maps Topic -> exchange and Key -> routing key, falling
// back to the topic as the routing key when no key is set (so a single-key
// topic still routes). This makes the common case — one topic per exchange —
// work with no configuration.
func defaultTopicMapper(topic, key string) (exchange, routingKey string) {
	if key == "" {
		key = topic
	}
	return topic, key
}

// Publisher publishes broker.Messages to RabbitMQ as CloudEvents v1.0
// structured-mode JSON. It runs in confirm mode and waits for the broker's
// publisher confirmation per publish, giving at-least-once delivery: Publish
// returns nil only after the broker has durably accepted the message.
type Publisher struct {
	log    *logger.Logger
	pub    *rmq.Publisher
	source string
	mapper TopicMapper
}

// Compile-time check.
var _ broker.Publisher = (*Publisher)(nil)

// PublisherOption customizes a Publisher.
type PublisherOption func(*Publisher)

// WithTopicMapper sets how a Message's transport-neutral Topic/Key map to a
// RabbitMQ exchange and routing key.
func WithTopicMapper(m TopicMapper) PublisherOption {
	return func(p *Publisher) {
		if m != nil {
			p.mapper = m
		}
	}
}

// NewPublisher builds a confirm-mode publisher. source is the CloudEvents
// source stamped on messages that don't carry their own.
func NewPublisher(conn *Conn, source string, log *logger.Logger, opts ...PublisherOption) (*Publisher, error) {
	if source == "" {
		return nil, fmt.Errorf("rabbitmq: new publisher: source is required")
	}
	p, err := rmq.NewPublisher(
		conn.Conn,
		rmq.WithPublisherOptionsLogging,
		rmq.WithPublisherOptionsConfirm,
	)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: new publisher: %w", err)
	}
	pub := &Publisher{log: log, pub: p, source: source, mapper: defaultTopicMapper}
	for _, opt := range opts {
		opt(pub)
	}
	return pub, nil
}

// Publish maps m's Topic/Key to a RabbitMQ exchange/routing key, sends it as a
// persistent CloudEvents message, then blocks on the broker's publisher
// confirmation.
func (p *Publisher) Publish(ctx context.Context, m broker.Message) error {
	if m.Topic == "" {
		return fmt.Errorf("rabbitmq publish: topic is required")
	}
	exchange, routingKey := p.mapper(m.Topic, m.Key)
	if exchange == "" || routingKey == "" {
		return fmt.Errorf("rabbitmq publish: topic %q maps to empty exchange/routing key", m.Topic)
	}

	// Fill CloudEvents defaults the producer left blank.
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	if m.Source == "" {
		m.Source = p.source
	}
	if m.Time.IsZero() {
		m.Time = time.Now().UTC()
	}

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
		rmq.WithPublishOptionsExchange(exchange),
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
		return fmt.Errorf("rabbitmq publish: broker returned no confirmation channel")
	}
	ok, err := confirms[0].WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("rabbitmq publish: wait confirm: %w", err)
	}
	if !ok {
		return fmt.Errorf("rabbitmq publish: broker nacked the message")
	}
	return nil
}

// Close closes the underlying publisher.
func (p *Publisher) Close() error {
	p.pub.Close()
	return nil
}
