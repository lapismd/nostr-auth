package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/pomegranate/common"
	bolt "go.etcd.io/bbolt"
)

func setupOperatorTestDB(t *testing.T) {
	t.Helper()

	testDB, err := bolt.Open(t.TempDir()+"/operator.db", 0600, nil)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	previousDB := db
	db = testDB
	t.Cleanup(func() {
		db = previousDB
		testDB.Close()
	})
	if err := db.Update(setupDB); err != nil {
		t.Fatalf("setup test db: %v", err)
	}
}

func TestHandleSignRejectsUnexpectedCentralWithoutPanicking(t *testing.T) {
	setupOperatorTestDB(t)

	email := "wrong-central@example.com"
	registeredCentral := nostr.Generate()
	if err := saveRegistration(Registration{
		Email:         email,
		CentralPubKey: registeredCentral.Public().Hex(),
	}); err != nil {
		t.Fatalf("save registration: %v", err)
	}

	evt := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      common.KindConfiguration,
		Tags:      nostr.Tags{{"email", email}},
		Content:   "not-used",
	}
	if err := evt.Sign(nostr.Generate()); err != nil {
		t.Fatalf("sign request: %v", err)
	}
	body, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/po/sign", bytes.NewReader(body))
	response := httptest.NewRecorder()
	handleSign(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: got %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
