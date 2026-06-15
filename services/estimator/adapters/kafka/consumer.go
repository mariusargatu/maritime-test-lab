// Package kafka is the estimator's event adapter: it consumes voyage.created,
// prices each voyage with the pure domain calc, and publishes estimate.ready.
// Offsets are committed only AFTER the estimate.ready produce is acked
// (autocommit off), so a crash mid-process re-delivers rather than silently
// dropping an estimate.
package kafka

import (
	"context"
	"log"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"maritime-test-lab/gen/avro"
	"maritime-test-lab/internal/avroserde"
	"maritime-test-lab/internal/money"
	"maritime-test-lab/schemas"
	"maritime-test-lab/services/estimator/domain"
)

// ConsumerGroup is the estimator's consumer group id for voyage.created.
const ConsumerGroup = "estimator-service"

// Consumer prices voyage.created events and publishes estimate.ready.
type Consumer struct {
	client *kgo.Client
	serde  *avroserde.Serde
	rate   money.Money
	now    func() time.Time // injected clock for calculated_at
}

// NewConsumer builds the estimator consumer.
func NewConsumer(client *kgo.Client, serde *avroserde.Serde, rate money.Money, now func() time.Time) *Consumer {
	return &Consumer{client: client, serde: serde, rate: rate, now: now}
}

// Run consumes until ctx is cancelled or the client is closed.
func (c *Consumer) Run(ctx context.Context) {
	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return
		}
		fetches.EachError(func(t string, p int32, err error) {
			log.Printf("estimator consumer: fetch %s[%d]: %v", t, p, err)
		})

		var committable []*kgo.Record
		fetches.EachRecord(func(r *kgo.Record) {
			if c.handle(ctx, r) {
				committable = append(committable, r)
			}
		})
		if len(committable) > 0 {
			if err := c.client.CommitRecords(ctx, committable...); err != nil {
				log.Printf("estimator consumer: commit: %v", err)
			}
		}
	}
}

// handle processes one record. It returns true when the offset is safe to commit:
// either the estimate was produced and acked, or the record is unprocessable and
// skipping it is correct (decode/price failures — these become DLQ rows in the
// next step). It returns false only for transient produce failures, so the record
// is redelivered.
func (c *Consumer) handle(ctx context.Context, r *kgo.Record) bool {
	var vc avro.VoyageCreated
	if err := c.serde.Decode(r.Value, &vc); err != nil {
		c.toDLQ(ctx, r, err)
		return true // poison pill routed to the DLQ; commit so it costs one entry, not a stall
	}

	cost, err := domain.EstimateCost(vc.DistanceNm, money.FromUSD(vc.FeesMinor), c.rate)
	if err != nil {
		log.Printf("estimator consumer: price %s: %v (skipping)", vc.ClientRequestID, err)
		return true
	}

	payload, err := c.serde.Encode(BuildEstimateReady(vc, cost, c.now()))
	if err != nil {
		log.Printf("estimator consumer: encode estimate.ready %s: %v", vc.ClientRequestID, err)
		return false
	}

	res := c.client.ProduceSync(ctx, &kgo.Record{Topic: schemas.TopicEstimateReady, Value: payload})
	if err := res.FirstErr(); err != nil {
		log.Printf("estimator consumer: produce estimate.ready %s: %v", vc.ClientRequestID, err)
		return false // do not commit — redeliver and retry
	}
	return true
}

// toDLQ ships the undecodable record verbatim to the dead-letter topic so one
// poison pill costs one DLQ entry, not a stalled consumer (D-007).
func (c *Consumer) toDLQ(ctx context.Context, r *kgo.Record, cause error) {
	log.Printf("estimator consumer: poison pill at offset %d: %v -> %s", r.Offset, cause, schemas.TopicVoyageCreatedDLQ)
	res := c.client.ProduceSync(ctx, &kgo.Record{Topic: schemas.TopicVoyageCreatedDLQ, Value: r.Value})
	if err := res.FirstErr(); err != nil {
		log.Printf("estimator consumer: DLQ produce: %v", err)
	}
}
