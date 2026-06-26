package rabbitmq

import (
	"fmt"
	"net/url"

	rmq "github.com/wagslane/go-rabbitmq"

	"github.com/assanoff/skit/logger"
)

// Config holds the AMQP connection settings.
type Config struct {
	User     string
	Password string
	Host     string
	Port     string
	VHost    string
	// URL, when set, overrides the component fields above (e.g. "amqp://...").
	URL string
}

func (c Config) amqpURL() string {
	if c.URL != "" {
		return c.URL
	}
	host := c.Host
	if c.Port != "" {
		host += ":" + c.Port
	}
	u := url.URL{
		Scheme: "amqp",
		User:   url.UserPassword(c.User, c.Password),
		Host:   host,
		Path:   c.VHost,
	}
	return u.String()
}

// Conn is a RabbitMQ connection manager. It transparently reconnects, so a
// single Conn is shared by all publishers and consumers in the process.
type Conn struct {
	*rmq.Conn
	log *logger.Logger
}

// Dial opens a managed connection to RabbitMQ.
func Dial(log *logger.Logger, cfg Config) (*Conn, error) {
	conn, err := rmq.NewConn(cfg.amqpURL(), rmq.WithConnectionOptionsLogging)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: dial: %w", err)
	}
	return &Conn{Conn: conn, log: log}, nil
}

// Close closes the underlying connection. Safe to register as a closer cleanup.
func (c *Conn) Close() error {
	return c.Conn.Close()
}
