package identitysync

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kotel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	// consumerGroup is the Kafka consumer group for this consumer. A
	// stable group id lets Keycloak event processing resume from the
	// last committed offset across worker restarts.
	consumerGroup = "pivox-identity-sync"
	// topic is the keycloak-kafka SPI's event topic.
	topic = "keycloak-events"
	// retryBackoff is the pause after a poll in which a record failed, so
	// a persistent error (e.g. the DB being down) doesn't hot-loop the
	// rewind-and-retry. Honors ctx cancellation.
	retryBackoff = time.Second
)

// eventHandler applies one raw Keycloak event. *Handler satisfies it;
// the interface exists so the consume loop's offset logic can be tested
// with a stub that injects failures — no Kafka or DB required.
type eventHandler interface {
	Handle(ctx context.Context, raw []byte) error
}

// Consumer reads Keycloak events from Kafka and feeds each record to a
// Handler. Offsets are committed manually, only for successfully-handled
// records, so a failed handle never advances the committed group offset
// past it — at-least-once delivery, which the idempotent sinks tolerate.
type Consumer struct {
	client  *kgo.Client
	handler eventHandler
	logger  *slog.Logger
	// tracer opens a per-record "process" span around each Handle. Nil in the
	// offset-logic tests that build Consumer directly (handle() falls through).
	tracer *kotel.Tracer
	// brokerHost/brokerPort label each process span with the peer this consumer
	// reads from (server.address/server.port). kotel emits only messaging.*
	// attributes — no peer address — so without these the trace UI has nothing to
	// resolve the broker against, and the consume spans just hang off the worker
	// instead of showing Kafka as its own node. Derived from the seed broker.
	brokerHost string
	brokerPort int
}

// ConsumerConfig configures Consumer. All fields are required.
type ConsumerConfig struct {
	// Brokers is the Kafka seed broker list (host:port).
	Brokers []string
	// Handler applies each consumed record.
	Handler eventHandler
	// Logger receives fetch + commit warnings.
	Logger *slog.Logger
}

// NewConsumer builds a Consumer and its underlying kgo client. Panics
// on missing required config; returns an error only if the client
// cannot be constructed.
func NewConsumer(cfg ConsumerConfig) (*Consumer, error) {
	if len(cfg.Brokers) == 0 {
		panic("identitysync: ConsumerConfig.Brokers is required")
	}
	if cfg.Handler == nil {
		panic("identitysync: ConsumerConfig.Handler is required")
	}
	if cfg.Logger == nil {
		panic("identitysync: ConsumerConfig.Logger is required")
	}

	// OpenTelemetry via kotel: hooks add fetch/commit spans + consumer metrics
	// (consumer lag, fetch/record rates). It reads the global providers +
	// propagator that telemetry.Setup installs — no-op providers when export is
	// disabled, so this is free in that case. Per-record process spans are opened
	// separately in handle().
	tracer := kotel.NewTracer(
		kotel.TracerProvider(otel.GetTracerProvider()),
		kotel.TracerPropagator(otel.GetTextMapPropagator()),
	)
	kt := kotel.NewKotel(
		kotel.WithTracer(tracer),
		kotel.WithMeter(kotel.NewMeter(kotel.MeterProvider(otel.GetMeterProvider()))),
	)

	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ConsumerGroup(consumerGroup),
		kgo.ConsumeTopics(topic),
		// We commit explicitly (only successfully-handled records) and
		// rewind the consume position on a handler error — see Run.
		kgo.DisableAutoCommit(),
		kgo.WithHooks(kt.Hooks()...),
	)
	if err != nil {
		return nil, fmt.Errorf("identitysync: new kafka client: %w", err)
	}

	brokerHost, brokerPort := splitBroker(cfg.Brokers[0])

	return &Consumer{
		client:     client,
		handler:    cfg.Handler,
		logger:     cfg.Logger,
		tracer:     tracer,
		brokerHost: brokerHost,
		brokerPort: brokerPort,
	}, nil
}

// defaultBrokerPort mirrors franz-go's own default for a seed broker given
// without a port (kgo's parseBrokerAddr). splitBroker must report the address the
// client actually DIALS, not the string it was handed — see below.
const defaultBrokerPort = 9092

// splitBroker splits a seed broker into the host/port reported as the
// server.address/server.port span attributes.
//
// This must agree with what franz-go dials, because the trace UI resolves the
// Kafka peer by matching these attributes against the broker's real address. A
// disagreement doesn't error — it silently fails to resolve, and Kafka stops
// rendering as its own node, defeating the only reason these attributes exist.
// So: a portless seed reports :9092 (franz-go's default), not port 0, and IPv6
// literals are unwrapped from their brackets to the bare address form the
// attribute wants.
//
// Instrumentation must never break consumption, so anything unparsable degrades
// to (raw address, default port) rather than erroring. Such input can't reach
// here anyway — kgo.NewClient rejects it before the consumer is constructed.
func splitBroker(broker string) (host string, port int) {
	h, p, err := net.SplitHostPort(broker)
	if err != nil {
		// No port at all, or a bare IPv6 literal ("::1" — SplitHostPort reads the
		// colons as a malformed port). Either way franz-go supplies :9092.
		return strings.Trim(broker, "[]"), defaultBrokerPort
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return h, defaultBrokerPort
	}
	return h, n
}

// Run polls Kafka until ctx is cancelled, dispatching each record to the
// Handler. It closes the kgo client before returning.
//
// At-least-once offset strategy. PollFetches advances the client's
// *consume cursor* past every record it returns, so merely declining to
// commit a failed record is NOT enough — that record would never be
// re-fetched, and a later commit would push the committed group offset
// past it (silent loss). So on the first Handle error within a partition
// we both (a) leave that record and everything after it uncommitted, and
// (b) SetOffsets the partition's consume position back to the failed
// offset, so the next poll re-fetches and retries it. The
// successfully-handled prefix is committed; the idempotent sinks make the
// retried records safe to replay. A persistent error rewinds every poll
// (retryBackoff prevents a hot loop) — it surfaces as a stalled, loudly
// logged partition rather than as dropped identity events.
func (c *Consumer) Run(ctx context.Context) error {
	defer c.client.Close()

	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}

		// Fetch-level errors (broker unreachable, partition errors) are
		// transient — log and keep polling; franz-go retries internally.
		fetches.EachError(func(t string, p int32, err error) {
			c.logger.WarnContext(ctx, "identitysync: fetch error",
				"topic", t, "partition", p, "error", err)
		})

		var committable []*kgo.Record
		rewind := make(map[string]map[int32]kgo.EpochOffset)
		fetches.EachPartition(func(p kgo.FetchTopicPartition) {
			prefix, rewindTo, failed := c.processPartitionRecords(ctx, p.Records)
			committable = append(committable, prefix...)
			if failed {
				if rewind[p.Topic] == nil {
					rewind[p.Topic] = make(map[int32]kgo.EpochOffset)
				}
				// Epoch -1: rewind by offset without asserting a leader epoch.
				rewind[p.Topic][p.Partition] = kgo.EpochOffset{Epoch: -1, Offset: rewindTo}
			}
		})

		// Commit the successfully-handled prefix first, so progress persists
		// and the committed offset never sits ahead of an unprocessed record.
		if len(committable) > 0 {
			if err := c.client.CommitRecords(ctx, committable...); err != nil {
				c.logger.WarnContext(ctx, "identitysync: commit offsets failed; records will redeliver",
					"count", len(committable), "error", err)
			}
		}

		// Rewind any failed partitions so the failed record (and everything
		// after it) is re-fetched next poll, then back off so a persistent
		// failure doesn't hot-loop.
		if len(rewind) > 0 {
			c.client.SetOffsets(rewind)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(retryBackoff):
			}
		}
	}
}

// processPartitionRecords applies records in offset order until one fails.
// It returns the contiguous successfully-handled prefix (safe to commit)
// and, when a record's Handle returns an error, that record's offset
// (rewindTo) so the caller can rewind the partition and retry it. Records
// after the failed one are left unprocessed; they replay after the rewind.
func (c *Consumer) processPartitionRecords(ctx context.Context, recs []*kgo.Record) (committable []*kgo.Record, rewindTo int64, failed bool) {
	for _, rec := range recs {
		if err := c.handle(ctx, rec); err != nil {
			c.logger.ErrorContext(ctx, "identitysync: handle failed; rewinding partition to retry",
				"topic", rec.Topic, "partition", rec.Partition, "offset", rec.Offset, "error", err)
			return committable, rec.Offset, true
		}
		committable = append(committable, rec)
	}
	return committable, 0, false
}

// handle applies one record's handler under a kotel "process" span — linked to
// the producing side's trace when the record carries W3C traceparent headers,
// otherwise a root span. The handler runs under the span's context so its own
// DB spans nest beneath it. Handler errors are recorded on the span.
//
// tracer is nil in the offset-logic unit tests that construct Consumer directly;
// fall through to a bare Handle so those tests need no OTel setup.
func (c *Consumer) handle(ctx context.Context, rec *kgo.Record) error {
	if c.tracer == nil {
		return c.handler.Handle(ctx, rec.Value)
	}
	ctx, span := c.tracer.WithProcessSpan(rec)
	defer span.End()
	// Name the peer. kotel sets only messaging.* attributes, so the trace UI has
	// no address to resolve the broker by; stamping it makes Kafka render as its
	// own node rather than the spans hanging off this process.
	span.SetAttributes(
		attribute.String("server.address", c.brokerHost),
		attribute.Int("server.port", c.brokerPort),
	)
	err := c.handler.Handle(ctx, rec.Value)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}
