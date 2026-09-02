package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/pomegranate/common"
	"fiatjaf.com/promenade/frost"
	"go.etcd.io/bbolt"
	"golang.org/x/oauth2"
)

type RegistrationOperator struct {
	Confirmed bool
	PubShard  string
}

type Registration struct {
	Email     string
	Operators map[string]RegistrationOperator // operator URL -> registration data
	Threshold int
	PublicKey nostr.PubKey
	Session   string // X-Pomegranate-Session
	CreatedAt time.Time
}

type JSONError struct {
	Error string `json:"error"`
}

const stateCookieName = "google_oauth_state"

func verifyToken(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Token ") {
		return "", fmt.Errorf("missing or invalid authorization header")
	}
	tokenB64 := strings.TrimPrefix(auth, "Token ")
	j, err := base64.StdEncoding.DecodeString(tokenB64)
	if err != nil {
		return "", err
	}
	var evt nostr.Event
	if err := json.Unmarshal(j, &evt); err != nil {
		return "", err
	}
	if ok := evt.VerifySignature(); !ok {
		return "", fmt.Errorf("invalid signature")
	}
	if evt.PubKey != settings.secretKey.Public() {
		return "", fmt.Errorf("wrong pubkey in authorization token")
	}
	if evt.Kind != common.KindCentralToken {
		return "", fmt.Errorf("invalid kind")
	}
	if evt.CreatedAt.Time().Before(time.Now().Add(-24 * time.Hour)) {
		return "", fmt.Errorf("token expired")
	}
	for _, tag := range evt.Tags {
		if tag[0] == "email" && len(tag) > 1 {
			return tag[1], nil
		}
	}
	return "", fmt.Errorf("email not found in token")
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeJSON(w, http.StatusNotFound, JSONError{Error: "path not found"})
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, "pomegranate central\n\n- coordinates FROST operators.\n- manages user bunker profiles.\n- serves NIP-46 signing requests.\n\nhttps://viewsource.win/fiatjaf.com/pomegranate")
}

func handleDebug(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/debug" {
		writeJSON(w, http.StatusNotFound, JSONError{Error: "path not found"})
		return
	}

	accounts, profiles, err := getRecordCounts()
	if err != nil {
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	pending := 0
	for range pendingRegistrations.Range {
		pending++
	}

	health := getOperatorHealthSnapshot()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "pomegranate central debug\n\n")
	fmt.Fprintf(w, "started_at: %s\n", startedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "uptime: %s\n", time.Since(startedAt).Round(time.Second))
	fmt.Fprintf(w, "service_url: %s\n", settings.ServiceURL)
	fmt.Fprintf(w, "db_path: %s\n", settings.DBPath)
	fmt.Fprintf(w, "accounts: %d\n", accounts)
	fmt.Fprintf(w, "profiles: %d\n", profiles)
	fmt.Fprintf(w, "pending_registrations: %d\n", pending)
	fmt.Fprintf(w, "known_operators: %d\n", health.KnownCount)
	fmt.Fprintf(w, "offline_operators: %d\n", len(health.Offline))
	if !health.LastCheckAt.IsZero() {
		fmt.Fprintf(w, "last_operator_check_at: %s\n", health.LastCheckAt.Format(time.RFC3339))
		fmt.Fprintf(w, "last_operator_check_took: %s\n", health.LastCheckTook.Round(time.Millisecond))
	}

	if len(health.Offline) == 0 {
		fmt.Fprintf(w, "\noffline_operator_details: none\n")
	} else {
		fmt.Fprintf(w, "\noffline_operator_details:\n")
		for _, operator := range health.Offline {
			fmt.Fprintf(w, "- %s\n", operator.URL)
			fmt.Fprintf(w, "  offline_since: %s\n", operator.OfflineSince.Format(time.RFC3339))
			fmt.Fprintf(w, "  offline_for: %s\n", time.Since(operator.OfflineSince).Round(time.Second))
		}
	}

	cachedOffline := getCachedOfflineOperators()
	fmt.Fprintf(w, "\ncached_offline_operators: %d\n", len(cachedOffline))
	if len(cachedOffline) > 0 {
		fmt.Fprintf(w, "cached_offline_details:\n")
		for _, op := range cachedOffline {
			fmt.Fprintf(w, "- %s\n", op.URL)
			fmt.Fprintf(w, "  offline_since: %s\n", op.OfflineSince.Format(time.RFC3339))
			fmt.Fprintf(w, "  cached_until: %s\n", op.CachedUntil.Format(time.RFC3339))
			fmt.Fprintf(w, "  remaining_cache: %s\n", time.Until(op.CachedUntil).Round(time.Second))
		}
	}
}

func handleGoogleCallback(oauthConfig *oauth2.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _, err := common.HandleGoogleCallback(r, oauthConfig)
		if err != nil {
			log.Warn().Err(err).Msg("google callback: fetch user failed")
			http.Error(w, "failed to fetch google user: "+err.Error(), http.StatusBadGateway)
			return
		}

		tokenB64, err := mintCentralToken(user.Email)
		if err != nil {
			log.Error().Err(err).Str("email", user.Email).Msg("google callback: mint token failed")
			http.Error(w, "failed to sign token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		log.Info().Str("email", user.Email).Msg("google callback: success")
		callbackPage(tokenB64).Render(r.Context(), w)
	}
}

func handleGoogleAndroidLogin(w http.ResponseWriter, r *http.Request) {
	var idTokenRequest struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&idTokenRequest); err != nil {
		log.Warn().Err(err).Msg("google android login: bad json")
		http.Error(w, "bad json request", http.StatusBadRequest)
		return
	}

	req, _ := http.NewRequestWithContext(
		r.Context(),
		http.MethodGet,
		"https://oauth2.googleapis.com/tokeninfo?id_token="+url.QueryEscape(idTokenRequest.IDToken),
		nil,
	)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Warn().Err(err).Msg("google android login: call google failed")
		http.Error(w, "failed to call google: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var payload struct {
		Subject       string `json:"sub"`
		Audience      string `json:"aud"`
		Email         string `json:"email"`
		EmailVerified string `json:"email_verified"`
		Picture       string `json:"picture"`
		ExpiresIn     string `json:"expires_in"`
		Error         string `json:"error_description"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("google android login: decode response failed")
		http.Error(w, "google returned a broken response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK {
		if payload.Error != "" {
			log.Warn().Str("error", payload.Error).Msg("google android login: token rejected")
			http.Error(w, "google rejected id token: "+payload.Error, http.StatusInternalServerError)
			return
		}
		log.Warn().Int("status", resp.StatusCode).Msg("google android login: token rejected")
		http.Error(w, "google rejected id token ("+strconv.Itoa(resp.StatusCode)+")", http.StatusInternalServerError)
		return
	}

	if payload.Email == "" || payload.Subject == "" {
		log.Warn().Str("email", payload.Email).Str("sub", payload.Subject).Msg("google android login: missing claims")
		http.Error(w, "google token missing required claims", http.StatusInternalServerError)
		return
	}
	if payload.Audience == "" {
		log.Warn().Str("email", payload.Email).Msg("google android login: missing audience")
		http.Error(w, "google token missing audience", http.StatusInternalServerError)
		return
	}

	if payload.Audience != settings.GoogleClientID {
		log.Warn().Str("email", payload.Email).Str("audience", payload.Audience).Msg("google android login: wrong audience")
		http.Error(w, "invalid google token audience", http.StatusInternalServerError)
		return
	}
	if payload.EmailVerified != "true" {
		log.Warn().Str("email", payload.Email).Msg("google android login: email not verified")
		http.Error(w, "user email not verified", http.StatusInternalServerError)
		return
	}

	user := common.GoogleUser{
		ID:            payload.Subject,
		Email:         payload.Email,
		VerifiedEmail: true,
		Picture:       payload.Picture,
	}

	tokenB64, err := mintCentralToken(user.Email)
	if err != nil {
		log.Error().Err(err).Str("email", user.Email).Msg("google android login: mint token failed")
		http.Error(w, "failed to sign token", http.StatusInternalServerError)
		return
	}

	log.Info().Str("email", user.Email).Msg("google android login: success")
	writeJSON(w, http.StatusOK, map[string]string{"token": tokenB64})
}

func handleGitHubCallback(oauthConfig *oauth2.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _, err := common.HandleGitHubCallback(r, oauthConfig)
		if err != nil {
			http.Error(w, "failed to fetch github user: "+err.Error(), http.StatusBadGateway)
			return
		}

		tokenB64, err := mintCentralToken(user.Email)
		if err != nil {
			http.Error(w, "failed to sign token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		callbackPage(tokenB64).Render(r.Context(), w)
	}
}

func handleMicrosoftCallback(oauthConfig *oauth2.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _, err := common.HandleMicrosoftCallback(r, oauthConfig)
		if err != nil {
			http.Error(w, "failed to fetch microsoft user: "+err.Error(), http.StatusBadGateway)
			return
		}

		tokenB64, err := mintCentralToken(user.Mail)
		if err != nil {
			http.Error(w, "failed to sign token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		callbackPage(tokenB64).Render(r.Context(), w)
	}
}

func handleAppleCallback() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _, err := common.HandleAppleCallback(r, settings.AppleClientID)
		if err != nil {
			http.Error(w, "failed to process apple sign in: "+err.Error(), http.StatusBadGateway)
			return
		}

		tokenB64, err := mintCentralToken(user.Email)
		if err != nil {
			http.Error(w, "failed to sign token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		callbackPage(tokenB64).Render(r.Context(), w)
	}
}

func mintCentralToken(email string) (string, error) {
	ev := nostr.Event{
		Kind:      common.KindCentralToken,
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{nostr.Tag{"email", normalizeEmail(email)}},
		Content:   "",
	}
	if err := ev.Sign(settings.secretKey); err != nil {
		return "", err
	}
	j, _ := json.Marshal(ev)
	return base64.StdEncoding.EncodeToString(j), nil
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	email, err := verifyToken(r)
	if err != nil {
		log.Warn().Err(err).Msg("register: verify token failed")
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	email = normalizeEmail(email)

	if _, isBlocked := blockedEmails[email]; isBlocked {
		log.Warn().Str("email", email).Msg("register: blocked email")
		http.Error(w, "registration failed", http.StatusForbidden)
		return
	}

	var evt nostr.Event
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		log.Warn().Err(err).Str("email", email).Msg("register: decode event")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if evt.Kind != common.KindCentralRegistration {
		log.Warn().Int("kind", int(evt.Kind)).Str("email", email).Msg("register: wrong kind")
		http.Error(w, "invalid kind", http.StatusBadRequest)
		return
	}
	if !evt.VerifySignature() {
		log.Warn().Str("email", email).Msg("register: invalid signature")
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}
	if evt.CreatedAt.Time().Before(time.Now().Add(-1 * time.Minute)) {
		log.Warn().Time("created_at", evt.CreatedAt.Time()).Str("email", email).Msg("register: event too old")
		http.Error(w, "event too old", http.StatusBadRequest)
		return
	}

	reg := &Registration{
		Email:     email,
		Operators: make(map[string]RegistrationOperator, len(evt.Tags)),
		PublicKey: evt.PubKey,
		Session:   r.Header.Get("X-Pomegranate-Session"),
		CreatedAt: time.Now(),
	}

	log.Info().Str("email", email).Str("pubkey", evt.PubKey.Hex()).Msg("register: request")

	seenThreshold := false
	for _, tag := range evt.Tags {
		if len(tag) == 0 {
			continue
		}

		switch tag[0] {
		case "threshold":
			if seenThreshold {
				log.Warn().Str("email", email).Msg("register: duplicate threshold")
				http.Error(w, "duplicate threshold tag", http.StatusBadRequest)
				return
			}
			if len(tag) < 2 {
				log.Warn().Str("email", email).Msg("register: missing threshold value")
				http.Error(w, "missing threshold value", http.StatusBadRequest)
				return
			}

			threshold, err := strconv.Atoi(tag[1])
			if err != nil {
				log.Warn().Err(err).Str("email", email).Str("value", tag[1]).Msg("register: invalid threshold")
				http.Error(w, "invalid threshold", http.StatusBadRequest)
				return
			}
			reg.Threshold = threshold
			seenThreshold = true
		case "operator":
			if len(tag) < 3 {
				log.Warn().Str("email", email).Str("url", tag[1]).Msg("register: operator tag missing pubshard")
				http.Error(w, "operator tag missing pubshard", http.StatusBadRequest)
				return
			}
			if _, exists := reg.Operators[tag[1]]; exists {
				log.Warn().Str("email", email).Str("url", tag[1]).Msg("register: duplicate operator")
				http.Error(w, "duplicate operator", http.StatusBadRequest)
				return
			}

			var shard frost.PublicKeyShard
			if err := shard.DecodeHex(tag[2]); err != nil {
				log.Warn().Err(err).Str("email", email).Str("url", tag[1]).Msg("register: invalid pubshard")
				http.Error(w, "invalid operator pubshard", http.StatusBadRequest)
				return
			}

			reg.Operators[tag[1]] = RegistrationOperator{PubShard: tag[2]}
		}
	}

	if len(reg.Operators) < 2 {
		log.Warn().Str("email", email).Int("n_operators", len(reg.Operators)).Msg("register: too few operators")
		http.Error(w, "need at least 2 operators", http.StatusBadRequest)
		return
	}
	if !seenThreshold {
		log.Warn().Str("email", email).Msg("register: missing threshold tag")
		http.Error(w, "missing threshold tag", http.StatusBadRequest)
		return
	}
	if reg.Threshold <= 0 || reg.Threshold > len(reg.Operators) {
		log.Warn().Str("email", email).Int("threshold", reg.Threshold).Int("n_operators", len(reg.Operators)).Msg("register: invalid threshold")
		http.Error(w, "invalid threshold", http.StatusBadRequest)
		return
	}

	// atomically check for conflicts (existing pending registration or existing
	// account) and store the new pending registration in a single critical section
	var conflictMsg string
	conflictStatus := http.StatusConflict
	pendingRegistrations.Compute(email, func(existing *Registration, loaded bool) (*Registration, bool) {
		if loaded {
			conflictMsg = "registration already in progress"
			log.Warn().Str("email", email).Msg("register: in progress")
			return existing, false
		}
		var accountExists bool
		if err := db.View(func(tx *bbolt.Tx) error {
			accountExists = tx.Bucket(ACCOUNT_BUCKET).Get([]byte(email)) != nil
			return nil
		}); err != nil {
			conflictMsg = "db error: " + err.Error()
			conflictStatus = http.StatusInternalServerError
			log.Error().Err(err).Str("email", email).Msg("register: db error")
			return nil, true
		}
		if accountExists {
			conflictMsg = "account already registered"
			log.Warn().Str("email", email).Msg("register: already registered")
			return nil, true
		}
		return reg, false
	})

	if conflictMsg != "" {
		http.Error(w, conflictMsg, conflictStatus)
		return
	}

	log.Info().Str("email", email).Int("threshold", reg.Threshold).Int("n_operators", len(reg.Operators)).Msg("register: pending registration stored")
	w.WriteHeader(http.StatusOK)
}

func handleAck(w http.ResponseWriter, r *http.Request) {
	email := normalizeEmail(r.FormValue("email"))
	url := r.FormValue("url")

	log.Info().Str("email", email).Str("operator", url).Msg("ack: received")

	// load and possibly mark as confirmed
	var returnErr error
	pendingRegistrations.Compute(email, func(reg *Registration, loaded bool) (*Registration, bool) {
		if !loaded {
			returnErr = fmt.Errorf("email %s not found", email)
			log.Warn().Str("email", email).Str("operator", url).Msg("ack: email not found")
			return nil, true
		}

		operator, exists := reg.Operators[url]
		if !exists {
			returnErr = fmt.Errorf("operator %s not found", url)
			log.Warn().Str("email", email).Str("operator", url).Msg("ack: operator not in registration")
			return reg, false
		}

		receivedToken, err := hex.DecodeString(r.Header.Get("X-Pomegranate-Operator-Token"))
		if err != nil {
			returnErr = fmt.Errorf("invalid operator token: %w", err)
			log.Warn().Err(err).Str("email", email).Str("operator", url).Msg("ack: invalid token")
			return reg, false
		}
		expectedToken := sha256.Sum256([]byte(reg.Session + ":" + url))
		if !slices.Equal(receivedToken, expectedToken[:]) {
			returnErr = fmt.Errorf("mismatched operator token")
			log.Warn().Str("email", email).Str("operator", url).Msg("ack: token mismatch")
			return reg, false
		}

		// mark as confirmed
		operator.Confirmed = true
		reg.Operators[url] = operator
		log.Info().Str("email", email).Str("operator", url).Msg("ack: operator confirmed")

		// check if all confirmed
		allConfirmed := true
		for _, operator := range reg.Operators {
			if !operator.Confirmed {
				allConfirmed = false
				break
			}
		}

		if allConfirmed {
			operators := make([]AccountOperator, 0, len(reg.Operators))
			for operatorURL, operator := range reg.Operators {
				operators = append(operators, AccountOperator{URL: operatorURL, PubShard: operator.PubShard})
			}

			account := AccountRecord{
				Operators: operators,
				Threshold: reg.Threshold,
				PubKey:    reg.PublicKey,
			}
			if err := db.Update(func(tx *bbolt.Tx) error {
				return saveAccountRecordTx(tx, email, account)
			}); err != nil {
				log.Error().Err(err).Str("email", email).Msg("ack: save account record failed")
				return reg, false
			}

			log.Info().Str("email", email).Int("threshold", reg.Threshold).Int("n_operators", len(reg.Operators)).Msg("ack: registration complete, account created")
			return nil, true // remove from map
		}

		// just commit our changes
		return reg, false
	})

	if returnErr != nil {
		http.Error(w, returnErr.Error(), http.StatusBadRequest)
		return
	}
}

func handleAccount(w http.ResponseWriter, r *http.Request) {
	email, err := verifyToken(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		account, err := loadAccountRecord(email)
		if err != nil {
			if errors.Is(err, errNotFound) {
				writeJSON(w, http.StatusNotFound, JSONError{Error: "no account registered with this email"})
				return
			}
			http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, account)
	case http.MethodDelete:
		if err := deleteAccountForEmail(email); err != nil {
			http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func deleteAccountForEmail(email string) error {
	profiles, err := listProfilesByEmail(email)
	if err != nil {
		return err
	}

	pendingRegistrations.Delete(email)
	if err := db.Update(func(tx *bbolt.Tx) error {
		return deleteAccountDataTx(tx, email)
	}); err != nil {
		return err
	}

	for _, profile := range profiles {
		nip46UserCache.Remove(profile.HandlerPubKey)
	}

	return nil
}

func handleProfiles(w http.ResponseWriter, r *http.Request) {
	email, err := verifyToken(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		profiles, err := listProfilesByEmail(email)
		if err != nil {
			http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, profiles)
	case http.MethodPost:
		var req CreateProfileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		handlerSecretKey := nostr.Generate()
		handlerPubKey := handlerSecretKey.Public().Hex()
		profile := ProfileRecord{
			HandlerSecretKey: handlerSecretKey.Hex(),
			Name:             req.Name,
			Restrictions:     req.Filter,
			Email:            email,
		}

		if err := db.Update(func(tx *bbolt.Tx) error {
			return saveProfileRecordTx(tx, handlerPubKey, profile)
		}); err != nil {
			http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusCreated, profile.Response(handlerPubKey))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleProfile(w http.ResponseWriter, r *http.Request) {
	email, err := verifyToken(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	handlerPubKey := r.PathValue("handlerPubKey")
	if handlerPubKey == "" {
		writeJSON(w, http.StatusNotFound, JSONError{Error: "missing profile pubkey"})
		return
	}
	if _, err := nostr.PubKeyFromHex(handlerPubKey); err != nil {
		http.Error(w, "invalid profile pubkey", http.StatusBadRequest)
		return
	}

	profile, err := loadProfileRecord(handlerPubKey)
	if err != nil {
		if errors.Is(err, errNotFound) {
			writeJSON(w, http.StatusNotFound, JSONError{Error: "bunker profile not found"})
			return
		}
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if profile.Email != email {
		writeJSON(w, http.StatusNotFound, JSONError{Error: "missing profile email?"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, profile.Response(handlerPubKey))
	case http.MethodDelete:
		if err := db.Update(func(tx *bbolt.Tx) error {
			return deleteProfileRecordTx(tx, handlerPubKey, profile)
		}); err != nil {
			http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		nip46UserCache.Remove(handlerPubKey)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPut, http.MethodPatch, http.MethodPost:
		if err := updateProfile(handlerPubKey, &profile, r); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, errNotFound) {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		writeJSON(w, http.StatusOK, profile.Response(handlerPubKey))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func updateProfile(handlerPubKey string, profile *ProfileRecord, r *http.Request) error {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return fmt.Errorf("invalid request")
	}

	if _, hasRestrictions := raw["restrictions"]; hasRestrictions {
		return fmt.Errorf("use only one of filter or restrictions")
	}

	changed := false
	if nameRaw, ok := raw["name"]; ok {
		var name string
		if err := json.Unmarshal(nameRaw, &name); err != nil {
			return fmt.Errorf("invalid name")
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("name is required")
		}
		profile.Name = name
		changed = true
	}

	if restrictionsRaw, ok := raw["restrictions"]; ok {
		var filter nostr.Filter
		if err := json.Unmarshal(restrictionsRaw, &filter); err != nil {
			return fmt.Errorf("invalid filter: %w", err)
		}
		profile.Restrictions = &filter
		changed = true
	}

	if !changed {
		return fmt.Errorf("nothing to update")
	}

	return db.Update(func(tx *bbolt.Tx) error {
		return saveProfileRecordTx(tx, handlerPubKey, *profile)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func getRecordCounts() (accounts int, profiles int, err error) {
	err = db.View(func(tx *bbolt.Tx) error {
		if bucket := tx.Bucket(ACCOUNT_BUCKET); bucket != nil {
			accounts = bucket.Stats().KeyN
		}
		if bucket := tx.Bucket(PROFILES_BUCKET); bucket != nil {
			profiles = bucket.Stats().KeyN
		}
		return nil
	})
	return accounts, profiles, err
}
