package main

import (
	"reflect"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/pomegranate/common"
	"fiatjaf.com/promenade/frost"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

func TestNewSigningRequestEventDoesNotInheritConfigurationTags(t *testing.T) {
	sessionID := nostr.ID{1, 2, 3}
	selected := []AccountOperator{
		{URL: "https://operator-one.example"},
		{URL: "https://operator-two.example"},
	}

	evt := newSigningRequestEvent(
		common.KindGroupCommit,
		"payload",
		"user@example.com",
		sessionID,
		selected,
	)
	want := nostr.Tags{
		{"email", "user@example.com"},
		{"e", sessionID.Hex()},
		{"operator", "https://operator-one.example"},
		{"operator", "https://operator-two.example"},
	}
	if !reflect.DeepEqual(evt.Tags, want) {
		t.Fatalf("unexpected request tags:\n got: %#v\nwant: %#v", evt.Tags, want)
	}
}

func TestComputeValidatedGroupCommitmentRejectsDuplicateSigner(t *testing.T) {
	secretKey := nostr.Generate()
	privateKey, _ := btcec.PrivKeyFromBytes(secretKey[:])
	shards, publicKey, _ := frost.TrustedKeyDeal(&privateKey.Key, 2, 3)
	cfg := &frost.Configuration{
		PublicKey:    publicKey,
		Threshold:    2,
		MaxSigners:   3,
		Participants: []int{1, 2},
	}
	signer, err := cfg.Signer(shards[0], make(frost.LambdaRegistry))
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	commitment := signer.Commit("duplicate-commitment-test")

	if _, _, _, err := computeValidatedGroupCommitment(
		cfg,
		[]frost.Commitment{commitment, commitment},
		[]byte("message"),
	); err == nil {
		t.Fatal("expected duplicate signer commitment to be rejected")
	}
}

func TestAttachVerifiedEventSignatureRejectsWrongPublicKey(t *testing.T) {
	evt := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      9,
		Tags:      nostr.Tags{},
		Content:   "hello",
	}
	if err := evt.Sign(nostr.Generate()); err != nil {
		t.Fatalf("sign fixture: %v", err)
	}
	sig, err := schnorr.ParseSignature(evt.Sig[:])
	if err != nil {
		t.Fatalf("parse fixture signature: %v", err)
	}

	evt.Sig = [64]byte{}
	if err := attachVerifiedEventSignature(&evt, sig); err != nil {
		t.Fatalf("attach valid signature: %v", err)
	}

	evt.PubKey = nostr.Generate().Public()
	if err := attachVerifiedEventSignature(&evt, sig); err == nil {
		t.Fatal("expected signature under the wrong public key to be rejected")
	}
}
