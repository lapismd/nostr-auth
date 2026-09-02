package main

import (
	"errors"
	"testing"

	"fiatjaf.com/nostr"
	"go.etcd.io/bbolt"
)

func setupTestDB(t *testing.T) {
	t.Helper()

	tempDB, err := bbolt.Open(t.TempDir()+"/test.db", 0600, nil)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	oldDB := db
	db = tempDB
	t.Cleanup(func() {
		db = oldDB
		tempDB.Close()
	})

	if err := db.Update(func(tx *bbolt.Tx) error {
		for _, bucket := range [][]byte{ACCOUNT_BUCKET, PROFILES_BUCKET, EMAILS_BUCKET} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("create buckets: %v", err)
	}
}

func TestDeleteAccountForEmailDeletesAllProfiles(t *testing.T) {
	setupTestDB(t)

	oldCache := nip46UserCache
	nip46UserCache = mustNewNIP46UserCache()
	t.Cleanup(func() {
		nip46UserCache = oldCache
	})

	email := "user@example.com"
	handler1 := nostr.Generate().Public().Hex()
	handler2 := nostr.Generate().Public().Hex()

	account := AccountRecord{
		Email:     email,
		Threshold: 2,
		Operators: []AccountOperator{{URL: "wss://one", PubShard: "01"}, {URL: "wss://two", PubShard: "02"}},
	}
	profile1 := ProfileRecord{HandlerSecretKey: nostr.Generate().Hex(), Name: "one", Email: email}
	profile2 := ProfileRecord{HandlerSecretKey: nostr.Generate().Hex(), Name: "two", Email: email}

	if err := db.Update(func(tx *bbolt.Tx) error {
		if err := saveAccountRecordTx(tx, email, account); err != nil {
			return err
		}
		if err := saveProfileRecordTx(tx, handler1, profile1); err != nil {
			return err
		}
		return saveProfileRecordTx(tx, handler2, profile2)
	}); err != nil {
		t.Fatalf("seed data: %v", err)
	}

	nip46UserCache.Add(handler1, nip46UserData{profile: profile1})
	nip46UserCache.Add(handler2, nip46UserData{profile: profile2})

	if err := deleteAccountForEmail(email); err != nil {
		t.Fatalf("delete account: %v", err)
	}

	if _, err := loadAccountRecord(email); !errors.Is(err, errNotFound) {
		t.Fatalf("expected account deleted, got %v", err)
	}
	if _, err := loadProfileRecord(handler1); !errors.Is(err, errNotFound) {
		t.Fatalf("expected first profile deleted, got %v", err)
	}
	if _, err := loadProfileRecord(handler2); !errors.Is(err, errNotFound) {
		t.Fatalf("expected second profile deleted, got %v", err)
	}

	profiles, err := listProfilesByEmail(email)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("expected no profiles left, got %d", len(profiles))
	}

	if _, ok := nip46UserCache.Get(handler1); ok {
		t.Fatal("expected first profile cache entry removed")
	}
	if _, ok := nip46UserCache.Get(handler2); ok {
		t.Fatal("expected second profile cache entry removed")
	}
}
