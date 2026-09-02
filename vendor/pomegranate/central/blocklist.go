package main

import (
	"bufio"
	"os"
	"strings"
	"time"
)

var blockedEmails = make(map[string]struct{})

func startBlockedEmailsLoader() {
	if settings.BlockedEmailsFile == "" {
		return
	}

	loadBlockedEmails()
	go func() {
		for range time.NewTicker(30 * time.Minute).C {
			loadBlockedEmails()
		}
	}()
}

func loadBlockedEmails() {
	f, err := os.Open(settings.BlockedEmailsFile)
	if err != nil {
		log.Error().Err(err).Str("path", settings.BlockedEmailsFile).Msg("open blocked emails file")
		return
	}
	defer f.Close()

	emails := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		email := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if email == "" || strings.HasPrefix(email, "#") {
			continue
		}
		emails[email] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		log.Error().Err(err).Str("path", settings.BlockedEmailsFile).Msg("read blocked emails file")
		return
	}

	blockedEmails = emails
	log.Info().Int("count", len(emails)).Str("path", settings.BlockedEmailsFile).Msg("blocked emails loaded")
}
