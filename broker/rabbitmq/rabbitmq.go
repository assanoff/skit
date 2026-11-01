package rabbitmq

import (
	"fmt"
	"net/url"

	rmq "github.com/wagslane/go-rabbitmq"
)

// Config holds the AMQP connection settings.
type Config struct {
	User     string
	Password string
	Host     string
	Port     string
}

func (c Config) amqpURL() string {
	host := c.Host
	if c.Port != "" {
		host += ":" + c.Port
	}
	u := url.URL{
		Scheme: "amqp",
		User:   url.UserPassword(c.User, c.Password),
		Host:   host,
	}
	return u.String()
}

// Conn is a RabbitMQ connection manager. It transparently reconnects, so a
// single Conn is shared by all publishers and consumers in the process.
type Conn struct {
	conn *rmq.Conn
}

// Dial opens a managed connection to RabbitMQ.
func Dial(cfg Config) (*Conn, error) {
	conn, err := rmq.NewConn(cfg.amqpURL(), rmq.WithConnectionOptionsLogging)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: dial: %w", err)
	}
	return &Conn{conn: conn}, nil
}

// Close closes the underlying connection. Safe to register as a closer cleanup.
func (c *Conn) Close() error {
	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("rabbitmq: close connection: %w", err)
	}
	return nil
}
