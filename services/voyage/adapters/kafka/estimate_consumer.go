package kafka

import (
	"context"
	"log"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"maritime-test-lab/gen/avro"
	"maritime-test-lab/internal/avroserde"
	"maritime-test-lab/schemas"
)

// EstimateConsumerGroup is the voyage service's consumer group for estimate.ready.
const EstimateConsumerGroup = "voyage-service"

// EstimateUpserter applies an estimate.ready event to a voyage. The consumer's
// own small port — implemented by the postgres repository.
type EstimateUpserter interface {
	UpsertEstimate(ctx context.Context, voyageID string, voyageVersion, amountMinor int64) (applied bool, createdAt time.Time, err error)
}

// EstimateConsumer applies estimate.ready events with a versioned idempotent
// upsert, committing offsets only after the upsert (autocommit off) so a crash
// re-delivers rather than dropping the estimate.
type EstimateConsumer struct {
	client *kgo.Client
	serde  *avroserde.Serde
	repo   EstimateUpserter
}

// NewEstimateConsumer builds the estimate.ready consumer.
func NewEstimateConsumer(client *kgo.Client, serde *avroserde.Serde, repo EstimateUpserter) *EstimateConsumer {
	return &EstimateConsumer{client: client, serde: serde, repo: repo}
}

// Run consumes until ctx is cancelled or the client is closed.
func (c *EstimateConsumer) Run(ctx context.Context) {
	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return
		}
		fetches.EachError(func(t string, p int32, err error) {
			log.Printf("voyage consumer: fetch %s[%d]: %v", t, p, err)
		})

		var committable []*kgo.Record
		fetches.EachRecord(func(r *kgo.Record) {
			if c.handle(ctx, r) {
				committable = append(committable, r)
			}
		})
		if len(committable) > 0 {
			if err := c.client.CommitRecords(ctx, committable...); err != nil {
				log.Printf("voyage consumer: commit: %v", err)
			}
		}
	}
}

// handle returns true when the offset is safe to commit (applied or a benign
// no-op, or an unprocessable record skipped); false only on a transient DB error
// so the record is redelivered.
func (c *EstimateConsumer) handle(ctx context.Context, r *kgo.Record) bool {
	var er avro.EstimateReady
	if err := c.serde.Decode(r.Value, &er); err != nil {
		c.toDLQ(ctx, r, err)
		estimateErrors.Inc()
		dlqDepth.Inc()
		return true // poison pill routed to the DLQ; commit so it costs one entry, not a stall
	}

	applied, createdAt, err := c.repo.UpsertEstimate(ctx, er.VoyageID, er.VoyageVersion, er.AmountMinor)
	if err != nil {
		estimateErrors.Inc()
		log.Printf("voyage consumer: apply estimate %s: %v", er.VoyageID, err)
		return false // transient — redeliver
	}
	if applied {
		estimateApplied.Inc()
		estimateLatency.Observe(time.Since(createdAt).Seconds())
	}
	return true
}

// toDLQ ships the undecodable record verbatim to the dead-letter topic so one
// poison pill costs one DLQ entry, not a stalled consumer (D-007).
func (c *EstimateConsumer) toDLQ(ctx context.Context, r *kgo.Record, cause error) {
	log.Printf("voyage consumer: poison pill at offset %d: %v -> %s", r.Offset, cause, schemas.TopicEstimateReadyDLQ)
	res := c.client.ProduceSync(ctx, &kgo.Record{Topic: schemas.TopicEstimateReadyDLQ, Value: r.Value})
	if err := res.FirstErr(); err != nil {
		log.Printf("voyage consumer: DLQ produce: %v", err)
	}
}
