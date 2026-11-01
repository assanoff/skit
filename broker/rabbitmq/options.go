package rabbitmq

import "github.com/assanoff/skit/broker"

// PublisherOption customizes a Publisher.
type PublisherOption interface {
	applyPublisher(p *Publisher)
}

// ConsumerOption customizes a Consumer.
type ConsumerOption interface {
	applyConsumer(c *Consumer)
}

// publisherFunc is a PublisherOption of one function.
type publisherFunc func(p *Publisher)

func (f publisherFunc) applyPublisher(p *Publisher) { f(p) }

// EnvelopeOption sets the envelope of a Publisher or a Consumer: it is a
// PublisherOption and a ConsumerOption at once.
type EnvelopeOption struct{ e broker.Envelope }

// WithEnvelope sets how messages are packed for the wire; the publisher and
// the consumers of one stream take the same envelope. The default is
// broker.CloudEvents{}; a nil envelope keeps it.
func WithEnvelope(e broker.Envelope) EnvelopeOption {
	return EnvelopeOption{e: e}
}

func (o EnvelopeOption) applyPublisher(p *Publisher) {
	if o.e != nil {
		p.env = o.e
	}
}

func (o EnvelopeOption) applyConsumer(c *Consumer) {
	if o.e != nil {
		c.env = o.e
	}
}
