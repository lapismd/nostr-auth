package main

import (
	"net/http"
	"os"
	"time"

	"fiatjaf.com/pomegranate/common"
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/cors"
	"github.com/rs/zerolog"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Settings struct {
	Port               string `envconfig:"PORT" default:"5041"`
	ServiceURL         string `envconfig:"SERVICE_URL" required:"true"`
	GoogleClientID     string `envconfig:"GOOGLE_CLIENT_ID"`
	GoogleClientSecret string `envconfig:"GOOGLE_CLIENT_SECRET"`
	DBPath             string `envconfig:"DB_PATH" default:"operator.db"`

	GitHubClientID        string `envconfig:"GITHUB_CLIENT_ID"`
	GitHubClientSecret    string `envconfig:"GITHUB_CLIENT_SECRET"`
	MicrosoftClientID     string `envconfig:"MICROSOFT_CLIENT_ID"`
	MicrosoftClientSecret string `envconfig:"MICROSOFT_CLIENT_SECRET"`
	AppleClientID         string `envconfig:"APPLE_CLIENT_ID"`
}

var (
	settings           Settings
	db                 *bolt.DB
	log                = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
	supportedProviders = make(map[string]bool)
)

func main() {
	err := envconfig.Process("", &settings)
	if err != nil {
		log.Fatal().Err(err).Msg("process env")
	}

	db, err = bolt.Open(settings.DBPath, 0600, nil)
	if err != nil {
		log.Fatal().Err(err).Str("path", settings.DBPath).Msg("open db")
	}
	defer db.Close()

	if err := db.Update(func(tx *bolt.Tx) error {
		return setupDB(tx)
	}); err != nil {
		log.Fatal().Err(err).Msg("setup db")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)

	if settings.GoogleClientID != "" && settings.GoogleClientSecret != "" {
		supportedProviders["google"] = true

		googleConfig := &oauth2.Config{
			ClientID:     settings.GoogleClientID,
			ClientSecret: settings.GoogleClientSecret,
			RedirectURL:  settings.ServiceURL + "/po/callback/google",
			Scopes: []string{
				"openid",
				"email",
			},
			Endpoint: google.Endpoint,
		}

		mux.HandleFunc("/po/action/google", common.HandleGoogleLogin(googleConfig))
		mux.HandleFunc("/po/recover/google", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/po/action/google?intent=recover", http.StatusFound)
		})
		mux.HandleFunc("/po/erase/google", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/po/action/google?intent=erase", http.StatusFound)
		})
		mux.HandleFunc("/po/callback/google", handleGoogleCallback(googleConfig))
	}

	if settings.GitHubClientID != "" && settings.GitHubClientSecret != "" {
		supportedProviders["github"] = true

		gitHubConfig := &oauth2.Config{
			ClientID:     settings.GitHubClientID,
			ClientSecret: settings.GitHubClientSecret,
			RedirectURL:  settings.ServiceURL + "/po/callback/github",
			Scopes:       []string{"read:user", "user:email"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://github.com/login/oauth/authorize",
				TokenURL: "https://github.com/login/oauth/access_token",
			},
		}

		mux.HandleFunc("/po/action/github", common.HandleGitHubLogin(gitHubConfig))
		mux.HandleFunc("/po/recover/github", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/po/action/github?intent=recover", http.StatusFound)
		})
		mux.HandleFunc("/po/erase/github", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/po/action/github?intent=erase", http.StatusFound)
		})
		mux.HandleFunc("/po/callback/github", handleGitHubCallback(gitHubConfig))
	}

	if settings.MicrosoftClientID != "" && settings.MicrosoftClientSecret != "" {
		supportedProviders["microsoft"] = true

		microsoftConfig := &oauth2.Config{
			ClientID:     settings.MicrosoftClientID,
			ClientSecret: settings.MicrosoftClientSecret,
			RedirectURL:  settings.ServiceURL + "/po/callback/microsoft",
			Scopes:       []string{"User.Read", "openid", "email"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
				TokenURL: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
			},
		}

		mux.HandleFunc("/po/action/microsoft", common.HandleMicrosoftLogin(microsoftConfig))
		mux.HandleFunc("/po/recover/microsoft", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/po/action/microsoft?intent=recover", http.StatusFound)
		})
		mux.HandleFunc("/po/erase/microsoft", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/po/action/microsoft?intent=erase", http.StatusFound)
		})
		mux.HandleFunc("/po/callback/microsoft", handleMicrosoftCallback(microsoftConfig))
	}

	if settings.AppleClientID != "" {
		supportedProviders["apple"] = true

		appleConfig := &oauth2.Config{
			ClientID:    settings.AppleClientID,
			RedirectURL: settings.ServiceURL + "/po/callback/apple",
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://appleid.apple.com/auth/authorize",
				TokenURL: "https://appleid.apple.com/auth/token",
			},
		}

		mux.HandleFunc("/po/action/apple", common.HandleAppleLogin(appleConfig))
		mux.HandleFunc("/po/recover/apple", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/po/action/apple?intent=recover", http.StatusFound)
		})
		mux.HandleFunc("/po/erase/apple", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/po/action/apple?intent=erase", http.StatusFound)
		})
		mux.HandleFunc("/po/callback/apple", handleAppleCallback())
	}

	mux.HandleFunc("POST /po/register", handleRegister)
	mux.HandleFunc("POST /po/sign", handleSign)
	mux.HandleFunc("DELETE /po/shard", handleDeleteShard)

	server := &http.Server{
		Addr:              "0.0.0.0:" + settings.Port,
		Handler:           cors.AllowAll().Handler(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Info().Str("addr", "http://localhost:"+settings.Port).Msg("operator server listening")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("serve http")
	}
}
