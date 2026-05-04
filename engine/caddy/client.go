package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/plainwork/boxx/engine/state"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Load posts a full Caddy config to /load. This is an atomic replace.
func Load(ctx context.Context, cfg map[string]any) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+AdminAddr+"/load", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy /load: %s: %s", resp.Status, string(b))
	}
	return nil
}

// Patch posts a JSON patch payload to a Caddy config path, e.g.:
//
//	Patch(ctx, "/config/apps/http/servers/srv0/routes/0/handle/0/upstreams", upstreams)
func Patch(ctx context.Context, path string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch,
		"http://"+AdminAddr+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy PATCH %s: %s: %s", path, resp.Status, string(b))
	}
	return nil
}

// Apply rebuilds the full Caddy config from boxx state and pushes it via /load.
func Apply(ctx context.Context, s *state.State) error {
	return Load(ctx, BuildConfig(s))
}

func waitAdminReady(ctx context.Context, max time.Duration) error {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+AdminAddr+"/config/", nil)
		resp, err := httpClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode/100 == 2 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return errors.New("caddy admin API did not become ready in time")
}
