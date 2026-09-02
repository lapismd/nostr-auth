package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip04"
	"fiatjaf.com/nostr/nip44"
	"fiatjaf.com/pomegranate/common"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/elastic/go-freelru"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/sync/errgroup"
)

type pair struct {
	a nostr.PubKey
	b nostr.PubKey
}

//go:inline
func makePair(x, y nostr.PubKey) pair {
	if bytes.Compare(x[:], y[:]) < 0 {
		return pair{x, y}
	}
	return pair{y, x}
}

var ecdhCache = func() *freelru.ShardedLRU[pair, [32]byte] {
	cache, err := freelru.NewSharded[pair, [32]byte](8192, func(key pair) uint32 {
		return uint32(key.a[9])<<24 | uint32(key.a[27])<<16 | uint32(key.b[9])<<8 | uint32(key.b[27])
	})
	if err != nil {
		panic(err)
	}
	return cache
}()

var rawSharedSecretCache = func() *freelru.ShardedLRU[pair, [32]byte] {
	cache, err := freelru.NewSharded[pair, [32]byte](8192, func(key pair) uint32 {
		return uint32(key.a[9])<<24 | uint32(key.a[27])<<16 | uint32(key.b[9])<<8 | uint32(key.b[27])
	})
	if err != nil {
		panic(err)
	}
	return cache
}()

func (a *AccountRecord) Encrypt(
	ctx context.Context,
	plaintext string,
	recipientPublicKey nostr.PubKey,
) (string, error) {
	conversationKey, err := a.getConversationKey(ctx, recipientPublicKey)
	if err != nil {
		return "", err
	}

	return nip44.Encrypt(plaintext, conversationKey)
}

func (a *AccountRecord) Decrypt(
	ctx context.Context,
	base64ciphertext string,
	senderPublicKey nostr.PubKey,
) (string, error) {
	conversationKey, err := a.getConversationKey(ctx, senderPublicKey)
	if err != nil {
		return "", err
	}

	return nip44.Decrypt(base64ciphertext, conversationKey)
}

func (a *AccountRecord) Nip04Encrypt(
	ctx context.Context,
	plaintext string,
	recipientPublicKey nostr.PubKey,
) (string, error) {
	rawSecret, err := a.getRawSharedSecret(ctx, recipientPublicKey)
	if err != nil {
		return "", err
	}

	return nip04.Encrypt(plaintext, rawSecret[:])
}

func (a *AccountRecord) Nip04Decrypt(
	ctx context.Context,
	payload string,
	senderPublicKey nostr.PubKey,
) (string, error) {
	rawSecret, err := a.getRawSharedSecret(ctx, senderPublicKey)
	if err != nil {
		return "", err
	}

	return nip04.Decrypt(payload, rawSecret[:])
}

func (a *AccountRecord) getRawSharedSecret(ctx context.Context, peerPubKey nostr.PubKey) ([32]byte, error) {
	key := makePair(a.PubKey, peerPubKey)
	if cached, ok := rawSharedSecretCache.Get(key); ok {
		return cached, nil
	}

	sharedPoint, err := a.computeECDH(ctx, peerPubKey)
	if err != nil {
		return [32]byte{}, err
	}

	sharedPoint.ToAffine()
	var sharedSecret [32]byte
	sharedPoint.X.PutBytesUnchecked(sharedSecret[:])

	rawSharedSecretCache.Add(key, sharedSecret)
	return sharedSecret, nil
}

func (a *AccountRecord) getConversationKey(ctx context.Context, peerPubKey nostr.PubKey) ([32]byte, error) {
	key := makePair(a.PubKey, peerPubKey)
	if cached, ok := ecdhCache.Get(key); ok {
		return cached, nil
	}

	sharedSecret, err := a.getRawSharedSecret(ctx, peerPubKey)
	if err != nil {
		return [32]byte{}, err
	}

	var conversationKey [32]byte
	copy(conversationKey[:], hkdf.Extract(sha256.New, sharedSecret[:], []byte("nip44-v2")))
	ecdhCache.Add(key, conversationKey)
	return conversationKey, nil
}

func (a *AccountRecord) computeECDH(ctx context.Context, peerPubKey nostr.PubKey) (*btcec.JacobianPoint, error) {
	log := log.With().Str("user", a.PubKey.Hex()).Str("email", a.Email).Str("peer", peerPubKey.Hex()).Logger()

	selectedLog := log.Info()
	selected, cfg, err := a.selectOperatorsAndConfig(selectedLog)
	if err != nil {
		return nil, err
	}
	selectedLog.Msg("operators selected for ecdh")

	reqEvt := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      common.KindECDHRequest,
		Content:   cfg.Hex(),
		Tags:      make(nostr.Tags, 0, 2+len(selected)),
	}
	reqEvt.Tags = append(reqEvt.Tags, nostr.Tag{"email", a.Email})
	reqEvt.Tags = append(reqEvt.Tags, nostr.Tag{"p", peerPubKey.Hex()})
	for _, operator := range selected {
		reqEvt.Tags = append(reqEvt.Tags, nostr.Tag{"operator", operator.URL})
	}
	if err := reqEvt.Sign(settings.secretKey); err != nil {
		return nil, fmt.Errorf("sign ecdh request event: %w", err)
	}

	shards := make([]*btcec.JacobianPoint, len(selected))
	g, gctx := errgroup.WithContext(ctx)
	for i, operator := range selected {
		g.Go(func() error {
			body, err := postEvent(gctx, operator.URL, reqEvt)
			if err != nil {
				setRelayStatus(operator.URL, false)
				return err
			}

			share, err := hex.DecodeString(body)
			if err != nil {
				setRelayStatus(operator.URL, false)
				return fmt.Errorf("decode ecdh share hex from %s: %w", operator.URL, err)
			}

			pub, err := btcec.ParsePubKey(share)
			if err != nil {
				setRelayStatus(operator.URL, false)
				return fmt.Errorf("decode ecdh share from %s: %w", operator.URL, err)
			}

			shards[i] = new(btcec.JacobianPoint)
			pub.AsJacobian(shards[i])
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	sharedPoint, err := cfg.AggregateECDHShards(shards)
	if err != nil {
		return nil, fmt.Errorf("aggregate ecdh shares: %w", err)
	}
	return sharedPoint, nil
}
