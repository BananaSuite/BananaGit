// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananawiki

import (
	"errors"
	"time"

	"gitea.dev/modules/json"

	"github.com/markbates/goth"
	"golang.org/x/oauth2"
)

// Session stores the state of an authorization in progress, including the
// PKCE verifier, which never leaves the server.
type Session struct {
	AuthURL      string
	CodeVerifier string
	AccessToken  string
	ExpiresAt    time.Time
}

var _ goth.Session = &Session{}

// GetAuthURL returns the URL the user is sent to.
func (s *Session) GetAuthURL() (string, error) {
	if s.AuthURL == "" {
		return "", errors.New(goth.NoAuthUrlErrorMessage)
	}
	return s.AuthURL, nil
}

// Authorize exchanges the authorization code for an access token.
func (s *Session) Authorize(provider goth.Provider, params goth.Params) (string, error) {
	p, ok := provider.(*Provider)
	if !ok {
		return "", errors.New("bananawiki: unexpected provider type")
	}
	code := params.Get("code")
	if code == "" {
		return "", errors.New("bananawiki: no authorization code in callback")
	}
	if s.CodeVerifier == "" {
		return "", errors.New("bananawiki: no PKCE verifier in session")
	}
	token, err := p.config.Exchange(goth.ContextForClient(p.Client()), code, oauth2.VerifierOption(s.CodeVerifier))
	if err != nil {
		return "", err
	}
	if !token.Valid() {
		return "", errors.New("bananawiki: invalid token received from provider")
	}
	s.AccessToken = token.AccessToken
	s.ExpiresAt = token.Expiry
	return token.AccessToken, nil
}

// Marshal serializes the session for the session store.
func (s *Session) Marshal() string {
	b, _ := json.Marshal(s)
	return string(b)
}

// String returns the marshalled session.
func (s *Session) String() string {
	return s.Marshal()
}
