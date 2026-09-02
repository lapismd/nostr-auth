package main

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/pomegranate/common"
	"github.com/kelseyhightower/envconfig"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/cors"
	"github.com/rs/zerolog"
	"go.etcd.io/bbolt"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Settings struct {
	Port               string `envconfig:"PORT" default:"5033"`
	ServiceURL         string `envconfig:"SERVICE_URL" default:"http://localhost:5033"`
	GoogleClientID     string `envconfig:"GOOGLE_CLIENT_ID"`
	GoogleClientSecret string `envconfig:"GOOGLE_CLIENT_SECRET"`
	SecretKeyHex       string `envconfig:"SECRET_KEY" required:"true"`
	DBPath             string `envconfig:"DB_PATH" default:"central.db"`
	BlockedEmailsFile  string `envconfig:"BLOCKED_EMAILS_FILE"`

	GitHubClientID        string `envconfig:"GITHUB_CLIENT_ID"`
	GitHubClientSecret    string `envconfig:"GITHUB_CLIENT_SECRET"`
	MicrosoftClientID     string `envconfig:"MICROSOFT_CLIENT_ID"`
	MicrosoftClientSecret string `envconfig:"MICROSOFT_CLIENT_SECRET"`
	AppleClientID         string `envconfig:"APPLE_CLIENT_ID"`

	secretKey nostr.SecretKey
}

var (
	settings  Settings
	db        *bbolt.DB
	startedAt = time.Now()
	log       = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
	relay     = khatru.NewRelay()

	pendingRegistrations = xsync.NewMapOf[string, *Registration]()
)

func main() {
	err := envconfig.Process("", &settings)
	if err != nil {
		log.Fatal().Err(err).Msg("process env")
		return
	}

	if settings.secretKey, err = nostr.SecretKeyFromHex(settings.SecretKeyHex); err != nil {
		log.Fatal().Err(err).Msg("invalid SECRET_KEY")
		return
	}

	// Initialize bolt database
	db, err = bbolt.Open(settings.DBPath, 0600, nil)
	if err != nil {
		log.Fatal().Err(err).Str("path", settings.DBPath).Msg("open bolt db")
		return
	}
	defer db.Close()

	if err := db.Update(func(tx *bbolt.Tx) error {
		for _, bucket := range []string{"profiles", "emails", "account"} {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucket)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		log.Fatal().Err(err).Msg("create buckets")
	}

	// cleanup expired pending registrations every 30 seconds
	startOperatorHealthChecker()

	// load blocked emails into memory, re-read every 30 minutes
	startBlockedEmailsLoader()

	go func() {
		for range time.NewTicker(30 * time.Second).C {
			cutoff := time.Now().Add(-60 * time.Second)
			for email, reg := range pendingRegistrations.Range {
				if reg.CreatedAt.Before(cutoff) {
					pendingRegistrations.Delete(email)
				}
			}
		}
	}()

	// initialize khatru relay
	relay.Info.Name = strings.TrimPrefix(settings.ServiceURL, "https://")
	pk := settings.secretKey.Public()
	relay.Info.Self = &pk

	relay.OnEphemeralEvent = func(ctx context.Context, event nostr.Event) {
		if event.Kind == nostr.KindNostrConnect {
			handleNIP46Request(ctx, event)
		}
	}

	// the chatter between clients and this central signer should be invisible to anyone except the requester
	// otherwise the pubkey used by the client to contact the server will be made public and can be used by anyone
	relay.PreventBroadcast = func(ws *khatru.WebSocket, filter nostr.Filter, event nostr.Event) bool {
		if event.Kind != nostr.KindNostrConnect {
			return false
		}

		requester, ok := requesterByResponse.Load(event.ID)
		if !ok {
			// this isn't a response we know about, so it must be a request
			// don't broadcast requests to anyone who is listening
			return true
		}

		if ws == requester.ws {
			// this is the same websocket connection that sent the request, it's safe to send the response to it
			return false
		}

		for _, pk := range ws.AuthedPublicKeys {
			if pk == requester.pubkey {
				// even from a different connection the requester should still be able to read the response if authed
				return false
			}
		}

		return true
	}

	mux := relay.Router()

	if settings.GoogleClientID != "" && settings.GoogleClientSecret != "" {
		googleConfig := &oauth2.Config{
			ClientID:     settings.GoogleClientID,
			ClientSecret: settings.GoogleClientSecret,
			RedirectURL:  settings.ServiceURL + "/callback/google",
			Scopes: []string{
				"openid",
				"email",
			},
			Endpoint: google.Endpoint,
		}

		mux.HandleFunc("GET /login/google", common.HandleGoogleLogin(googleConfig))
		mux.HandleFunc("GET /callback/google", handleGoogleCallback(googleConfig))
		mux.HandleFunc("POST /login/google/android", handleGoogleAndroidLogin)
	}

	if settings.GitHubClientID != "" && settings.GitHubClientSecret != "" {
		gitHubConfig := &oauth2.Config{
			ClientID:     settings.GitHubClientID,
			ClientSecret: settings.GitHubClientSecret,
			RedirectURL:  settings.ServiceURL + "/callback/github",
			Scopes:       []string{"read:user", "user:email"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://github.com/login/oauth/authorize",
				TokenURL: "https://github.com/login/oauth/access_token",
			},
		}
		mux.HandleFunc("GET /login/github", common.HandleGitHubLogin(gitHubConfig))
		mux.HandleFunc("GET /callback/github", handleGitHubCallback(gitHubConfig))
	}

	if settings.MicrosoftClientID != "" && settings.MicrosoftClientSecret != "" {
		microsoftConfig := &oauth2.Config{
			ClientID:     settings.MicrosoftClientID,
			ClientSecret: settings.MicrosoftClientSecret,
			RedirectURL:  settings.ServiceURL + "/callback/microsoft",
			Scopes:       []string{"User.Read", "openid", "email"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
				TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
			},
		}
		mux.HandleFunc("GET /login/microsoft", common.HandleMicrosoftLogin(microsoftConfig))
		mux.HandleFunc("GET /callback/microsoft", handleMicrosoftCallback(microsoftConfig))
	}

	if settings.AppleClientID != "" {
		appleConfig := &oauth2.Config{
			ClientID:    settings.AppleClientID,
			RedirectURL: settings.ServiceURL + "/callback/apple",
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://appleid.apple.com/auth/authorize",
				TokenURL: "https://appleid.apple.com/auth/token",
			},
		}
		mux.HandleFunc("GET /login/apple", common.HandleAppleLogin(appleConfig))
		mux.HandleFunc("POST /callback/apple", handleAppleCallback())
	}

	mux.HandleFunc("/account", handleAccount)
	mux.HandleFunc("POST /register", handleRegister)
	mux.HandleFunc("POST /operator/ack", handleAck)
	mux.HandleFunc("/profiles", handleProfiles)
	mux.HandleFunc("/profiles/{handlerPubKey}", handleProfile)
	mux.HandleFunc("GET /debug", handleDebug)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/", handleRoot)

	server := &http.Server{
		Addr:              "localhost:" + settings.Port,
		Handler:           cors.AllowAll().Handler(relay),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Info().Str("addr", "http://localhost:"+settings.Port).Msg("central server listening")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("serve http")
	}
}
