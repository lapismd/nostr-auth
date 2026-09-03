package common

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const stateCookieName = "google_oauth_state"
const intentCookieName = "pomegranate_intent"

func oauthCookieSecure(redirectURL string) bool {
	parsed, err := url.Parse(redirectURL)
	return err == nil && strings.EqualFold(parsed.Scheme, "https")
}

type GoogleUser struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Picture       string `json:"picture"`
}

func HandleGoogleLogin(oauthConfig *oauth2.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			http.Error(w, "failed to generate oauth state", http.StatusInternalServerError)
			return
		}
		state := base64.RawURLEncoding.EncodeToString(buf)

		http.SetCookie(w, &http.Cookie{
			Name:     stateCookieName,
			Value:    state,
			Path:     "/",
			HttpOnly: true,
			Secure:   oauthCookieSecure(oauthConfig.RedirectURL),
			SameSite: http.SameSiteLaxMode,
			MaxAge:   600,
		})

		intent := r.URL.Query().Get("intent")
		if intent != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     intentCookieName,
				Value:    intent,
				Path:     "/",
				HttpOnly: true,
				Secure:   oauthCookieSecure(oauthConfig.RedirectURL),
				SameSite: http.SameSiteLaxMode,
				MaxAge:   600,
			})
		}

		http.Redirect(w, r, oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOnline), http.StatusFound)
	}
}

func HandleGoogleCallback(r *http.Request, oauthConfig *oauth2.Config) (GoogleUser, string, error) {
	cookie, err := r.Cookie(stateCookieName)
	if err != nil {
		return GoogleUser{}, "", fmt.Errorf("missing oauth state cookie")
	}

	intentCookie, _ := r.Cookie(intentCookieName)
	intent := ""
	if intentCookie != nil {
		intent = intentCookie.Value
	}

	state := r.URL.Query().Get("state")
	if state == "" || state != cookie.Value {
		return GoogleUser{}, "", fmt.Errorf("invalid oauth state")
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		return GoogleUser{}, "", fmt.Errorf("missing oauth code")
	}

	token, err := oauthConfig.Exchange(r.Context(), code)
	if err != nil {
		return GoogleUser{}, "", fmt.Errorf("failed to exchange oauth code")
	}

	client := oauthConfig.Client(r.Context(), token)
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)

	resp, err := client.Do(req)
	if err != nil {
		return GoogleUser{}, "", fmt.Errorf("failed to call google: "+err.Error(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return GoogleUser{}, "", errors.New("google returned not ok (" + strconv.Itoa(resp.StatusCode) + ")")
	}

	var user GoogleUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return GoogleUser{}, "", errors.New("google returned a broken response: " + err.Error())
	}

	if !user.VerifiedEmail {
		return GoogleUser{}, "", fmt.Errorf("user email not verified")
	}

	return user, intent, nil
}

// --- GitHub ---

type GitHubUser struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	Email string `json:"email"`
}

type GitHubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

const githubStateCookieName = "github_oauth_state"

func HandleGitHubLogin(oauthConfig *oauth2.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			http.Error(w, "failed to generate oauth state", http.StatusInternalServerError)
			return
		}
		state := base64.RawURLEncoding.EncodeToString(buf)

		http.SetCookie(w, &http.Cookie{
			Name:     githubStateCookieName,
			Value:    state,
			Path:     "/",
			HttpOnly: true,
			Secure:   oauthCookieSecure(oauthConfig.RedirectURL),
			SameSite: http.SameSiteLaxMode,
			MaxAge:   600,
		})

		intent := r.URL.Query().Get("intent")
		if intent != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     intentCookieName,
				Value:    intent,
				Path:     "/",
				HttpOnly: true,
				Secure:   oauthCookieSecure(oauthConfig.RedirectURL),
				SameSite: http.SameSiteLaxMode,
				MaxAge:   600,
			})
		}

		http.Redirect(w, r, oauthConfig.AuthCodeURL(state), http.StatusFound)
	}
}

func HandleGitHubCallback(r *http.Request, oauthConfig *oauth2.Config) (GitHubUser, string, error) {
	cookie, err := r.Cookie(githubStateCookieName)
	if err != nil {
		return GitHubUser{}, "", fmt.Errorf("missing oauth state cookie")
	}

	intentCookie, _ := r.Cookie(intentCookieName)
	intent := ""
	if intentCookie != nil {
		intent = intentCookie.Value
	}

	state := r.URL.Query().Get("state")
	if state == "" || state != cookie.Value {
		return GitHubUser{}, "", fmt.Errorf("invalid oauth state")
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		return GitHubUser{}, "", fmt.Errorf("missing oauth code")
	}

	token, err := oauthConfig.Exchange(r.Context(), code)
	if err != nil {
		return GitHubUser{}, "", fmt.Errorf("failed to exchange oauth code")
	}

	client := oauthConfig.Client(r.Context(), token)
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://api.github.com/user", nil)

	resp, err := client.Do(req)
	if err != nil {
		return GitHubUser{}, "", fmt.Errorf("failed to call github: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return GitHubUser{}, "", fmt.Errorf("github returned not ok (%d)", resp.StatusCode)
	}

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return GitHubUser{}, "", fmt.Errorf("github returned broken response: %w", err)
	}

	if user.Email == "" {
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://api.github.com/user/emails", nil)
		resp, err := client.Do(req)
		if err != nil {
			return GitHubUser{}, "", fmt.Errorf("failed to call github emails: %w", err)
		}
		defer resp.Body.Close()

		var emails []GitHubEmail
		if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
			return GitHubUser{}, "", fmt.Errorf("github returned broken emails: %w", err)
		}

		for _, e := range emails {
			if e.Primary && e.Verified {
				user.Email = e.Email
				break
			}
		}
	}

	if user.Email == "" {
		return GitHubUser{}, "", fmt.Errorf("no verified email found on github")
	}

	return user, intent, nil
}

// --- Microsoft ---

type MicrosoftUser struct {
	ID                string `json:"id"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
}

const microsoftStateCookieName = "microsoft_oauth_state"

func HandleMicrosoftLogin(oauthConfig *oauth2.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			http.Error(w, "failed to generate oauth state", http.StatusInternalServerError)
			return
		}
		state := base64.RawURLEncoding.EncodeToString(buf)

		http.SetCookie(w, &http.Cookie{
			Name:     microsoftStateCookieName,
			Value:    state,
			Path:     "/",
			HttpOnly: true,
			Secure:   oauthCookieSecure(oauthConfig.RedirectURL),
			SameSite: http.SameSiteLaxMode,
			MaxAge:   600,
		})

		intent := r.URL.Query().Get("intent")
		if intent != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     intentCookieName,
				Value:    intent,
				Path:     "/",
				HttpOnly: true,
				Secure:   oauthCookieSecure(oauthConfig.RedirectURL),
				SameSite: http.SameSiteLaxMode,
				MaxAge:   600,
			})
		}

		http.Redirect(w, r, oauthConfig.AuthCodeURL(state), http.StatusFound)
	}
}

func HandleMicrosoftCallback(r *http.Request, oauthConfig *oauth2.Config) (MicrosoftUser, string, error) {
	cookie, err := r.Cookie(microsoftStateCookieName)
	if err != nil {
		return MicrosoftUser{}, "", fmt.Errorf("missing oauth state cookie")
	}

	intentCookie, _ := r.Cookie(intentCookieName)
	intent := ""
	if intentCookie != nil {
		intent = intentCookie.Value
	}

	state := r.URL.Query().Get("state")
	if state == "" || state != cookie.Value {
		return MicrosoftUser{}, "", fmt.Errorf("invalid oauth state")
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		return MicrosoftUser{}, "", fmt.Errorf("missing oauth code")
	}

	token, err := oauthConfig.Exchange(r.Context(), code)
	if err != nil {
		return MicrosoftUser{}, "", fmt.Errorf("failed to exchange oauth code")
	}

	client := oauthConfig.Client(r.Context(), token)
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://graph.microsoft.com/v1.0/me", nil)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return MicrosoftUser{}, "", fmt.Errorf("failed to call microsoft graph: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return MicrosoftUser{}, "", fmt.Errorf("microsoft graph returned not ok (%d)", resp.StatusCode)
	}

	var user MicrosoftUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return MicrosoftUser{}, "", fmt.Errorf("microsoft graph returned broken response: %w", err)
	}

	email := user.Mail
	if email == "" {
		email = user.UserPrincipalName
	}
	if email == "" {
		return MicrosoftUser{}, "", fmt.Errorf("no email found from microsoft")
	}
	user.Mail = email

	return user, intent, nil
}

// --- Apple ---

type AppleUser struct {
	ID    string `json:"sub"`
	Email string `json:"email"`
}

const (
	appleStateCookieName = "apple_oauth_state"
	appleNonceCookieName = "apple_oauth_nonce"
	appleIssuer          = "https://appleid.apple.com"
	appleJWKSURL         = "https://appleid.apple.com/auth/keys"
)

func HandleAppleLogin(oauthConfig *oauth2.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			http.Error(w, "failed to generate oauth state", http.StatusInternalServerError)
			return
		}
		state := base64.RawURLEncoding.EncodeToString(buf)

		http.SetCookie(w, &http.Cookie{
			Name:     appleStateCookieName,
			Value:    state,
			Path:     "/",
			HttpOnly: true,
			Secure:   oauthCookieSecure(oauthConfig.RedirectURL),
			SameSite: http.SameSiteNoneMode,
			MaxAge:   600,
		})

		if _, err := rand.Read(buf); err != nil {
			http.Error(w, "failed to generate oauth nonce", http.StatusInternalServerError)
			return
		}
		nonce := base64.RawURLEncoding.EncodeToString(buf)
		http.SetCookie(w, &http.Cookie{
			Name:     appleNonceCookieName,
			Value:    nonce,
			Path:     "/",
			HttpOnly: true,
			Secure:   oauthCookieSecure(oauthConfig.RedirectURL),
			SameSite: http.SameSiteNoneMode,
			MaxAge:   600,
		})

		intent := r.URL.Query().Get("intent")
		if intent != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     intentCookieName,
				Value:    intent,
				Path:     "/",
				HttpOnly: true,
				Secure:   oauthCookieSecure(oauthConfig.RedirectURL),
				SameSite: http.SameSiteNoneMode,
				MaxAge:   600,
			})
		}

		http.Redirect(w, r, oauthConfig.AuthCodeURL(state,
			oauth2.SetAuthURLParam("response_mode", "form_post"),
			oauth2.SetAuthURLParam("scope", "name email"),
			oauth2.SetAuthURLParam("nonce", nonce),
		), http.StatusFound)
	}
}

func HandleAppleCallback(r *http.Request, clientID string) (AppleUser, string, error) {
	cookie, err := r.Cookie(appleStateCookieName)
	if err != nil {
		return AppleUser{}, "", fmt.Errorf("missing oauth state cookie")
	}
	nonceCookie, err := r.Cookie(appleNonceCookieName)
	if err != nil {
		return AppleUser{}, "", fmt.Errorf("missing oauth nonce cookie")
	}

	intentCookie, _ := r.Cookie(intentCookieName)
	intent := ""
	if intentCookie != nil {
		intent = intentCookie.Value
	}

	state := r.FormValue("state")
	if state == "" {
		state = r.URL.Query().Get("state")
	}
	if state == "" || state != cookie.Value {
		return AppleUser{}, "", fmt.Errorf("invalid oauth state")
	}

	idToken := r.FormValue("id_token")
	if idToken == "" {
		return AppleUser{}, "", fmt.Errorf("missing id_token from apple")
	}

	claims, err := verifyAppleIDToken(idToken, clientID, nonceCookie.Value)
	if err != nil {
		return AppleUser{}, "", err
	}

	return AppleUser{ID: claims.Subject, Email: claims.Email}, intent, nil
}

type appleJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type appleIDTokenClaims struct {
	Issuer        string `json:"iss"`
	Audience      string `json:"aud"`
	Subject       string `json:"sub"`
	ExpiresAt     int64  `json:"exp"`
	IssuedAt      int64  `json:"iat"`
	Nonce         string `json:"nonce"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

var (
	appleKeysMux       sync.Mutex
	appleKeysCache     []appleJWK
	appleKeysFetchedAt time.Time
	appleJWKSClient    = &http.Client{Timeout: 10 * time.Second}
	appleKeysSource    = fetchAppleKeys
)

func fetchAppleKeys() ([]appleJWK, error) {
	appleKeysMux.Lock()
	defer appleKeysMux.Unlock()

	if appleKeysCache != nil && time.Since(appleKeysFetchedAt) < time.Hour {
		return appleKeysCache, nil
	}

	req, _ := http.NewRequest(http.MethodGet, appleJWKSURL, nil)
	resp, err := appleJWKSClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch apple jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apple jwks returned not ok (%d)", resp.StatusCode)
	}

	var jwks struct {
		Keys []appleJWK `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode apple jwks: %w", err)
	}

	appleKeysCache = jwks.Keys
	appleKeysFetchedAt = time.Now()
	return appleKeysCache, nil
}

func verifyAppleIDToken(idToken, clientID, nonce string) (*appleIDTokenClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid id_token from apple")
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeJWTBase64(parts[0], &header); err != nil {
		return nil, fmt.Errorf("failed to decode apple id_token header: %w", err)
	}
	if header.Alg != "ES256" {
		return nil, fmt.Errorf("unexpected apple id_token algorithm %q", header.Alg)
	}

	keys, err := appleKeysSource()
	if err != nil {
		return nil, err
	}

	var key *appleJWK
	for i := range keys {
		if keys[i].Kid == header.Kid {
			key = &keys[i]
			break
		}
	}
	if key == nil {
		return nil, fmt.Errorf("apple signing key %q not found", header.Kid)
	}

	x, err := decodeBase64URL(key.X)
	if err != nil {
		return nil, fmt.Errorf("failed to decode apple key x: %w", err)
	}
	y, err := decodeBase64URL(key.Y)
	if err != nil {
		return nil, fmt.Errorf("failed to decode apple key y: %w", err)
	}

	pubKey := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(x),
		Y:     new(big.Int).SetBytes(y),
	}
	if !pubKey.Curve.IsOnCurve(pubKey.X, pubKey.Y) {
		return nil, fmt.Errorf("apple signing key not on curve")
	}

	sig, err := decodeBase64URL(parts[2])
	if err != nil || len(sig) != 64 {
		return nil, fmt.Errorf("invalid apple id_token signature encoding")
	}

	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(pubKey, digest[:], r, s) {
		return nil, fmt.Errorf("invalid apple id_token signature")
	}

	var claims appleIDTokenClaims
	if err := decodeJWTBase64(parts[1], &claims); err != nil {
		return nil, fmt.Errorf("failed to decode apple id_token payload: %w", err)
	}

	if claims.Issuer != appleIssuer && claims.Issuer != appleIssuer+"/" {
		return nil, fmt.Errorf("invalid apple id_token issuer %q", claims.Issuer)
	}
	if claims.Audience != clientID {
		return nil, fmt.Errorf("invalid apple id_token audience")
	}
	if claims.ExpiresAt < time.Now().Unix() {
		return nil, fmt.Errorf("apple id_token expired")
	}
	if claims.Nonce == "" || nonce == "" || claims.Nonce != nonce {
		return nil, fmt.Errorf("invalid apple id_token nonce")
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("no user id found in apple id_token")
	}
	if claims.Email == "" {
		return nil, fmt.Errorf("no email found in apple id_token")
	}
	if !claims.EmailVerified {
		return nil, fmt.Errorf("apple email not verified")
	}

	return &claims, nil
}

func decodeBase64URL(s string) ([]byte, error) {
	if data, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	if l := len(s) % 4; l != 0 {
		s += strings.Repeat("=", 4-l)
	}
	return base64.URLEncoding.DecodeString(s)
}

func decodeJWTBase64(part string, v any) error {
	data, err := decodeBase64URL(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
