package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"fiatjaf.com/nostr"
	"fiatjaf.com/promenade/frost"
	"go.etcd.io/bbolt"
)

var (
	errNotFound     = errors.New("not found")
	ACCOUNT_BUCKET  = []byte("account")
	PROFILES_BUCKET = []byte("profiles")
	EMAILS_BUCKET   = []byte("emails")
)

type AccountRecord struct {
	Operators []AccountOperator `json:"operators"`
	Threshold int               `json:"threshold"`
	PubKey    nostr.PubKey      `json:"pubkey"`

	// not stored, but filled afterwards
	Email string `json:"email,omitempty"`
}

type AccountOperator struct {
	URL      string `json:"url"`
	PubShard string `json:"pubshard"`

	shard frost.PublicKeyShard
}

type ProfileRecord struct {
	HandlerSecretKey string        `json:"handler-secret-key"`
	Name             string        `json:"name"`
	Restrictions     *nostr.Filter `json:"restrictions,omitempty"`
	Email            string        `json:"email"`
}

type ProfileResponse struct {
	HandlerPubKey string        `json:"handler_pubkey"`
	Name          string        `json:"name"`
	Filter        *nostr.Filter `json:"filter,omitempty"`
	Email         string        `json:"email"`
}

type CreateProfileRequest struct {
	Name   string        `json:"name"`
	Filter *nostr.Filter `json:"filter,omitempty"`
}

func (p ProfileRecord) Response(handlerPubKey string) ProfileResponse {
	return ProfileResponse{
		HandlerPubKey: handlerPubKey,
		Name:          p.Name,
		Filter:        p.Restrictions,
		Email:         p.Email,
	}
}

func emailProfileKey(email string, handlerPubKey string) []byte {
	return []byte(email + "|" + handlerPubKey)
}

func saveAccountRecordTx(tx *bbolt.Tx, email string, account AccountRecord) error {
	b := tx.Bucket(ACCOUNT_BUCKET)

	if len(account.Operators) == 0 {
		return fmt.Errorf("account has no operators")
	}
	if account.Threshold <= 0 {
		return fmt.Errorf("invalid threshold %d", account.Threshold)
	}
	if len(account.Operators) < account.Threshold {
		return fmt.Errorf("not enough operators: have %d, need %d", len(account.Operators), account.Threshold)
	}

	account.Email = ""

	data, _ := json.Marshal(account)
	return b.Put([]byte(email), data)
}

func loadAccountRecord(email string) (AccountRecord, error) {
	account := AccountRecord{
		Email: email,
	}

	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(ACCOUNT_BUCKET)
		data := b.Get([]byte(email))
		if data == nil {
			return errNotFound
		}

		return json.Unmarshal(data, &account)
	})
	if err != nil {
		return account, err
	}

	for i, operator := range account.Operators {
		if operator.PubShard == "" {
			return account, fmt.Errorf("operator %s missing pubshard", operator.URL)
		}
		if err := account.Operators[i].shard.DecodeHex(operator.PubShard); err != nil {
			return account, fmt.Errorf("decode pubshard for %s: %w", operator.URL, err)
		}
	}

	return account, err
}

func deleteAccountDataTx(tx *bbolt.Tx, email string) error {
	accounts := tx.Bucket(ACCOUNT_BUCKET)
	emails := tx.Bucket(EMAILS_BUCKET)
	profiles := tx.Bucket(PROFILES_BUCKET)

	// first collect all matching email-bucket keys, since mutating the bucket
	// while iterating with a cursor can cause entries to be skipped. bbolt keys
	// are only valid during the transaction, so we copy them.
	prefix := []byte(email + "|")
	var keys [][]byte
	cursor := emails.Cursor()
	for key, _ := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, _ = cursor.Next() {
		keys = append(keys, append([]byte(nil), key...))
	}

	for _, key := range keys {
		if err := emails.Delete(key); err != nil {
			return err
		}
		if err := profiles.Delete(key[len(prefix):]); err != nil {
			return err
		}
	}

	return accounts.Delete([]byte(email))
}

func saveProfileRecordTx(tx *bbolt.Tx, handlerPubKey string, profile ProfileRecord) error {
	profiles := tx.Bucket(PROFILES_BUCKET)
	emails := tx.Bucket(EMAILS_BUCKET)

	data, _ := json.Marshal(profile)
	if err := profiles.Put([]byte(handlerPubKey), data); err != nil {
		return err
	}

	return emails.Put(emailProfileKey(profile.Email, handlerPubKey), nil)
}

//go:inline
func loadProfileRecord(handlerPubKey string) (ProfileRecord, error) {
	var profile ProfileRecord
	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(PROFILES_BUCKET)
		data := b.Get([]byte(handlerPubKey))
		if data == nil {
			return errNotFound
		}

		return json.Unmarshal(data, &profile)
	})
	return profile, err
}

func deleteProfileRecordTx(tx *bbolt.Tx, handlerPubKey string, profile ProfileRecord) error {
	profiles := tx.Bucket(PROFILES_BUCKET)
	emails := tx.Bucket(EMAILS_BUCKET)

	if err := profiles.Delete([]byte(handlerPubKey)); err != nil {
		return err
	}

	return emails.Delete(emailProfileKey(profile.Email, handlerPubKey))
}

func listProfilesByEmail(email string) ([]ProfileResponse, error) {
	prefix := []byte(email + "|")
	profiles := make([]ProfileResponse, 0, 8)

	err := db.View(func(tx *bbolt.Tx) error {
		emails := tx.Bucket(EMAILS_BUCKET)
		profileBucket := tx.Bucket(PROFILES_BUCKET)

		cursor := emails.Cursor()
		for key, _ := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, _ = cursor.Next() {
			handlerPubKey := string(key[len(prefix):])
			data := profileBucket.Get([]byte(handlerPubKey))
			if data == nil {
				continue
			}

			var profile ProfileRecord
			if err := json.Unmarshal(data, &profile); err != nil {
				return err
			}

			profiles = append(profiles, profile.Response(handlerPubKey))
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return profiles, nil
}
