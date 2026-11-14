package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jonxsong/Relay/internal/dispatcher"
	"github.com/jonxsong/Relay/internal/store"
	"github.com/nats-io/nats.go/jetstream"
)

type taskMessage struct {
	AttemptID uuid.UUID `json:"attempt_id"`
}

func Consume(ctx context.Context, broker *dispatcher.NATS, handler *Worker) error {
	consumer, err := broker.JetStream().CreateOrUpdateConsumer(ctx, dispatcher.TaskStreamName, jetstream.ConsumerConfig{
		Name: "relay-workers", Durable: "relay-workers", Description: "Relay horizontally shared worker pool",
		AckPolicy: jetstream.AckExplicitPolicy, AckWait: 90 * time.Second, MaxDeliver: -1,
		FilterSubject: "relay.tasks.>", MaxAckPending: 256,
	})
	if err != nil {
		return err
	}
	consumeContext, err := consumer.Consume(func(message jetstream.Msg) {
		handleMessage(ctx, handler, message)
	}, jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
		slog.Error("JetStream consume error", "error", err)
	}))
	if err != nil {
		return err
	}
	<-ctx.Done()
	consumeContext.Drain()
	select {
	case <-consumeContext.Closed():
	case <-time.After(10 * time.Second):
		consumeContext.Stop()
	}
	return ctx.Err()
}

func handleMessage(ctx context.Context, handler *Worker, message jetstream.Msg) {
	var task taskMessage
	if err := json.Unmarshal(message.Data(), &task); err != nil || task.AttemptID == uuid.Nil {
		slog.Error("discard invalid task message", "error", err)
		_ = message.TermWithReason("invalid task envelope")
		return
	}
	if err := message.InProgress(); err != nil {
		slog.Warn("send task progress", "attempt_id", task.AttemptID, "error", err)
	}
	err := handler.Handle(ctx, task.AttemptID)
	if err == nil || errors.Is(err, store.ErrStaleClaim) || errors.Is(err, store.ErrRunCancelled) {
		ackContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if ackErr := message.DoubleAck(ackContext); ackErr != nil {
			slog.Error("ack task", "attempt_id", task.AttemptID, "error", ackErr)
		}
		return
	}
	slog.Error("process task", "attempt_id", task.AttemptID, "error", err)
	if nackErr := message.NakWithDelay(2 * time.Second); nackErr != nil {
		slog.Error("nack task", "attempt_id", task.AttemptID, "error", nackErr)
	}
}
