package common

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func testJWK(t *testing.T, key *ecdsa.PublicKey, kid string) appleJWK {
	t.Helper()
	return appleJWK{
		Kty: "EC",
		Kid: kid,
		Use: "sig",
		Alg: "ES256",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(key.X.Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(key.Y.Bytes()),
	}
}

func signToken(t *testing.T, key *ecdsa.PrivateKey, header, claims map[string]any) string {
	t.Helper()

	hb, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}

	signingInput := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(cb)
	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}

	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestVerifyAppleIDToken(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	oldSource := appleKeysSource
	appleKeysSource = func() ([]appleJWK, error) {
		return []appleJWK{testJWK(t, &key.PublicKey, "testkid")}, nil
	}
	defer func() { appleKeysSource = oldSource }()

	clientID := "com.example.app"
	nonce := "abc123"
	header := map[string]any{"alg": "ES256", "kid": "testkid"}
	baseClaims := map[string]any{
		"iss":            "https://appleid.apple.com",
		"aud":            clientID,
		"sub":            "user123",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Unix(),
		"nonce":          nonce,
		"email":          "user@example.com",
		"email_verified": true,
	}

	tok := signToken(t, key, header, baseClaims)
	claims, err := verifyAppleIDToken(tok, clientID, nonce)
	if err != nil {
		t.Fatalf("expected valid token to pass: %v", err)
	}
	if claims.Email != "user@example.com" || claims.Subject != "user123" {
		t.Fatalf("wrong claims: %+v", claims)
	}

	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"wrong audience", func(c map[string]any) { c["aud"] = "other.app" }},
		{"expired", func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }},
		{"wrong nonce", func(c map[string]any) { c["nonce"] = "different" }},
		{"missing subject", func(c map[string]any) { delete(c, "sub") }},
		{"missing email", func(c map[string]any) { delete(c, "email") }},
		{"unverified email", func(c map[string]any) { c["email_verified"] = false }},
		{"wrong issuer", func(c map[string]any) { c["iss"] = "https://evil.example.com" }},
	}
	for _, tc := range cases {
		claims := make(map[string]any)
		for k, v := range baseClaims {
			claims[k] = v
		}
		tc.mutate(claims)
		tok := signToken(t, key, header, claims)
		if _, err := verifyAppleIDToken(tok, clientID, nonce); err == nil {
			t.Errorf("%s: expected rejection", tc.name)
		}
	}

	tok = signToken(t, other, header, baseClaims)
	if _, err := verifyAppleIDToken(tok, clientID, nonce); err == nil {
		t.Error("expected wrong signature to be rejected")
	}

	badAlg := map[string]any{"alg": "none", "kid": "testkid"}
	tok = signToken(t, key, badAlg, baseClaims)
	if _, err := verifyAppleIDToken(tok, clientID, nonce); err == nil {
		t.Error("expected wrong algorithm to be rejected")
	}

	unknownKid := map[string]any{"alg": "ES256", "kid": "nope"}
	tok = signToken(t, key, unknownKid, baseClaims)
	if _, err := verifyAppleIDToken(tok, clientID, nonce); err == nil {
		t.Error("expected unknown kid to be rejected")
	}
}
