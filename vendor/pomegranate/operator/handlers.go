package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/pomegranate/common"
	"fiatjaf.com/promenade/frost"
	"golang.org/x/oauth2"
)

func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	providers := make([]string, 0, len(supportedProviders))
	for p := range supportedProviders {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, "pomegranate operator\n\n- holds a FROST shard of your nostr secret key.\n- signs events on request\n- gives the shard back if you prove you are the registered user via "+strings.Join(providers, ", ")+".\n\nhttps://viewsource.win/fiatjaf.com/pomegranate")
}

func handleGoogleCallback(oauthConfig *oauth2.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, intent, err := common.HandleGoogleCallback(r, oauthConfig)
		if err != nil {
			log.Warn().Err(err).Msg("op google callback: fetch user failed")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var reg Registration
		if loaded, err := loadRegistration(user.Email); err != nil {
			if errors.Is(err, ErrAccountNotFound) {
				log.Warn().Str("email", user.Email).Msg("op google callback: account not found")
				http.Error(w, "account not found", http.StatusNotFound)
				return
			}
			log.Error().Err(err).Str("email", user.Email).Msg("op google callback: load registration failed")
			http.Error(w, "failed to load registration", http.StatusInternalServerError)
			return
		} else {
			reg = loaded
		}

		if intent == "erase" {
			log.Info().Str("email", user.Email).Msg("op google callback: erase intent")
			http.SetCookie(w, &http.Cookie{
				Name:     "reallyDelete",
				Value:    user.Email,
				Path:     "/po",
				MaxAge:   300,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Secure:   serviceCookiesSecure(),
			})
			confirmErasePage(user.Email).Render(r.Context(), w)
		} else {
			log.Info().Str("email", user.Email).Str("intent", intent).Msg("op google callback: success")
			confirmRecoveryPage(user.Email, reg.Shard).Render(r.Context(), w)
		}
	}
}

func handleGitHubCallback(oauthConfig *oauth2.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, intent, err := common.HandleGitHubCallback(r, oauthConfig)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var reg Registration
		if loaded, err := loadRegistration(user.Email); err != nil {
			if errors.Is(err, ErrAccountNotFound) {
				http.Error(w, "account not found", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to load registration", http.StatusInternalServerError)
			return
		} else {
			reg = loaded
		}

		if intent == "erase" {
			http.SetCookie(w, &http.Cookie{
				Name:     "reallyDelete",
				Value:    user.Email,
				Path:     "/po",
				MaxAge:   300,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Secure:   serviceCookiesSecure(),
			})
			confirmErasePage(user.Email).Render(r.Context(), w)
		} else {
			confirmRecoveryPage(user.Email, reg.Shard).Render(r.Context(), w)
		}
	}
}

func handleMicrosoftCallback(oauthConfig *oauth2.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, intent, err := common.HandleMicrosoftCallback(r, oauthConfig)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var reg Registration
		if loaded, err := loadRegistration(user.Mail); err != nil {
			if errors.Is(err, ErrAccountNotFound) {
				http.Error(w, "account not found", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to load registration", http.StatusInternalServerError)
			return
		} else {
			reg = loaded
		}

		if intent == "erase" {
			http.SetCookie(w, &http.Cookie{
				Name:     "reallyDelete",
				Value:    user.Mail,
				Path:     "/po",
				MaxAge:   300,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Secure:   serviceCookiesSecure(),
			})
			confirmErasePage(user.Mail).Render(r.Context(), w)
		} else {
			confirmRecoveryPage(user.Mail, reg.Shard).Render(r.Context(), w)
		}
	}
}

func handleAppleCallback() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, intent, err := common.HandleAppleCallback(r, settings.AppleClientID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var reg Registration
		if loaded, err := loadRegistration(user.Email); err != nil {
			if errors.Is(err, ErrAccountNotFound) {
				http.Error(w, "account not found", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to load registration", http.StatusInternalServerError)
			return
		} else {
			reg = loaded
		}

		if intent == "erase" {
			http.SetCookie(w, &http.Cookie{
				Name:     "reallyDelete",
				Value:    user.Email,
				Path:     "/po",
				MaxAge:   300,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Secure:   serviceCookiesSecure(),
			})
			confirmErasePage(user.Email).Render(r.Context(), w)
		} else {
			confirmRecoveryPage(user.Email, reg.Shard).Render(r.Context(), w)
		}
	}
}

func handleDeleteShard(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("reallyDelete")
	if err != nil {
		http.Error(w, "missing delete confirmation", http.StatusForbidden)
		return
	}

	email, err := mail.ParseAddress(cookie.Value)
	if err != nil || email.Address != cookie.Value {
		http.Error(w, "invalid delete confirmation", http.StatusForbidden)
		return
	}

	if err := deleteRegistration(cookie.Value); err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to delete registration", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "reallyDelete",
		Value:    "",
		Path:     "/po",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   serviceCookiesSecure(),
	})

	w.WriteHeader(http.StatusNoContent)
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	var evt nostr.Event
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		log.Warn().Err(err).Msg("op-register: decode event")
		http.Error(w, "failed to decode event", http.StatusBadRequest)
		return
	}
	if ok := evt.VerifySignature(); !ok {
		log.Warn().Str("pubkey", evt.PubKey.Hex()).Msg("op-register: bad signature")
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}
	if evt.Kind != common.KindOperatorRegistration {
		log.Warn().Int("kind", int(evt.Kind)).Str("pubkey", evt.PubKey.Hex()).Msg("op-register: wrong kind")
		http.Error(w, "invalid kind", http.StatusBadRequest)
		return
	}

	emailTag := evt.Tags.Find("email")
	centralTag := evt.Tags.Find("central")

	if emailTag == nil || len(emailTag) < 2 || centralTag == nil || len(centralTag) < 2 {
		log.Warn().Str("pubkey", evt.PubKey.Hex()).Msg("op-register: missing tags")
		http.Error(w, "missing or invalid tags", http.StatusBadRequest)
		return
	}
	centralURL, err := requireTrustedCentralURL(centralTag[1])
	if err != nil {
		log.Warn().Err(err).Str("pubkey", evt.PubKey.Hex()).Msg("op-register: untrusted central")
		http.Error(w, "untrusted central", http.StatusForbidden)
		return
	}

	log.Info().Str("email", emailTag[1]).Str("pubkey", evt.PubKey.Hex()).Str("central", centralURL).Msg("op-register: request")

	var shard frost.KeyShard
	if err := shard.DecodeHex(evt.Content); err != nil {
		log.Warn().Err(err).Str("email", emailTag[1]).Msg("op-register: invalid shard hex")
		http.Error(w, "invalid shard", http.StatusBadRequest)
		return
	}

	var pk nostr.PubKey
	shard.PublicKey.X.PutBytes((*[32]byte)(&pk))
	var our nostr.PubKey
	shard.PublicKeyShard.PublicKey.X.PutBytes((*[32]byte)(&our))

	oauthTag := evt.Tags.Find("oauth")
	if oauthTag != nil && len(oauthTag) > 1 {
		if !supportedProviders[oauthTag[1]] {
			http.Error(w, "operator does not support "+oauthTag[1]+" authentication", http.StatusBadRequest)
			return
		}
	} else if len(supportedProviders) == 0 {
		http.Error(w, "operator has no authentication methods configured", http.StatusBadRequest)
		return
	}

	// confirm on central
	req, err := http.NewRequestWithContext(
		r.Context(),
		http.MethodPost,
		fmt.Sprintf("%s/operator/ack", centralURL),
		strings.NewReader(url.Values{
			"email": {emailTag[1]},
			"url":   {settings.ServiceURL},
		}.Encode()),
	)
	if err != nil {
		log.Warn().Err(err).Str("email", emailTag[1]).Msg("op-register: create ack request")
		http.Error(w, "failed to confirm with central: "+err.Error(), http.StatusBadGateway)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Pomegranate-Operator-Token", r.Header.Get("X-Pomegranate-Operator-Token"))
	resp, err := centralHTTPClient.Do(req)
	if err != nil {
		log.Warn().Err(err).Str("email", emailTag[1]).Str("central", centralURL).Msg("op-register: ack to central failed")
		http.Error(w, "failed to confirm with central: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, readErr := readCentralResponseBody(resp.Body)
		log.Warn().Err(readErr).Str("email", emailTag[1]).Str("central", centralURL).Int("status", resp.StatusCode).Msg("op-register: central rejected ack")
		http.Error(w, "central rejected registration acknowledgement", http.StatusBadGateway)
		return
	}

	centralInfo, err := fetchCentralInfo(r.Context(), centralURL)
	if err != nil || centralInfo.Self == nil {
		log.Warn().Err(err).Str("email", emailTag[1]).Str("central", centralURL).Msg("op-register: fetch central pubkey")
		http.Error(w, "failed to fetch central pubkey", http.StatusBadGateway)
		return
	}

	var existing Registration
	existing, err = loadRegistration(emailTag[1])
	if err != nil && !errors.Is(err, ErrAccountNotFound) {
		log.Error().Err(err).Str("email", emailTag[1]).Msg("op-register: load existing registration")
		http.Error(w, "failed to load registration", http.StatusInternalServerError)
		return
	}
	if err == nil {
		// if a registration already exists for this email, require it to be from the same public key
		// (this prevents rogue actors from overwriting other people's shard registrations)
		// if one wants to use a new keypair with their previous email they'll have to delete their shards manually first
		if existing.PubKey == "" {
			// DEPRECATED, remove these hardcoded urls in 2028, start enforcing only full pubkey match (or maybe trust some preconfigured centrals?)
			if centralURL != "https://auth.njump.me" && centralURL != "https://auth.yakihonne.com" {
				log.Warn().Str("email", emailTag[1]).Str("central", centralURL).Msg("op-register: legacy empty pubkey registration rejected by central")
				http.Error(w, "a pubkey is already registered for this email, but it was never stored; this central cannot claim it", http.StatusForbidden)
				return
			}
		} else if existing.PubKey != evt.PubKey.Hex() {
			log.Warn().Str("email", emailTag[1]).Str("old", existing.PubKey).Str("new", evt.PubKey.Hex()).Msg("op-register: pubkey mismatch")
			http.Error(w, "a different pubkey is already registered for this email", http.StatusForbidden)
			return
		}
	}

	reg := Registration{
		Email:         emailTag[1],
		PubKey:        evt.PubKey.Hex(),
		Central:       centralURL,
		CentralPubKey: centralInfo.Self.Hex(),
		Shard:         evt.Content,
	}

	if err := saveRegistration(reg); err != nil {
		log.Error().Err(err).Str("email", emailTag[1]).Msg("op-register: save registration")
		http.Error(w, "failed to save registration", http.StatusInternalServerError)
		return
	}

	log.Info().Str("email", emailTag[1]).Str("central", centralURL).Msg("op-register: registration saved")
	w.WriteHeader(http.StatusOK)
}
