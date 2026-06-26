package provider

import (
	"context"
	"fmt"

	"github.com/assanoff/skit/broker/rabbitmq"
	"github.com/assanoff/skit/dim"
	"github.com/assanoff/skit/logger"
)

// RabbitMQ returns a dim factory that dials RabbitMQ with cfg. The cleanup
// closes the connection (safe to register as a closer cleanup). Build a
// Publisher/Consumer on the returned *rabbitmq.Conn separately.
//
//	c.BrokerConn, cleanup = dim.NewResource("BrokerConn",
//		provider.RabbitMQ(log, rabbitmq.Config{
//			User: opts.Broker.User, Password: opts.Broker.Password,
//			Host: opts.Broker.Host, Port: opts.Broker.Port,
//		}))
func RabbitMQ(log *logger.Logger, cfg rabbitmq.Config) func(ctx context.Context) (*rabbitmq.Conn, dim.CleanupFunc, error) {
	return func(ctx context.Context) (*rabbitmq.Conn, dim.CleanupFunc, error) {
		conn, err := rabbitmq.Dial(log, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("provider: dial rabbitmq: %w", err)
		}
		return conn, func() error { return conn.Close() }, nil
	}
}
