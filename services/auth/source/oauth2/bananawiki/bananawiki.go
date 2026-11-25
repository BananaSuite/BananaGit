// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package bananawiki implements a goth provider for BananaWiki Hosting, a plain
// OAuth 2.0 provider (no OpenID Connect) that requires PKCE S256 and accepts
// access tokens only in the Authorization header.
package bananawiki

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"

	"gitea.dev/modules/json"

	"github.com/markbates/goth"
	"golang.org/x/oauth2"
)

// Default endpoints of the production portal.
const (
	AuthURL    = "https://hosting.bananawiki.com/oauth/authorize"
	TokenURL   = "https://hosting.bananawiki.com/oauth/token"
	ProfileURL = "https://hosting.bananawiki.com/oauth/userinfo"
)

const maxProfileSize = 1 << 20

// Provider is the goth provider for BananaWiki Hosting.
type Provider struct {
	ClientKey    string
	Secret       string
	CallbackURL  string
	HTTPClient   *http.Client
	config       *oauth2.Config
	providerName string
	profileURL   string
}

var _ goth.Provider = &Provider{}

// New creates a provider for the production portal.
func New(clientKey, secret, callbackURL string, scopes ...string) *Provider {
	return NewCustomisedURL(clientKey, secret, callbackURL, AuthURL, TokenURL, ProfileURL, scopes...)
}

// NewCustomisedURL creates a provider for a portal at custom URLs.
func NewCustomisedURL(clientKey, secret, callbackURL, authURL, tokenURL, profileURL string, scopes ...string) *Provider {
	p := &Provider{
		ClientKey:    clientKey,
		Secret:       secret,
		CallbackURL:  callbackURL,
		providerName: "bananawiki",
		profileURL:   profileURL,
	}
	p.config = &oauth2.Config{
		ClientID:     clientKey,
		ClientSecret: secret,
		RedirectURL:  callbackURL,
		Endpoint: oauth2.Endpoint{
			AuthURL:   authURL,
			TokenURL:  tokenURL,
			AuthStyle: oauth2.AuthStyleInHeader,
		},
		Scopes: buildScopes(scopes),
	}
	return p
}

// buildScopes always asks for "profile" and "email" (needed for auto-registration), plus any configured scope.
func buildScopes(extra []string) []string {
	scopes := []string{"profile", "email"}
	for _, scope := range extra {
		if scope != "" && !slices.Contains(scopes, scope) {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

// Name is the name used to retrieve this provider later.
func (p *Provider) Name() string {
	return p.providerName
}

// SetName is to update the name of the provider (needed in case of multiple providers of 1 type).
func (p *Provider) SetName(name string) {
	p.providerName = name
}

// Client returns the HTTP client used to talk to the portal.
func (p *Provider) Client() *http.Client {
	return goth.HTTPClientWithFallBack(p.HTTPClient)
}

// Debug is a no-op for this provider.
func (p *Provider) Debug(bool) {}

// BeginAuth returns the authorization URL with a fresh PKCE S256 challenge.
func (p *Provider) BeginAuth(state string) (goth.Session, error) {
	verifier := oauth2.GenerateVerifier()
	return &Session{
		AuthURL:      p.config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)),
		CodeVerifier: verifier,
	}, nil
}

// UnmarshalSession restores a session marshalled by Session.Marshal.
func (p *Provider) UnmarshalSession(data string) (goth.Session, error) {
	s := &Session{}
	if err := json.Unmarshal([]byte(data), s); err != nil {
		return nil, err
	}
	return s, nil
}

// FetchUser reads the profile of the signed-in account from the userinfo endpoint.
func (p *Provider) FetchUser(session goth.Session) (goth.User, error) {
	sess, ok := session.(*Session)
	if !ok {
		return goth.User{}, errors.New("bananawiki: unexpected session type")
	}
	user := goth.User{
		Provider:    p.Name(),
		AccessToken: sess.AccessToken,
		ExpiresAt:   sess.ExpiresAt,
	}
	if user.AccessToken == "" {
		// gothic calls FetchUser before Authorize to reuse a completed session
		return user, fmt.Errorf("%s cannot get user information without accessToken", p.providerName)
	}

	req, err := http.NewRequest(http.MethodGet, p.profileURL, nil)
	if err != nil {
		return user, err
	}
	req.Header.Set("Authorization", "Bearer "+sess.AccessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := p.Client().Do(req)
	if err != nil {
		return user, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return user, fmt.Errorf("%s responded with a %d trying to fetch user information", p.providerName, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProfileSize+1))
	if err != nil {
		return user, err
	}
	if len(body) > maxProfileSize {
		return user, fmt.Errorf("%s user information is larger than %d bytes", p.providerName, maxProfileSize)
	}
	if err := userFromReader(body, &user); err != nil {
		return user, err
	}
	return user, nil
}

func userFromReader(body []byte, user *goth.User) error {
	raw := map[string]any{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("bananawiki: invalid user information: %w", err)
	}

	sub, _ := raw["sub"].(string)
	if sub == "" {
		return errors.New("bananawiki: user information has no sub")
	}
	user.RawData = raw
	user.UserID = sub
	user.Name, _ = raw["name"].(string)
	user.NickName, _ = raw["preferred_username"].(string)
	if user.NickName == "" {
		user.NickName, _ = raw["username"].(string)
	}
	if verified, _ := raw["email_verified"].(bool); verified {
		user.Email, _ = raw["email"].(string)
	}
	if _, err := ActingFromRawData(raw); err != nil {
		return err // an incomplete "act" must never pass for an ordinary sign-in
	}
	return nil
}

// RefreshTokenAvailable reports false: the portal issues no refresh tokens.
func (p *Provider) RefreshTokenAvailable() bool {
	return false
}

// RefreshToken is not supported by the portal.
func (p *Provider) RefreshToken(string) (*oauth2.Token, error) {
	return nil, errors.New("bananawiki: refresh tokens are not supported")
}
