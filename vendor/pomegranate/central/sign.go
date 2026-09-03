package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"sync"

	"fiatjaf.com/nostr"
	"fiatjaf.com/pomegranate/common"
	"fiatjaf.com/promenade/frost"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/mailru/easyjson"
	"golang.org/x/sync/errgroup"
)

var (
	lambdaRegistry     = make(frost.LambdaRegistry)
	lambdaRegistryLock sync.Mutex
)

func newSigningRequestEvent(
	kind nostr.Kind,
	content string,
	email string,
	sessionID nostr.ID,
	selected []AccountOperator,
) nostr.Event {
	evt := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      kind,
		Content:   content,
		Tags:      make(nostr.Tags, 0, 2+len(selected)),
	}
	evt.Tags = append(evt.Tags, nostr.Tag{"email", email})
	evt.Tags = append(evt.Tags, nostr.Tag{"e", sessionID.Hex()})
	for _, operator := range selected {
		evt.Tags = append(evt.Tags, nostr.Tag{"operator", operator.URL})
	}
	return evt
}

func computeValidatedGroupCommitment(
	cfg *frost.Configuration,
	commitments []frost.Commitment,
	message []byte,
) (frost.BinoncePublic, *btcec.ModNScalar, *btcec.JacobianPoint, error) {
	ordered := slices.Clone(commitments)
	slices.SortFunc(ordered, func(a, b frost.Commitment) int {
		return a.SignerID - b.SignerID
	})
	if err := cfg.ValidateCommitmentList(ordered); err != nil {
		return frost.BinoncePublic{}, nil, nil, fmt.Errorf("invalid commitment list: %w", err)
	}

	groupCommitment, bindingCoefficient, finalNonce := cfg.ComputeGroupCommitment(commitments, message)
	return groupCommitment, bindingCoefficient, finalNonce, nil
}

func attachVerifiedEventSignature(event *nostr.Event, signature *schnorr.Signature) error {
	if signature == nil {
		return fmt.Errorf("aggregated signature is nil")
	}

	event.Sig = [64]byte(signature.Serialize())
	if !event.VerifySignature() {
		event.Sig = [64]byte{}
		return fmt.Errorf("aggregated signature failed verification")
	}
	return nil
}

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
			if commitment.SignerID != operator.shard.ID {
				return fmt.Errorf(
					"commitment from %s has signer id %d, expected %d",
					operator.URL,
					commitment.SignerID,
					operator.shard.ID,
				)
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
	groupCommitment, bindingCoefficient, finalNonce, err := computeValidatedGroupCommitment(
		cfg,
		commitments,
		event.ID[:],
	)
	if err != nil {
		return err
	}
	log.Info().Msg("group commitment computed")

	// prepare group commitment payload
	groupCommitEvt := newSigningRequestEvent(
		common.KindGroupCommit,
		groupCommitment.Hex(),
		a.Email,
		sessionID,
		selected,
	)
	if err := groupCommitEvt.Sign(settings.secretKey); err != nil {
		return fmt.Errorf("sign group commitment event: %w", err)
	}

	// prepare event to be signed payload
	jevt, _ := easyjson.Marshal(event)
	evtEvt := newSigningRequestEvent(
		common.KindEventToBeSigned,
		string(jevt),
		a.Email,
		sessionID,
		selected,
	)
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
			if partial.SignerIdentifier != operator.shard.ID {
				return fmt.Errorf(
					"partial signature from %s has signer id %d, expected %d",
					operator.URL,
					partial.SignerIdentifier,
					operator.shard.ID,
				)
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

	if err := attachVerifiedEventSignature(event, sig); err != nil {
		return err
	}
	log.Info().Msg("signing session finished")
	return nil
}
