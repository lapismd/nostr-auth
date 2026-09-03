package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"fiatjaf.com/nostr/nip11"
)

const maxCentralResponseBytes = 64 * 1024

var (
	trustedCentralURLs = make(map[string]struct{})
	centralHTTPClient  = &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
)

func normalizeCentralURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse central URL: %w", err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("central URL must use http or https with a host")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("central URL must not contain user information")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("central URL must not contain a path")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("central URL must not contain a query or fragment")
	}
	parsed.Path = ""
	parsed.RawPath = ""
	return parsed.String(), nil
}

func parseTrustedCentralURLs(raw string) (map[string]struct{}, error) {
	trusted := make(map[string]struct{})
	for _, candidate := range strings.Split(raw, ",") {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		normalized, err := normalizeCentralURL(candidate)
		if err != nil {
			return nil, err
		}
		trusted[normalized] = struct{}{}
	}
	if len(trusted) == 0 {
		return nil, fmt.Errorf("at least one trusted central URL is required")
	}
	return trusted, nil
}

func requireTrustedCentralURL(raw string) (string, error) {
	normalized, err := normalizeCentralURL(raw)
	if err != nil {
		return "", err
	}
	if _, ok := trustedCentralURLs[normalized]; !ok {
		return "", fmt.Errorf("central URL is not trusted")
	}
	return normalized, nil
}

func readCentralResponseBody(body io.Reader) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(body, maxCentralResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxCentralResponseBytes {
		return nil, fmt.Errorf("central response exceeds %d bytes", maxCentralResponseBytes)
	}
	return content, nil
}

func fetchCentralInfo(ctx context.Context, centralURL string) (nip11.RelayInformationDocument, error) {
	info := nip11.RelayInformationDocument{URL: centralURL}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, centralURL, nil)
	if err != nil {
		return info, fmt.Errorf("create central information request: %w", err)
	}
	req.Header.Set("Accept", "application/nostr+json")
	req.Header.Set("User-Agent", "pomegranate-operator")

	resp, err := centralHTTPClient.Do(req)
	if err != nil {
		return info, fmt.Errorf("fetch central information: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("central information returned status %d", resp.StatusCode)
	}

	body, err := readCentralResponseBody(resp.Body)
	if err != nil {
		return info, fmt.Errorf("read central information: %w", err)
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return info, fmt.Errorf("decode central information: %w", err)
	}
	return info, nil
}

func serviceCookiesSecure() bool {
	parsed, err := url.Parse(settings.ServiceURL)
	return err == nil && strings.EqualFold(parsed.Scheme, "https")
}
