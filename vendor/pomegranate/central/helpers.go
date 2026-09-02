package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"

	"fiatjaf.com/nostr"
)

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func shuffle[I any](slice []I) {
	for i := 1; i < len(slice); i++ {
		j := rand.IntN(i)
		slice[i], slice[j] = slice[j], slice[i]
	}
}

func postEvent(ctx context.Context, operatorURL string, evt nostr.Event) (string, error) {
	payload, err := json.Marshal(evt)
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(operatorURL, "/") + "/po/sign"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			setRelayStatus(operatorURL, false)
		}
		return "", fmt.Errorf("post %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	text := strings.TrimSpace(string(body))
	if resp.StatusCode != http.StatusOK {
		if text == "" {
			text = resp.Status
		}
		return "", fmt.Errorf("operator %s: %s", operatorURL, text)
	}

	return text, nil
}
