package bus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/hellodk/hetu/pkg/config"
	"github.com/hellodk/hetu/pkg/logger"
)

// Bus wraps a NATS JetStream connection and provides typed Publish/Subscribe
// helpers for the cluster-intel event bus.
type Bus struct {
	nc     *nats.Conn
	js     jetstream.JetStream
	prefix string
}

// Connect establishes a NATS connection and creates/attaches the main
// JetStream stream. The returned Bus is safe for concurrent use.
func Connect(ctx context.Context, cfg config.NATSConfig) (*Bus, error) {
	if !cfg.Enabled {
		return nil, errors.New("bus: nats is not enabled")
	}

	opts := []nats.Option{
		nats.Name("hetu"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
	}
	if cfg.Token != "" {
		opts = append(opts, nats.Token(cfg.Token))
	}
	if cfg.User != "" {
		opts = append(opts, nats.UserInfo(cfg.User, cfg.Password))
	}
	if cfg.CredsFile != "" {
		opts = append(opts, nats.UserCredentials(cfg.CredsFile))
	}
	if cfg.NkeyFile != "" {
		opt, err := nats.NkeyOptionFromSeed(cfg.NkeyFile)
		if err != nil {
			return nil, fmt.Errorf("bus: nkey: %w", err)
		}
		opts = append(opts, opt)
	}

	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("bus: connect %q: %w", cfg.URL, err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("bus: jetstream: %w", err)
	}

	prefix := cfg.StreamPrefix
	if prefix == "" {
		prefix = "ci"
	}

	// Ensure the main stream exists. CreateOrUpdate is idempotent.
	streamName := prefix + "_signals"
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{prefix + ".>"},
		Storage:  jetstream.FileStorage,
		MaxAge:   7 * 24 * time.Hour,
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("bus: create stream %q: %w", streamName, err)
	}

	return &Bus{nc: nc, js: js, prefix: prefix}, nil
}

// Publish sends data to the given subject. The subject is automatically
// prefixed with the configured stream prefix (e.g. "ci.signals.k8s.events").
func (b *Bus) Publish(ctx context.Context, subject string, data []byte) error {
	fqn := b.prefix + "." + subject

	msg := &nats.Msg{Subject: fqn, Data: data}
	if id := logger.RequestIDFromContext(ctx); id != "" {
		msg.Header = nats.Header{}
		msg.Header.Set("X-Request-ID", id)
	}

	_, err := b.js.PublishMsg(ctx, msg)
	if err != nil {
		return fmt.Errorf("bus: publish %q: %w", fqn, err)
	}
	return nil
}

// Subscribe creates a durable pull consumer and returns a channel of messages.
// The caller must call msg.Ack() on each message after processing.
// The returned cancel function stops the consumer.
func (b *Bus) Subscribe(ctx context.Context, consumer string, subjects []string) (<-chan jetstream.Msg, func(), error) {
	streamName := b.prefix + "_signals"

	fqn := make([]string, len(subjects))
	for i, s := range subjects {
		fqn[i] = b.prefix + "." + s
	}

	cons, err := b.js.CreateOrUpdateConsumer(ctx, streamName, jetstream.ConsumerConfig{
		Durable:        consumer,
		FilterSubjects: fqn,
		AckPolicy:      jetstream.AckExplicitPolicy,
		MaxDeliver:     5,
		AckWait:        30 * time.Second,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("bus: create consumer %q: %w", consumer, err)
	}

	msgCh := make(chan jetstream.Msg, 256)
	cctx, err := cons.Consume(func(msg jetstream.Msg) {
		select {
		case msgCh <- msg:
		case <-ctx.Done():
		}
	})
	if err != nil {
		return nil, nil, fmt.Errorf("bus: consume %q: %w", consumer, err)
	}

	cancel := func() {
		cctx.Stop()
		close(msgCh)
	}
	return msgCh, cancel, nil
}

// Close drains the connection and releases resources.
func (b *Bus) Close() {
	if b.nc != nil {
		b.nc.Drain() //nolint:errcheck
	}
}
