package main

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

type OperatorHealth struct {
	URL          string
	OfflineSince time.Time
}

type OperatorHealthSnapshot struct {
	KnownCount    int
	Offline       []OperatorHealth
	LastCheckAt   time.Time
	LastCheckTook time.Duration
}

var (
	operatorHealthMu    sync.RWMutex
	offlineOperators    = make(map[string]OperatorHealth)
	knownOperatorCount  int
	lastHealthCheckAt   time.Time
	lastHealthCheckTook time.Duration
)

// operatorHealthClient intentionally does not follow redirects so that the
// health probe only measures whether the operator itself is reachable, rather
// than following it out to a third-party OAuth provider.
var operatorHealthClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func startOperatorHealthChecker() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()

		checkOperators()
		for range ticker.C {
			checkOperators()
		}
	}()
}

func getOperatorHealthSnapshot() OperatorHealthSnapshot {
	operatorHealthMu.RLock()
	defer operatorHealthMu.RUnlock()

	offline := make([]OperatorHealth, 0, len(offlineOperators))
	for _, v := range offlineOperators {
		offline = append(offline, v)
	}
	slices.SortFunc(offline, func(a, b OperatorHealth) int {
		return strings.Compare(a.URL, b.URL)
	})

	return OperatorHealthSnapshot{
		KnownCount:    knownOperatorCount,
		Offline:       offline,
		LastCheckAt:   lastHealthCheckAt,
		LastCheckTook: lastHealthCheckTook,
	}
}

func checkOperators() {
	startedAt := time.Now()
	urls := make(map[string]struct{})

	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(ACCOUNT_BUCKET)
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var account AccountRecord
			if err := json.Unmarshal(v, &account); err != nil {
				log.Warn().Err(err).Str("email", string(k)).Msg("unmarshal account for health check")
				continue
			}
			for _, op := range account.Operators {
				urls[op.URL] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		log.Error().Err(err).Msg("iterate accounts for health check")
		return
	}

	var wg sync.WaitGroup
	for url := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			checkOperator(u)
		}(url)
	}
	wg.Wait()

	operatorHealthMu.Lock()
	knownOperatorCount = len(urls)
	for url := range offlineOperators {
		if _, exists := urls[url]; !exists {
			delete(offlineOperators, url)
		}
	}
	lastHealthCheckAt = time.Now()
	lastHealthCheckTook = time.Since(startedAt)
	operatorHealthMu.Unlock()
}

func checkOperator(url string) {
	target := strings.TrimRight(url, "/") + "/po/recover/google"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		markOffline(url, "request creation failed")
		return
	}

	resp, err := operatorHealthClient.Do(req)
	if err != nil {
		markOffline(url, "http request failed")
		return
	}
	resp.Body.Close()

	operatorHealthMu.Lock()
	delete(offlineOperators, url)
	operatorHealthMu.Unlock()
}

func markOffline(url string, reason string) {
	log.Warn().Str("operator", url).Str("reason", reason).Msg("operator marked offline")

	operatorHealthMu.Lock()
	defer operatorHealthMu.Unlock()

	if _, exists := offlineOperators[url]; !exists {
		offlineOperators[url] = OperatorHealth{
			URL:          url,
			OfflineSince: time.Now(),
		}
	}
}
