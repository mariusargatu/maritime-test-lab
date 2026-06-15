// Package graphqltransport sends queued operations to the gateway as GraphQL
// mutations over HTTP, using the committed onBOARD ops. Every send carries a
// context deadline — a timeout is how the client learns it is offline (D-018).
package graphqltransport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"maritime-test-lab/clients/onboard-sync/domain"
	graphqlops "maritime-test-lab/clients/onboard-sync/graphql"
)

// Transport implements domain.Transport against a GraphQL HTTP endpoint.
type Transport struct {
	url     string
	client  *http.Client
	timeout time.Duration
}

// New builds a transport posting to gatewayURL (the full /query URL).
func New(gatewayURL string, client *http.Client, timeout time.Duration) *Transport {
	return &Transport{url: gatewayURL, client: client, timeout: timeout}
}

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type graphqlResponse struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// Send posts the operation's mutation and returns nil only on a clean ack.
func (t *Transport) Send(ctx context.Context, op domain.Operation) error {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	body, err := json.Marshal(graphqlRequest{Query: query(op), Variables: variables(op)})
	if err != nil {
		return fmt.Errorf("transport marshal %s: %w", op.ClientRequestID, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("transport request %s: %w", op.ClientRequestID, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("transport send %s: %w", op.ClientRequestID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("transport read %s: %w", op.ClientRequestID, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("transport %s: status %d: %s", op.ClientRequestID, resp.StatusCode, payload)
	}

	var parsed graphqlResponse
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return fmt.Errorf("transport decode %s: %w", op.ClientRequestID, err)
	}
	if len(parsed.Errors) > 0 {
		return fmt.Errorf("transport %s: graphql error: %s", op.ClientRequestID, parsed.Errors[0].Message)
	}
	return nil
}

func query(op domain.Operation) string {
	if op.Kind == domain.KindUpdate {
		return graphqlops.UpdateVoyage
	}
	return graphqlops.CreateVoyage
}

func variables(op domain.Operation) map[string]any {
	input := map[string]any{
		"clientRequestId": op.ClientRequestID,
		"origin":          op.Origin,
		"dest":            op.Dest,
		"distanceNm":      op.DistanceNm,
		"feesMinor":       op.FeesMinor,
	}
	if op.Kind == domain.KindUpdate {
		input["version"] = op.Version
	}
	return map[string]any{"input": input}
}
