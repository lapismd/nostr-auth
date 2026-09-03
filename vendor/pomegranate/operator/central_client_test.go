package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseTrustedCentralURLs(t *testing.T) {
	trusted, err := parseTrustedCentralURLs(
		" https://AUTH.example/ , http://central:5033 ",
	)
	if err != nil {
		t.Fatalf("parse trusted central URLs: %v", err)
	}
	for _, expected := range []string{"https://auth.example", "http://central:5033"} {
		if _, ok := trusted[expected]; !ok {
			t.Errorf("missing normalized trusted URL %q", expected)
		}
	}

	for _, invalid := range []string{
		"",
		"central.example",
		"ftp://central.example",
		"https://user@central.example",
		"https://central.example/path",
		"https://central.example?next=https://evil.example",
	} {
		if _, err := parseTrustedCentralURLs(invalid); err == nil {
			t.Errorf("expected %q to be rejected", invalid)
		}
	}
}

func TestRequireTrustedCentralURLUsesExactNormalizedMatch(t *testing.T) {
	previous := trustedCentralURLs
	trustedCentralURLs = map[string]struct{}{"https://auth.example": {}}
	t.Cleanup(func() { trustedCentralURLs = previous })

	if got, err := requireTrustedCentralURL("https://AUTH.example/"); err != nil || got != "https://auth.example" {
		t.Fatalf("normalized trusted URL = %q, %v", got, err)
	}
	if _, err := requireTrustedCentralURL("https://evil.example"); err == nil {
		t.Fatal("expected untrusted central to be rejected")
	}
}

func TestCentralHTTPClientDoesNotFollowRedirects(t *testing.T) {
	targetReached := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetReached = true
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, redirect.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := centralHTTPClient.Do(request)
	if err != nil {
		t.Fatalf("request redirect response: %v", err)
	}
	response.Body.Close()

	if response.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusFound)
	}
	if targetReached {
		t.Fatal("central HTTP client followed a redirect")
	}
}

func TestReadCentralResponseBodyRejectsOversizedBody(t *testing.T) {
	_, err := readCentralResponseBody(
		io.NopCloser(strings.NewReader(strings.Repeat("x", maxCentralResponseBytes+1))),
	)
	if err == nil {
		t.Fatal("expected oversized response to be rejected")
	}
}
