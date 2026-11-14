package dispatcher

import (
	"context"
	"fmt"
	"time"

	"github.com/jonxsong/Relay/internal/store/db"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const TaskStreamName = "RELAY_TASKS"

type NATS struct {
	connection *nats.Conn
	jetstream  jetstream.JetStream
}

func Open(ctx context.Context, url, clientName string) (*NATS, error) {
	connection, err := nats.Connect(
		url,
		nats.Name(clientName),
		nats.Timeout(5*time.Second),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}
	stream, err := jetstream.New(connection)
	if err != nil {
		connection.Close()
		return nil, fmt.Errorf("create JetStream client: %w", err)
	}
	if _, err := stream.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        TaskStreamName,
		Description: "Relay durable node task dispatch",
		Subjects:    []string{"relay.tasks.>"},
		Retention:   jetstream.WorkQueuePolicy,
		Storage:     jetstream.FileStorage,
		MaxAge:      7 * 24 * time.Hour,
		MaxMsgSize:  1 << 20,
		Duplicates:  10 * time.Minute,
	}); err != nil {
		connection.Close()
		return nil, fmt.Errorf("ensure task stream: %w", err)
	}
	return &NATS{connection: connection, jetstream: stream}, nil
}

func (n *NATS) JetStream() jetstream.JetStream {
	return n.jetstream
}

func (n *NATS) Publish(ctx context.Context, event db.OutboxEvent) error {
	if _, err := n.jetstream.Publish(ctx, event.Topic, event.Payload, jetstream.WithMsgID(event.DeduplicationID)); err != nil {
		return fmt.Errorf("publish %s: %w", event.DeduplicationID, err)
	}
	return nil
}

func (n *NATS) Close() error {
	if n.connection == nil || n.connection.IsClosed() {
		return nil
	}
	if err := n.connection.Drain(); err != nil {
		n.connection.Close()
		return fmt.Errorf("drain NATS connection: %w", err)
	}
	return nil
}
