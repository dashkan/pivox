package identitysync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
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

	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ConsumerGroup(consumerGroup),
		kgo.ConsumeTopics(topic),
		// We commit explicitly (only successfully-handled records) and
		// rewind the consume position on a handler error — see Run.
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return nil, fmt.Errorf("identitysync: new kafka client: %w", err)
	}

	return &Consumer{
		client:  client,
		handler: cfg.Handler,
		logger:  cfg.Logger,
	}, nil
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
		if err := c.handler.Handle(ctx, rec.Value); err != nil {
			c.logger.ErrorContext(ctx, "identitysync: handle failed; rewinding partition to retry",
				"topic", rec.Topic, "partition", rec.Partition, "offset", rec.Offset, "error", err)
			return committable, rec.Offset, true
		}
		committable = append(committable, rec)
	}
	return committable, 0, false
}
