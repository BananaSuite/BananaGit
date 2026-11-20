// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananawiki

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gitea.dev/modules/json"

	"github.com/markbates/goth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

const (
	testClientID = "bwa_abcdefghijklmnopqrstuvwxyz01"
	testSecret   = "s3cret"
	testCallback = "https://git.example.org/user/oauth2/bananawiki/callback"
)

type fakePortal struct {
	server       *httptest.Server
	t            *testing.T
	wantVerifier string
	profile      string
	profileCode  int
	tokenCalls   atomic.Int32
	profileCalls atomic.Int32
}

func newFakePortal(t *testing.T) *fakePortal {
	fp := &fakePortal{t: t, profileCode: http.StatusOK}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", fp.token)
	mux.HandleFunc("GET /oauth/userinfo", fp.userinfo)
	fp.server = httptest.NewServer(mux)
	t.Cleanup(fp.server.Close)
	return fp
}

func (fp *fakePortal) provider(scopes ...string) *Provider {
	u := fp.server.URL
	return NewCustomisedURL(testClientID, testSecret, testCallback, u+"/oauth/authorize", u+"/oauth/token", u+"/oauth/userinfo", scopes...)
}

func (fp *fakePortal) token(w http.ResponseWriter, r *http.Request) {
	fp.tokenCalls.Add(1)
	t := fp.t
	user, pass, ok := r.BasicAuth()
	assert.True(t, ok, "token request must use HTTP Basic client authentication")
	assert.Equal(t, testClientID, user)
	assert.Equal(t, testSecret, pass)
	assert.NoError(t, r.ParseForm())
	assert.Equal(t, "authorization_code", r.PostForm.Get("grant_type"))
	assert.Equal(t, testCallback, r.PostForm.Get("redirect_uri"))
	assert.Empty(t, r.PostForm.Get("client_secret"))
	if r.PostForm.Get("code") != "good-code" || r.PostForm.Get("code_verifier") != fp.wantVerifier {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"access_token":"at-123","token_type":"Bearer","expires_in":3600,"scope":"profile email"}`))
}

func (fp *fakePortal) userinfo(w http.ResponseWriter, r *http.Request) {
	fp.profileCalls.Add(1)
	t := fp.t
	assert.Equal(t, "Bearer at-123", r.Header.Get("Authorization"))
	assert.Empty(t, r.URL.RawQuery, "the access token must never be sent in the query string")
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer at-123" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
		return
	}
	w.WriteHeader(fp.profileCode)
	_, _ = w.Write([]byte(fp.profile))
}

func beginAuth(t *testing.T, p *Provider, state string) *Session {
	sess, err := p.BeginAuth(state)
	require.NoError(t, err)
	s, ok := sess.(*Session)
	require.True(t, ok)
	return s
}

func TestBeginAuth(t *testing.T) {
	p := NewCustomisedURL(testClientID, testSecret, testCallback, "https://portal.example/oauth/authorize", "https://portal.example/oauth/token", "https://portal.example/oauth/userinfo", "email", "extra", "profile", "extra", "")
	s := beginAuth(t, p, "state-xyz")

	authURL, err := s.GetAuthURL()
	require.NoError(t, err)
	u, err := url.Parse(authURL)
	require.NoError(t, err)
	assert.Equal(t, "portal.example", u.Host)
	assert.Equal(t, "/oauth/authorize", u.Path)

	q := u.Query()
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, testClientID, q.Get("client_id"))
	assert.Equal(t, testCallback, q.Get("redirect_uri"))
	assert.Equal(t, "state-xyz", q.Get("state"))
	assert.Equal(t, "profile email extra", q.Get("scope"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))

	require.NotEmpty(t, s.CodeVerifier)
	sum := sha256.Sum256([]byte(s.CodeVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	assert.Equal(t, challenge, q.Get("code_challenge"))
	assert.Len(t, q.Get("code_challenge"), 43)
	assert.NotContains(t, authURL, s.CodeVerifier)

	s2 := beginAuth(t, p, "state-xyz")
	assert.NotEqual(t, s.CodeVerifier, s2.CodeVerifier, "every authorization needs a fresh verifier")
}

func TestDefaults(t *testing.T) {
	p := New(testClientID, testSecret, testCallback)
	assert.Equal(t, "bananawiki", p.Name())
	p.SetName("my-portal")
	assert.Equal(t, "my-portal", p.Name())
	assert.False(t, p.RefreshTokenAvailable())
	_, err := p.RefreshToken("anything")
	require.Error(t, err)

	s := beginAuth(t, p, "st")
	assert.True(t, strings.HasPrefix(s.AuthURL, AuthURL+"?"))
	assert.Equal(t, TokenURL, p.config.Endpoint.TokenURL)
	assert.Equal(t, ProfileURL, p.profileURL)
	assert.Equal(t, oauth2.AuthStyleInHeader, p.config.Endpoint.AuthStyle)
	assert.Equal(t, []string{"profile", "email"}, p.config.Scopes)
}

func TestSessionMarshalRoundTrip(t *testing.T) {
	p := New(testClientID, testSecret, testCallback)
	s := beginAuth(t, p, "st")
	s.AccessToken = "at"
	s.ExpiresAt = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	restored, err := p.UnmarshalSession(s.Marshal())
	require.NoError(t, err)
	assert.Equal(t, s, restored)
	assert.Equal(t, s.Marshal(), s.String())

	_, err = p.UnmarshalSession("not json")
	require.Error(t, err)

	_, err = (&Session{}).GetAuthURL()
	require.Error(t, err)
}

func TestFullFlow(t *testing.T) {
	fp := newFakePortal(t)
	p := fp.provider()
	s := beginAuth(t, p, "st")
	fp.wantVerifier = s.CodeVerifier

	// the session goes through the session store between the redirect and the callback
	restored, err := p.UnmarshalSession(s.Marshal())
	require.NoError(t, err)

	// gothic first tries FetchUser on the stored session: without a token it must fail without a request
	_, err = p.FetchUser(restored)
	require.Error(t, err)
	assert.Zero(t, fp.profileCalls.Load())

	token, err := restored.Authorize(p, url.Values{"code": {"good-code"}, "state": {"st"}})
	require.NoError(t, err)
	assert.Equal(t, "at-123", token)
	assert.EqualValues(t, 1, fp.tokenCalls.Load())

	fp.profile = `{"sub":"3f0c9a","preferred_username":"alice","username":"alice-old","name":"Alice A.",` +
		`"created_at":"2026-01-02 03:04:05","email":"alice@example.org","email_verified":true}`
	user, err := p.FetchUser(restored)
	require.NoError(t, err)
	assert.Equal(t, "bananawiki", user.Provider)
	assert.Equal(t, "3f0c9a", user.UserID)
	assert.Equal(t, "alice", user.NickName)
	assert.Equal(t, "Alice A.", user.Name)
	assert.Equal(t, "alice@example.org", user.Email)
	assert.Equal(t, "at-123", user.AccessToken)
	assert.Empty(t, user.RefreshToken)
	assert.WithinDuration(t, time.Now().Add(time.Hour), user.ExpiresAt, time.Minute)
	assert.Equal(t, "alice", user.RawData["preferred_username"])
	assert.Equal(t, "2026-01-02 03:04:05", user.RawData["created_at"])
}

func TestAuthorizeErrors(t *testing.T) {
	fp := newFakePortal(t)
	p := fp.provider()
	s := beginAuth(t, p, "st")
	fp.wantVerifier = s.CodeVerifier

	_, err := s.Authorize(p, url.Values{})
	require.Error(t, err)
	assert.Zero(t, fp.tokenCalls.Load())

	_, err = s.Authorize(p, url.Values{"code": {"bad-code"}})
	var retrieveErr *oauth2.RetrieveError
	require.ErrorAs(t, err, &retrieveErr)
	assert.Equal(t, "invalid_grant", retrieveErr.ErrorCode)

	// a verifier that does not match the challenge is refused by the portal
	other := beginAuth(t, p, "st")
	_, err = other.Authorize(p, url.Values{"code": {"good-code"}})
	require.ErrorAs(t, err, &retrieveErr)

	_, err = (&Session{}).Authorize(p, url.Values{"code": {"good-code"}})
	require.Error(t, err)

	_, err = s.Authorize(&otherProvider{}, url.Values{"code": {"good-code"}})
	require.Error(t, err)
}

func fetchProfile(t *testing.T, code int, profile string) (goth.User, error) {
	t.Helper()
	fp := newFakePortal(t)
	fp.profileCode = code
	fp.profile = profile
	p := fp.provider()
	return p.FetchUser(&Session{AccessToken: "at-123"})
}

func TestFetchUserProfiles(t *testing.T) {
	t.Run("UnverifiedEmailDropped", func(t *testing.T) {
		user, err := fetchProfile(t, http.StatusOK, `{"sub":"id-1","username":"bob","email":"bob@example.org","email_verified":false}`)
		require.NoError(t, err)
		assert.Equal(t, "id-1", user.UserID)
		assert.Equal(t, "bob", user.NickName, "username is the fallback for preferred_username")
		assert.Empty(t, user.Email)
	})
	t.Run("EmailWithoutVerifiedFlagDropped", func(t *testing.T) {
		user, err := fetchProfile(t, http.StatusOK, `{"sub":"id-1","email":"bob@example.org","email_verified":"true"}`)
		require.NoError(t, err)
		assert.Empty(t, user.Email)
	})
	t.Run("NoEmailScope", func(t *testing.T) {
		user, err := fetchProfile(t, http.StatusOK, `{"sub":"id-2","preferred_username":"carol","name":"carol"}`)
		require.NoError(t, err)
		assert.Equal(t, "id-2", user.UserID)
		assert.Equal(t, "carol", user.NickName)
		assert.Empty(t, user.Email)
	})
	t.Run("MissingSub", func(t *testing.T) {
		_, err := fetchProfile(t, http.StatusOK, `{"preferred_username":"alice","email":"alice@example.org","email_verified":true}`)
		require.ErrorContains(t, err, "sub")
	})
	t.Run("EmptySub", func(t *testing.T) {
		_, err := fetchProfile(t, http.StatusOK, `{"sub":"","preferred_username":"alice"}`)
		require.Error(t, err)
	})
	t.Run("NumericSub", func(t *testing.T) {
		_, err := fetchProfile(t, http.StatusOK, `{"sub":42,"preferred_username":"alice"}`)
		require.Error(t, err)
	})
	t.Run("InvalidJSON", func(t *testing.T) {
		_, err := fetchProfile(t, http.StatusOK, `<html>`)
		require.Error(t, err)
	})
	t.Run("Unauthorized", func(t *testing.T) {
		_, err := fetchProfile(t, http.StatusUnauthorized, `{"error":"invalid_token"}`)
		require.ErrorContains(t, err, "401")
	})
	t.Run("Suspended", func(t *testing.T) {
		_, err := fetchProfile(t, http.StatusForbidden, `{"error":"account_suspended"}`)
		require.ErrorContains(t, err, "403")
	})
	t.Run("TooLarge", func(t *testing.T) {
		big, err := json.Marshal(map[string]string{"sub": "id-3", "pad": strings.Repeat("x", maxProfileSize)})
		require.NoError(t, err)
		_, err = fetchProfile(t, http.StatusOK, string(big))
		require.ErrorContains(t, err, "larger")
	})
}

func TestFetchUserWrongSession(t *testing.T) {
	_, err := New(testClientID, testSecret, testCallback).FetchUser(&otherSession{})
	require.Error(t, err)
}

type otherProvider struct{ Provider }

type otherSession struct{ Session }
