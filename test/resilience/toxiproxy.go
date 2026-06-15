//go:build resilience

package resilience

// A small Toxiproxy admin-API client the scenarios use to shape the network —
// plain HTTP, so there is no extra module dependency.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// toxiproxy talks to the Toxiproxy admin API (default :8474).
type toxiproxy struct {
	adminURL string
	proxy    string
}

func newToxiproxy(adminURL, proxy string) *toxiproxy {
	return &toxiproxy{adminURL: adminURL, proxy: proxy}
}

// ensureProxy (re)creates the proxy listening on listen and forwarding to
// upstream, removing any stale one first so reruns start clean.
func (t *toxiproxy) ensureProxy(listen, upstream string) error {
	_, _ = t.do(http.MethodDelete, "/proxies/"+t.proxy, nil) // ignore "not found"
	body := map[string]any{"name": t.proxy, "listen": listen, "upstream": upstream, "enabled": true}
	_, err := t.do(http.MethodPost, "/proxies", body)
	return err
}

// reset removes all toxics and resets every proxy to a clean enabled state.
func (t *toxiproxy) reset() error {
	_, err := t.do(http.MethodPost, "/reset", nil)
	return err
}

// addToxic installs a toxic (e.g. timeout black-hole, reset_peer RST).
func (t *toxiproxy) addToxic(name, toxicType string, attributes map[string]any) error {
	body := map[string]any{"name": name, "type": toxicType, "stream": "downstream", "toxicity": 1.0, "attributes": attributes}
	_, err := t.do(http.MethodPost, "/proxies/"+t.proxy+"/toxics", body)
	return err
}

func (t *toxiproxy) do(method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, t.adminURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("toxiproxy %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusConflict && resp.StatusCode != http.StatusNotFound {
		return nil, fmt.Errorf("toxiproxy %s %s: status %d: %s", method, path, resp.StatusCode, payload)
	}
	return payload, nil
}
