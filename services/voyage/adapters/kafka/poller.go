package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Poller is the transactional-outbox relay: every interval it publishes
// unpublished outbox rows and stamps published_at only after the broker acks
// (flag-on-ack, D-005). A crash between publish and stamp re-publishes; consumers
// are idempotent by version, so redelivery is safe.
type Poller struct {
	pool     *pgxpool.Pool
	client   *kgo.Client
	interval time.Duration
}

// NewPoller builds the outbox poller.
func NewPoller(pool *pgxpool.Pool, client *kgo.Client, interval time.Duration) *Poller {
	return &Poller{pool: pool, client: client, interval: interval}
}

// Run polls until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.Flush(ctx); err != nil {
				log.Printf("outbox poller: %v", err)
			}
		}
	}
}

const selectUnpublished = `
SELECT id, topic, payload, trace_carrier FROM outbox
WHERE published_at IS NULL
ORDER BY id
LIMIT 100`

const markPublished = `UPDATE outbox SET published_at = now() WHERE id = $1`

type outboxRow struct {
	id      int64
	topic   string
	payload []byte
	carrier []byte // JSON W3C trace carrier captured at write time (may be NULL)
}

// Flush publishes one batch of unpublished rows. Exported so tests can drive a
// single deterministic pass instead of waiting on the ticker.
func (p *Poller) Flush(ctx context.Context) error {
	batch, err := p.unpublished(ctx)
	if err != nil {
		return err
	}
	// Publish one row at a time so each carries its own restored trace context
	// (kotel injects it into the record headers as the trace continues).
	for _, row := range batch {
		res := p.client.ProduceSync(rowContext(ctx, row.carrier), &kgo.Record{Topic: row.topic, Value: row.payload})
		if err := res.FirstErr(); err != nil {
			// Leave the row unpublished; the next tick retries.
			log.Printf("outbox poller: publish id=%d: %v", row.id, err)
			continue
		}
		if _, err := p.pool.Exec(ctx, markPublished, row.id); err != nil {
			return fmt.Errorf("mark published id=%d: %w", row.id, err)
		}
	}
	return nil
}

// rowContext restores the trace context captured when the outbox row was written.
func rowContext(ctx context.Context, carrierJSON []byte) context.Context {
	if len(carrierJSON) == 0 {
		return ctx
	}
	var carrier propagation.MapCarrier
	if err := json.Unmarshal(carrierJSON, &carrier); err != nil {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

func (p *Poller) unpublished(ctx context.Context) ([]outboxRow, error) {
	rows, err := p.pool.Query(ctx, selectUnpublished)
	if err != nil {
		return nil, fmt.Errorf("query outbox: %w", err)
	}
	defer rows.Close()

	var batch []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.id, &r.topic, &r.payload, &r.carrier); err != nil {
			return nil, fmt.Errorf("scan outbox: %w", err)
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox: %w", err)
	}
	return batch, nil
}
