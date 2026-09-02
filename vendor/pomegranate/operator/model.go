package main

import (
	"encoding/json"
	"fmt"
	"hash/maphash"

	"github.com/elastic/go-freelru"
	bolt "go.etcd.io/bbolt"
)

var ACCOUNTS_BUCKET = []byte("accounts")

var ErrAccountNotFound = fmt.Errorf("account not found")

var registrationHashSeed = maphash.MakeSeed()

var registrationCache = func() *freelru.SyncedLRU[string, Registration] {
	cache, err := freelru.NewSynced[string, Registration](256, func(key string) uint32 {
		return uint32(maphash.String(registrationHashSeed, key))
	})
	if err != nil {
		panic(err)
	}
	return cache
}()

type Registration struct {
	Email         string `json:"email"`
	PubKey        string `json:"pubkey"`
	Central       string `json:"central"`
	CentralPubKey string `json:"central_pubkey"`
	Shard         string `json:"shard"`
}

func setupDB(tx *bolt.Tx) error {
	_, err := tx.CreateBucketIfNotExists(ACCOUNTS_BUCKET)
	return err
}

func saveRegistration(reg Registration) error {
	if err := db.Update(func(tx *bolt.Tx) error {
		return saveRegistrationTx(tx, reg)
	}); err != nil {
		return err
	}

	registrationCache.Add(reg.Email, reg)
	return nil
}

func loadRegistration(email string) (Registration, error) {
	if reg, ok := registrationCache.Get(email); ok {
		return reg, nil
	}

	var reg Registration
	if err := db.View(func(tx *bolt.Tx) error {
		loaded, err := loadRegistrationTx(tx, email)
		if err != nil {
			return err
		}
		reg = loaded
		return nil
	}); err != nil {
		return Registration{}, err
	}

	registrationCache.Add(email, reg)
	return reg, nil
}

func deleteRegistration(email string) error {
	if err := db.Update(func(tx *bolt.Tx) error {
		return deleteRegistrationTx(tx, email)
	}); err != nil {
		return err
	}

	registrationCache.Remove(email)
	return nil
}

func saveRegistrationTx(tx *bolt.Tx, reg Registration) error {
	bucket := tx.Bucket(ACCOUNTS_BUCKET)
	data, _ := json.Marshal(reg)
	return bucket.Put([]byte(reg.Email), data)
}

func loadRegistrationTx(tx *bolt.Tx, email string) (Registration, error) {
	bucket := tx.Bucket(ACCOUNTS_BUCKET)

	data := bucket.Get([]byte(email))
	if data == nil {
		return Registration{}, ErrAccountNotFound
	}

	var reg Registration
	if err := json.Unmarshal(data, &reg); err != nil {
		return Registration{}, err
	}

	return reg, nil
}

func deleteRegistrationTx(tx *bolt.Tx, email string) error {
	bucket := tx.Bucket(ACCOUNTS_BUCKET)
	if bucket.Get([]byte(email)) == nil {
		return ErrAccountNotFound
	}
	return bucket.Delete([]byte(email))
}
