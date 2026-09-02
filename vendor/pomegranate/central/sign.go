package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"fiatjaf.com/nostr"
	"fiatjaf.com/pomegranate/common"
	"fiatjaf.com/promenade/frost"
	"github.com/mailru/easyjson"
	"golang.org/x/sync/errgroup"
)

var (
	lambdaRegistry     = make(frost.LambdaRegistry)
	lambdaRegistryLock sync.Mutex
)

func (a *AccountRecord) GetPublicKey(ctx context.Context) (nostr.PubKey, error) {
	return a.PubKey, nil
}

func (a *AccountRecord) SignEvent(ctx context.Context, event *nostr.Event) (err error) {
	pubkey, err := a.GetPublicKey(ctx)
	if err != nil {
		return err
	}

	log := log.With().Str("user", pubkey.Hex()).Str("email", a.Email).Logger()
	logSelected := log.Info()
	selected, cfg, err := a.selectOperatorsAndConfig(logSelected)
	if err != nil {
		return err
	}
	logSelected.Msg("operators selected")

	// prepare configuration payload
	confEvt := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      common.KindConfiguration,
		Content:   cfg.Hex(),
		Tags:      make(nostr.Tags, 0, 1+len(selected)),
	}
	confEvt.Tags = append(confEvt.Tags, nostr.Tag{"email", a.Email})
	for _, operator := range selected {
		confEvt.Tags = append(confEvt.Tags, nostr.Tag{"operator", operator.URL})
	}
	if err := confEvt.Sign(settings.secretKey); err != nil {
		return fmt.Errorf("sign configuration event: %w", err)
	}

	sessionID := confEvt.ID
	defer func() {
		if err != nil {
			log.Error().Err(err).Str("session", sessionID.Hex()).Msg("signing session failed")
		}
	}()

	event.PubKey = pubkey
	event.ID = sha256.Sum256(event.Serialize())

	log = log.With().Str("session", sessionID.Hex()).Str("event", event.ID.Hex()).Logger()
	log.Info().Uint16("kind", event.Kind.Num()).Int("selected", len(selected)).Msg("starting signing session")

	// send configuration, get commitments
	commitments := make([]frost.Commitment, len(selected))
	g, gctx := errgroup.WithContext(ctx)
	for i, operator := range selected {
		g.Go(func() error {
			log.Info().Str("operator", operator.URL).Msg("requesting operator commitment")
			body, err := postEvent(gctx, operator.URL, confEvt)
			if err != nil {
				return err
			}

			var commitment frost.Commitment
			if err := commitment.DecodeHex(body); err != nil {
				return fmt.Errorf("failed decode commitment from %s: %w", operator.URL, err)
			}

			commitments[i] = commitment
			log.Info().Str("operator", operator.URL).Int("signer_id", commitment.SignerID).Msg("operator commitment received")
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// build group commitment
	groupCommitment, bindingCoefficient, finalNonce := cfg.ComputeGroupCommitment(commitments, event.ID[:])
	log.Info().Msg("group commitment computed")

	// prepare group commitment payload
	groupCommitEvt := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      common.KindGroupCommit,
		Content:   groupCommitment.Hex(),
		Tags:      make(nostr.Tags, 0, 2+len(selected)),
	}
	groupCommitEvt.Tags = append(confEvt.Tags, nostr.Tag{"email", a.Email})
	groupCommitEvt.Tags = append(groupCommitEvt.Tags, nostr.Tag{"e", sessionID.Hex()})
	for _, operator := range selected {
		groupCommitEvt.Tags = append(groupCommitEvt.Tags, nostr.Tag{"operator", operator.URL})
	}
	if err := groupCommitEvt.Sign(settings.secretKey); err != nil {
		return fmt.Errorf("sign group commitment event: %w", err)
	}

	// prepare event to be signed payload
	jevt, _ := easyjson.Marshal(event)
	evtEvt := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      common.KindEventToBeSigned,
		Content:   string(jevt),
		Tags:      make(nostr.Tags, 0, 2+len(selected)),
	}
	evtEvt.Tags = append(confEvt.Tags, nostr.Tag{"email", a.Email})
	evtEvt.Tags = append(evtEvt.Tags, nostr.Tag{"e", sessionID.Hex()})
	for _, operator := range selected {
		evtEvt.Tags = append(evtEvt.Tags, nostr.Tag{"operator", operator.URL})
	}
	if err := evtEvt.Sign(settings.secretKey); err != nil {
		return fmt.Errorf("sign event request event: %w", err)
	}

	// send these payloads and get back a partial signature from each operator
	partials := make([]frost.PartialSignature, len(selected))
	g, gctx = errgroup.WithContext(ctx)
	for i, operator := range selected {
		g.Go(func() error {
			log.Info().Str("operator", operator.URL).Msg("sending group commitment to operator")
			if _, err := postEvent(gctx, operator.URL, groupCommitEvt); err != nil {
				return err
			}

			log.Info().Str("operator", operator.URL).Msg("requesting partial signature")
			body, err := postEvent(gctx, operator.URL, evtEvt)
			if err != nil {
				return err
			}

			var partial frost.PartialSignature
			if err := partial.DecodeHex(body); err != nil {
				return fmt.Errorf("decode partial signature from %s: %w", operator.URL, err)
			}

			partials[i] = partial
			log.Info().Str("operator", operator.URL).Int("signer_id", partial.SignerIdentifier).Msg("partial signature received")
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// verify the partial signatures or see which operator did things wrong
	for i, operator := range selected {
		lambdaRegistryLock.Lock()
		verifyErr := cfg.VerifyPartialSignature(
			operator.shard,
			commitments[i].BinoncePublic,
			bindingCoefficient,
			finalNonce,
			partials[i],
			event.ID[:],
			lambdaRegistry,
		)
		lambdaRegistryLock.Unlock()
		if verifyErr != nil {
			return fmt.Errorf("partial signature from operator %s isn't good: %w", operator.URL, verifyErr)
		}

		log.Info().
			Str("operator", operator.URL).
			Int("signer_id", partials[i].SignerIdentifier).
			Msg("got good partial signature")
	}

	// aggregate signature and attach it to the event
	log.Info().Msg("aggregating")
	sig, err := cfg.AggregateSignatures(finalNonce, partials)
	if err != nil {
		return fmt.Errorf("failed to aggregate signatures: %w", err)
	}

	event.Sig = [64]byte(sig.Serialize())
	log.Info().Msg("signing session finished")
	return nil
}
