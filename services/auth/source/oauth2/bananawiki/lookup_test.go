// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananawiki

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gitea.dev/modules/json"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortalBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		TokenURL:                         "https://hosting.bananawiki.com",
		"http://portal:8000/oauth/token": "http://portal:8000",
		"https://example.org/hosting/oauth/token/":  "https://example.org/hosting",
		"https://example.org/something/else/token":  "https://example.org/something/else/token",
		"https://example.org/oauth/other/oauth/abc": "https://example.org",
	} {
		got, err := PortalBaseURL(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "ftp://x/oauth/token", "https://u:p@x/oauth/token", "/oauth/token"} {
		_, err := PortalBaseURL(in)
		assert.Error(t, err, in)
	}
}

func TestLookup(t *testing.T) {
	var calls atomic.Int32
	var status atomic.Int32
	status.Store(http.StatusOK)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, LookupPath, r.URL.Path)
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		if code := int(status.Load()); code != http.StatusOK {
			w.WriteHeader(code)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Usernames []string `json:"usernames"`
			Emails    []string `json:"emails"`
		}
		require.NoError(t, json.Unmarshal(body, &req))
		assert.LessOrEqual(t, len(req.Usernames)+len(req.Emails), LookupMaxEntries)
		out := map[string]any{"not_found": []string{}}
		accounts := []map[string]any{}
		for _, u := range req.Usernames {
			switch u {
			case "alice":
				accounts = append(accounts, map[string]any{"sub": "acct-alice", "username": "Alice", "email": "alice@example.org", "email_verified": true, "status": "Active"})
			case "bob":
				accounts = append(accounts, map[string]any{"sub": "acct-bob", "username": "bob", "email": "bob@example.org", "email_verified": false, "status": "suspended"})
			case "evil":
				accounts = append(accounts, map[string]any{"sub": "has space", "username": "evil", "status": "active"})
			default:
				out["not_found"] = append(out["not_found"].([]string), u)
			}
		}
		for _, e := range req.Emails {
			if e == "alice@example.org" {
				accounts = append(accounts, map[string]any{"sub": "acct-alice", "username": "Alice", "email": "alice@example.org", "email_verified": true, "status": "active"})
			}
		}
		out["accounts"] = accounts
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()

	c := &LookupClient{BaseURL: srv.URL + "/", Token: "tok"}
	res, err := c.Lookup(context.Background(), []string{"alice", "bob", "evil", "nobody"}, []string{"alice@example.org"})
	require.NoError(t, err)
	require.Len(t, res.Accounts, 2, "duplicates merged, invalid sub dropped")
	assert.Equal(t, LookupAccount{Sub: "acct-alice", Username: "Alice", Email: "alice@example.org", EmailVerified: true, Status: StatusActive}, res.Accounts[0])
	assert.Empty(t, res.Accounts[1].Email, "unverified email dropped")
	assert.Equal(t, StatusSuspended, res.Accounts[1].Status)
	assert.Equal(t, []string{"nobody"}, res.NotFound)
	assert.EqualValues(t, 1, calls.Load())

	many := make([]string, 1200)
	for i := range many {
		many[i] = "user" + strings.Repeat("x", i%3)
	}
	calls.Store(0)
	_, err = c.Lookup(context.Background(), many, []string{"a@example.org"})
	require.NoError(t, err)
	assert.EqualValues(t, 3, calls.Load(), "batches of at most 500 entries")

	status.Store(http.StatusForbidden)
	_, err = c.Lookup(context.Background(), []string{"alice"}, nil)
	require.ErrorIs(t, err, ErrLookupRefused)
	status.Store(http.StatusTooManyRequests)
	_, err = c.Lookup(context.Background(), []string{"alice"}, nil)
	require.ErrorIs(t, err, ErrLookupRefused)
	status.Store(http.StatusInternalServerError)
	_, err = c.Lookup(context.Background(), []string{"alice"}, nil)
	require.Error(t, err)

	_, err = (&LookupClient{BaseURL: srv.URL}).Lookup(context.Background(), []string{"alice"}, nil)
	require.Error(t, err, "no token, no request")
}

func TestLookupDoesNotFollowRedirects(t *testing.T) {
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(true)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+LookupPath, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	_, err := (&LookupClient{BaseURL: srv.URL, Token: "tok"}).Lookup(context.Background(), []string{"alice"}, nil)
	require.Error(t, err)
	assert.False(t, leaked.Load())
}
