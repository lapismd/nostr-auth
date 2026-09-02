package main

import (
	"context"
	"fmt"
	"hash/maphash"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip46"
	"fiatjaf.com/promenade/frost"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/elastic/go-freelru"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

const (
	ACCOUNT = "account"
	PROFILE = "profile"
)

var forbiddenKinds = []nostr.Kind{1776, 1777}

var (
	relayStatusHashSeed = maphash.MakeSeed()
	nip46UserHashSeed   = maphash.MakeSeed()
)

type contextKey string

const nip46UserDataContextKey contextKey = "nip46-user-data"

type relayStatus struct {
	online       bool
	lastChecked  time.Time
	OfflineSince time.Time
}

type nip46UserData struct {
	profile ProfileRecord
	account AccountRecord
	sk      nostr.SecretKey
}

type requesterMetadata struct {
	ws     *khatru.WebSocket
	pubkey nostr.PubKey
}

var (
	relayStatusCache    = mustNewRelayStatusCache()
	nip46UserCache      = mustNewNIP46UserCache()
	requesterByResponse = xsync.NewMapOf[nostr.ID, requesterMetadata]()
)

func setRelayStatus(url string, online bool) {
	if online {
		relayStatusCache.Add(url, relayStatus{online: true, lastChecked: time.Now()})
		return
	}

	if existing, ok := relayStatusCache.Peek(url); ok && !existing.online {
		relayStatusCache.Add(url, relayStatus{
			online:       false,
			lastChecked:  time.Now(),
			OfflineSince: existing.OfflineSince,
		})
		return
	}

	relayStatusCache.Add(url, relayStatus{
		online:       false,
		lastChecked:  time.Now(),
		OfflineSince: time.Now(),
	})
}

type CachedOfflineInfo struct {
	URL          string
	OfflineSince time.Time
	CachedUntil  time.Time
}

func getCachedOfflineOperators() []CachedOfflineInfo {
	keys := relayStatusCache.Keys()
	result := make([]CachedOfflineInfo, 0, len(keys))
	now := time.Now()
	for _, url := range keys {
		status, ok := relayStatusCache.Peek(url)
		if !ok || status.online {
			continue
		}
		cachedUntil := status.lastChecked.Add(2 * time.Minute)
		if cachedUntil.Before(now) {
			continue
		}
		result = append(result, CachedOfflineInfo{
			URL:          url,
			OfflineSince: status.OfflineSince,
			CachedUntil:  cachedUntil,
		})
	}
	slices.SortFunc(result, func(a, b CachedOfflineInfo) int {
		return strings.Compare(a.URL, b.URL)
	})
	return result
}

func mustNewRelayStatusCache() *freelru.ShardedLRU[string, relayStatus] {
	cache, err := freelru.NewSharded[string, relayStatus](256, func(key string) uint32 {
		return uint32(maphash.String(relayStatusHashSeed, key))
	})
	if err != nil {
		panic(err)
	}
	return cache
}

func mustNewNIP46UserCache() *freelru.ShardedLRU[string, nip46UserData] {
	cache, err := freelru.NewSharded[string, nip46UserData](256, func(key string) uint32 {
		return uint32(maphash.String(nip46UserHashSeed, key))
	})
	if err != nil {
		panic(err)
	}
	return cache
}

func putNIP46UserDataInContext(ctx context.Context, userData nip46UserData) context.Context {
	ctx = context.WithValue(ctx, nip46UserDataContextKey, userData)
	ctx = context.WithValue(ctx, ACCOUNT, userData.account)
	ctx = context.WithValue(ctx, PROFILE, userData.profile)
	return ctx
}

//go:inline
func getNIP46UserDataFromContext(ctx context.Context) (nip46UserData, bool) {
	val := ctx.Value(nip46UserDataContextKey)
	if val == nil {
		return nip46UserData{}, false
	}
	userData, ok := val.(nip46UserData)
	return userData, ok
}

//go:inline
func loadNIP46UserData(handlerPubkey nostr.PubKey) (nip46UserData, error) {
	handler := handlerPubkey.Hex()
	if userData, ok := nip46UserCache.Get(handler); ok {
		if _, isBlocked := blockedEmails[userData.profile.Email]; isBlocked {
			nip46UserCache.Remove(handler)
			log.Warn().Str("email", userData.profile.Email).Msg("blocked email sent signing request, deleting account")
			deleteAccountForEmail(userData.profile.Email)
			return nip46UserData{}, fmt.Errorf("account blocked")
		}
		return userData, nil
	}

	profile, err := loadProfileRecord(handler)
	if err != nil {
		return nip46UserData{}, fmt.Errorf("load profile: %w", err)
	}

	if _, isBlocked := blockedEmails[profile.Email]; isBlocked {
		log.Warn().Str("email", profile.Email).Msg("blocked email sent signing request, deleting account")
		if err := deleteAccountForEmail(profile.Email); err != nil {
			log.Error().Err(err).Str("email", profile.Email).Msg("delete blocked account failed")
		}
		return nip46UserData{}, fmt.Errorf("account blocked")
	}

	sk, err := nostr.SecretKeyFromHex(profile.HandlerSecretKey)
	if err != nil {
		return nip46UserData{}, fmt.Errorf("invalid handler secret: %w", err)
	}

	account, err := loadAccountRecord(profile.Email)
	if err != nil {
		return nip46UserData{}, fmt.Errorf("load account: %w", err)
	}

	userData := nip46UserData{profile: profile, account: account, sk: sk}
	nip46UserCache.Add(handler, userData)
	return userData, nil
}

var nip46Signer = &nip46.DynamicSigner{
	GetHandlerSecretKey: func(ctx context.Context, handlerPubkey nostr.PubKey) (context.Context, nostr.SecretKey, error) {
		logger := log.With().Str("handler", handlerPubkey.Hex()).Logger()
		logger.Info().Msg("loading handler secret and account")

		userData, err := loadNIP46UserData(handlerPubkey)
		if err != nil {
			logger.Error().Err(err).Msg("load handler data failed")
			return ctx, [32]byte{}, err
		}
		profile := userData.profile
		logger.Info().Str("profile", profile.Name).Str("email", profile.Email).Msg("profile record loaded")
		account := userData.account
		logger.Info().
			Str("email", account.Email).
			Int("operators", len(account.Operators)).
			Int("threshold", account.Threshold).
			Msg("account record loaded")

		ctx = putNIP46UserDataInContext(ctx, userData)
		return ctx, userData.sk, nil
	},
	OnConnect: func(ctx context.Context, from nostr.PubKey, secret string) error {
		log.Info().Str("from", from.Hex()).Msg("nip46 connect received")
		return nil
	},
	GetUserKeyer: func(ctx context.Context, handlerPubkey nostr.PubKey) (context.Context, nostr.Keyer, error) {
		logger := log.With().Str("handler", handlerPubkey.Hex()).Logger()

		userData, ok := getNIP46UserDataFromContext(ctx)
		if !ok {
			var err error
			userData, err = loadNIP46UserData(handlerPubkey)
			if err != nil {
				logger.Error().Err(err).Msg("user signer load failed")
				return ctx, nil, err
			}
			ctx = putNIP46UserDataInContext(ctx, userData)
		}

		account := userData.account
		return ctx, &account, nil
	},
	AuthorizeEncryption: func(ctx context.Context, from nostr.PubKey) bool {
		logger := log.With().Str("from", from.Hex()).Logger()
		userData, ok := getNIP46UserDataFromContext(ctx)
		if !ok {
			logger.Warn().Msg("authorize encryption denied: missing profile context")
			return false
		}
		profile := userData.profile
		logger = logger.With().Str("profile", profile.Name).Str("email", profile.Email).Logger()

		if profile.Restrictions != nil {
			if profile.Restrictions.Kinds != nil && !slices.Contains(profile.Restrictions.Kinds, 1059) {
				// if there is a kinds limitation it must include gift-wraps
				logger.Warn().Any("allowed_kinds", profile.Restrictions.Kinds).Msg("authorize encryption denied: kind restriction")
				return false
			}

			if profile.Restrictions.Authors != nil && !slices.Contains(profile.Restrictions.Authors, from) {
				// if there is an authors limitation it must include this message counterpart
				logger.Warn().Any("allowed_authors", profile.Restrictions.Authors).Msg("authorize encryption denied: author restriction")
				return false
			}

			if profile.Restrictions.Tags != nil {
				if pTags, ok := profile.Restrictions.Tags["p"]; ok {
					// if there is a "p" tag limitation it must include this message counterpart
					found := false
					for _, p := range pTags {
						if pk, err := nostr.PubKeyFromHex(p); err == nil && pk == from {
							found = true
							break
						}
					}
					if !found {
						logger.Warn().Any("allowed_p", pTags).Msg("authorize encryption denied: p-tag restriction")
						return false
					}
				}
			}
		}

		logger.Info().Msg("authorize encryption allowed")
		return true
	},
	AuthorizeSigning: func(ctx context.Context, event nostr.Event, from nostr.PubKey) error {
		logger := log.With().
			Uint16("kind", event.Kind.Num()).
			Uint32("created_at", uint32(event.CreatedAt)).
			Str("from", from.Hex()).
			Logger()

		userData, ok := getNIP46UserDataFromContext(ctx)
		if !ok {
			logger.Error().Msg("authorize signing failed: missing profile context")
			return fmt.Errorf("invalid profile context")
		}
		profile := userData.profile
		logger = logger.With().Str("profile", profile.Name).Str("email", profile.Email).Logger()

		if slices.Contains(forbiddenKinds, event.Kind) {
			logger.Warn().Msg("authorize signing denied: forbidden kind")
			return fmt.Errorf("forbidden kind %d", event.Kind)
		}
		if event.Kind == nostr.KindClientAuthentication {
			if tag := event.Tags.Find("challenge"); tag != nil && strings.HasPrefix(tag[1], "frostbunker:") {
				logger.Warn().Str("challenge", tag[1]).Msg("authorize signing denied: unsafe auth event")
				return fmt.Errorf("unsafe AUTH event")
			}
		}

		// disallow events signed for the future and the past
		now := nostr.Now()
		if event.CreatedAt > now+80 {
			logger.Warn().Int64("created_at", int64(event.CreatedAt)).Int64("now", int64(now)).Msg("authorize signing denied: event in future")
			return fmt.Errorf("can't sign event in the future")
		}

		if profile.Restrictions == nil {
			logger.Info().Msg("authorize signing allowed: no profile restrictions")
			return nil
		}

		if profile.Restrictions.Since > 0 {
			sinceBound := profile.Restrictions.Since
			if sinceBound <= 157680000 {
				sinceBound = now - sinceBound
			}
			if event.CreatedAt < sinceBound {
				log.Info().
					Str("profile", profile.Name).
					Any("since", profile.Restrictions.Since).
					Int64("created_at", int64(event.CreatedAt)).
					Int64("resolved_since", int64(sinceBound)).
					Msg("disallowed timestamp: before since")
				return fmt.Errorf("disallowed timestamp")
			}
		}

		if profile.Restrictions.Until > 0 {
			untilBound := profile.Restrictions.Until
			if untilBound <= 157680000 {
				untilBound = now + untilBound
			}
			if event.CreatedAt > untilBound {
				log.Info().
					Str("profile", profile.Name).
					Any("until", profile.Restrictions.Until).
					Int64("created_at", int64(event.CreatedAt)).
					Int64("resolved_until", int64(untilBound)).
					Msg("disallowed timestamp: after until")
				return fmt.Errorf("disallowed timestamp")
			}
		}

		if len(profile.Restrictions.Kinds) > 0 && !slices.Contains(profile.Restrictions.Kinds, event.Kind) {
			log.Info().
				Str("profile", profile.Name).
				Any("allowed", profile.Restrictions.Kinds).
				Uint16("kind", event.Kind.Num()).
				Msg("disallowed kind")
			return fmt.Errorf("disallowed kind")
		}

		logger.Info().Msg("authorize signing allowed")
		return nil
	},
	OnEventSigned: func(event nostr.Event) {
		log.Info().Str("id", event.ID.Hex()).Str("pubkey", event.PubKey.Hex()).Msg("event signed")
	},
}

func init() {
	nip46Signer.Init()
}

func handleNIP46Request(ctx context.Context, event nostr.Event) {
	log.Info().
		Str("event_id", event.ID.Hex()).
		Str("pubkey", event.PubKey.Hex()).
		Uint16("kind", event.Kind.Num()).
		Msg("handling nip46 request")

	_, resp, eventResponse, err := nip46Signer.HandleRequest(ctx, event)
	if err != nil {
		log.Error().Err(err).Str("event_id", event.ID.Hex()).Str("pubkey", event.PubKey.Hex()).Msg("failed to handle request")
		return
	}

	// this should live only while BroadcastEvent is running
	requesterByResponse.Store(eventResponse.ID, requesterMetadata{
		ws:     khatru.GetConnection(ctx),
		pubkey: event.PubKey,
	})
	defer requesterByResponse.Delete(eventResponse.ID)

	log.Info().Str("event_id", event.ID.Hex()).Stringer("response", resp).Str("id", eventResponse.ID.Hex()[0:8]).Msg("broadcasting nip46 response")
	relay.BroadcastEvent(eventResponse)
}

func (a *AccountRecord) selectOperatorsAndConfig(l *zerolog.Event) ([]AccountOperator, *frost.Configuration, error) {
	log.Info().Str("email", a.Email).Int("operators", len(a.Operators)).Int("threshold", a.Threshold).Msg("selecting operators")
	shuffle(a.Operators)
	selected := make([]AccountOperator, 0, a.Threshold)
	participants := make([]int, 0, a.Threshold)
	statuses := make([]relayStatus, len(a.Operators))
	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	var g errgroup.Group
	for i, operator := range a.Operators {
		status, ok := relayStatusCache.Get(operator.URL)
		if ok && time.Since(status.lastChecked) <= 2*time.Minute {
			statuses[i] = status
			log.Info().Str("operator", operator.URL).Bool("online", status.online).Time("checked_at", status.lastChecked).Msg("using cached operator status")
			if !status.online {
				log.Warn().Str("operator", operator.URL).Msg("operator offline (cached)")
			}
			continue
		}

		g.Go(func() error {
			log.Info().Str("operator", operator.URL).Msg("checking operator health")
			url := strings.TrimRight(operator.URL, "/") + "/po/action/google"
			req, _ := http.NewRequest(http.MethodHead, url, nil)
			resp, err := client.Do(req)
			if err != nil {
				log.Warn().Err(err).Str("operator", operator.URL).Msg("operator offline")
				setRelayStatus(operator.URL, false)
				statuses[i] = relayStatus{online: false, lastChecked: time.Now()}
				return nil
			}
			resp.Body.Close()
			status := relayStatus{
				online:      resp.StatusCode >= 200 && resp.StatusCode < 400,
				lastChecked: time.Now(),
			}
			setRelayStatus(operator.URL, status.online)
			statuses[i] = status
			log.Info().Str("operator", operator.URL).Bool("online", status.online).Int("status", resp.StatusCode).Msg("operator healthcheck finished")
			if !status.online {
				log.Warn().Int("status", resp.StatusCode).Str("operator", operator.URL).Msg("operator healthcheck failed")
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	for i, operator := range a.Operators {
		status := statuses[i]
		if !status.online {
			log.Info().Str("operator", operator.URL).Msg("skipping offline operator")
			continue
		}

		selected = append(selected, operator)
		participants = append(participants, operator.shard.ID)
		log.Info().Str("operator", operator.URL).Int("participant", operator.shard.ID).Int("selected", len(selected)).Msg("operator selected")
		l = l.Str("sel:"+strconv.Itoa(i), operator.URL)
		if len(selected) == a.Threshold {
			break
		}
	}

	if len(selected) != a.Threshold {
		log.Error().Int("needed", a.Threshold).Int("selected", len(selected)).Msg("not enough operators selected")
		return nil, nil, fmt.Errorf("failed to select %d operators", a.Threshold)
	}

	ipk := make([]byte, 33)
	ipk[0] = 2
	copy(ipk[1:], a.PubKey[:])
	point, _ := btcec.ParseJacobian(ipk)
	log.Info().Int("participants", len(participants)).Msg("operator configuration ready")

	return selected, &frost.Configuration{
		Threshold:    a.Threshold,
		MaxSigners:   len(a.Operators),
		PublicKey:    &point,
		Participants: participants,
	}, nil
}
