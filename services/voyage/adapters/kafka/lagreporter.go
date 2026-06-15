package kafka

import (
	"context"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

// ReportLag publishes the voyage consumer-group lag to the consumer_lag gauge on
// an interval (the dashboard surface; the perf gate computes lag independently,
// D-032). Runs until ctx is cancelled.
func ReportLag(ctx context.Context, client *kgo.Client, group string, interval time.Duration) {
	admin := kadm.NewClient(client)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lags, err := admin.Lag(ctx, group)
			if err != nil {
				continue
			}
			if described, ok := lags[group]; ok && described.DescribeErr == nil && described.FetchErr == nil {
				consumerLag.Set(float64(described.Lag.Total()))
			}
		}
	}
}
