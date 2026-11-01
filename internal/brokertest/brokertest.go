// Package brokertest starts the brokers of the skit tests in containers —
// RabbitMQ and a single-node Kafka — pauses a container to stand for a broker
// that stops answering, and packs messages in an envelope other than
// CloudEvents.
package brokertest

import (
	"context"
	"net"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	tcrabbitmq "github.com/testcontainers/testcontainers-go/modules/rabbitmq"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/assanoff/skit/broker/rabbitmq"
)

// RabbitMQ starts RabbitMQ and returns its container and the config to dial
// it with. The container is removed when the test ends.
func RabbitMQ(ctx context.Context, tb testing.TB) (testcontainers.Container, rabbitmq.Config) {
	tb.Helper()
	c, err := tcrabbitmq.Run(ctx, "rabbitmq:3.13-management-alpine")
	testcontainers.CleanupContainer(tb, c)
	if err != nil {
		tb.Fatalf("brokertest: start rabbitmq: %v", err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		tb.Fatalf("brokertest: rabbitmq host: %v", err)
	}
	port, err := c.MappedPort(ctx, tcrabbitmq.DefaultAMQPPort)
	if err != nil {
		tb.Fatalf("brokertest: rabbitmq port: %v", err)
	}
	return c, rabbitmq.Config{
		User:     c.AdminUsername,
		Password: c.AdminPassword,
		Host:     host,
		Port:     port.Port(),
	}
}

// Kafka starts a single-node Kafka in KRaft mode and returns its bootstrap
// address. Clients connect to the address the broker advertises, so it
// listens on a fixed host port, picked free. The container is removed when the
// test ends.
func Kafka(ctx context.Context, tb testing.TB) string {
	tb.Helper()
	port := freePort(ctx, tb)
	addr := "localhost:" + port
	c, err := testcontainers.Run(ctx, "apache/kafka:3.9.1",
		testcontainers.WithEnv(map[string]string{
			"KAFKA_NODE_ID":                                  "1",
			"KAFKA_PROCESS_ROLES":                            "broker,controller",
			"KAFKA_LISTENERS":                                "PLAINTEXT://:9092,CONTROLLER://:9093",
			"KAFKA_ADVERTISED_LISTENERS":                     "PLAINTEXT://" + addr,
			"KAFKA_CONTROLLER_LISTENER_NAMES":                "CONTROLLER",
			"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP":           "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT",
			"KAFKA_CONTROLLER_QUORUM_VOTERS":                 "1@localhost:9093",
			"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":         "1",
			"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1",
			"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":            "1",
			"KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS":         "0",
		}),
		testcontainers.WithExposedPorts("9092/tcp"),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
			hc.PortBindings = network.PortMap{
				network.MustParsePort("9092/tcp"): {{HostPort: port}},
			}
		}),
		testcontainers.WithWaitStrategy(wait.ForLog("Kafka Server started")),
	)
	testcontainers.CleanupContainer(tb, c)
	if err != nil {
		tb.Fatalf("brokertest: start kafka: %v", err)
	}
	return addr
}

// Pause freezes the processes of c: the broker keeps its connections but
// answers nothing, as one that hangs. The returned func resumes it; it also
// runs when the test ends, so a failed test leaves no frozen container.
func Pause(ctx context.Context, tb testing.TB, c testcontainers.Container) (resume func()) {
	tb.Helper()
	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		tb.Fatalf("brokertest: docker client: %v", err)
	}
	id := c.GetContainerID()
	if _, err := cli.ContainerPause(ctx, id, client.ContainerPauseOptions{}); err != nil {
		tb.Fatalf("brokertest: pause: %v", err)
	}
	paused := true
	resume = func() {
		if !paused {
			return
		}
		paused = false
		_, err := cli.ContainerUnpause(context.WithoutCancel(ctx), id, client.ContainerUnpauseOptions{})
		if err != nil {
			tb.Errorf("brokertest: unpause: %v", err)
		}
	}
	tb.Cleanup(resume)
	return resume
}

// freePort returns a host port no one listens on.
func freePort(ctx context.Context, tb testing.TB) string {
	tb.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", "localhost:0")
	if err != nil {
		tb.Fatalf("brokertest: free port: %v", err)
	}
	defer func() { _ = l.Close() }()
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		tb.Fatalf("brokertest: free port: %v", err)
	}
	return port
}
