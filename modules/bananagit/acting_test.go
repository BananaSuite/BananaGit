// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananagit

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActingRefuses(t *testing.T) {
	refused := map[string][]string{
		http.MethodPost: {
			"/user/settings/account", "/user/settings/account/email", "/user/settings/account/email/delete",
			"/user/settings/account/delete", "/user/settings/change_password",
			"/user/settings/security/two_factor/enroll", "/user/settings/security/two_factor/disable",
			"/user/settings/security/two_factor/regenerate_scratch", "/user/settings/security/webauthn/register",
			"/user/settings/security/webauthn/delete", "/user/settings/security/openid",
			"/user/settings/security/account_link", "/user/settings/applications",
			"/user/settings/applications/delete", "/user/settings/applications/oauth2",
			"/user/settings/applications/oauth2/3/regenerate_secret", "/user/settings/applications/oauth2/3/revoke/4",
			"/user/settings/keys", "/user/settings/keys/delete", "/user/settings/packages/chef/regenerate_keypair",
			"/user/link_account_signin", "/user/link_account_signup", "/login/oauth/grant",
			"/alice/repo/settings/keys", "/alice/repo/settings/keys/delete",
			// spelled differently, still refused
			"/user/settings//keys", "/User/Settings/Keys", "/user/settings/x/../keys",
		},
		http.MethodGet: {
			"/user/settings/security/two_factor/enroll", "/user/settings/change_password", "/user/oauth2/BananaWiki",
			"/user/oauth2/BananaWiki/callback", "/user/link_account", "/login/oauth/authorize",
		},
	}
	allowed := map[string][]string{
		http.MethodGet: {
			"/", "/user/settings", "/user/settings/account", "/user/settings/security", "/user/settings/applications",
			"/user/settings/keys", "/user/settings/applications/oauth2/3", "/alice/repo/settings/keys", "/alice/repo",
			"/user/settings/keysmith",
		},
		http.MethodPost: {
			"/user/settings", "/user/settings/appearance/theme", "/user/logout", "/alice/repo/issues/new",
			"/user/settings/accounts", "/alice/repo/settings", "/-/bananagit/eggs/x/found",
		},
	}
	for method, paths := range refused {
		for _, p := range paths {
			assert.True(t, ActingRefuses(method, p), "%s %s must be refused", method, p)
		}
	}
	for method, paths := range allowed {
		for _, p := range paths {
			assert.False(t, ActingRefuses(method, p), "%s %s must be allowed", method, p)
		}
	}
	assert.True(t, ActingRefuses(http.MethodHead, "/user/settings/security/two_factor/enroll"))
	assert.False(t, ActingRefuses(http.MethodHead, "/user/settings/keys"))
	assert.True(t, ActingRefuses(http.MethodPut, "/user/settings/keys"))
}

func TestActingSession(t *testing.T) {
	now := time.Unix(1795000000, 0)
	a := &Acting{UID: 7, AdminSub: "acct-root", AdminName: "root", Until: now.Add(30 * time.Minute)}
	store := a.SessionValues()
	got, ok := ActingFromSession(func(k string) any { return store[k] })
	require.True(t, ok)
	assert.Equal(t, a.UID, got.UID)
	assert.Equal(t, a.AdminName, got.AdminName)
	assert.Equal(t, a.Until.Unix(), got.Until.Unix())
	assert.False(t, got.Expired(now))
	assert.False(t, got.Expired(now.Add(29*time.Minute)))
	assert.True(t, got.Expired(now.Add(30*time.Minute)), "the session ends at act_expires_at")

	_, ok = ActingFromSession(func(string) any { return nil })
	assert.False(t, ok)

	damaged, ok := ActingFromSession(func(k string) any {
		if k == SessionActUID {
			return int64(7)
		}
		return "garbage"
	})
	require.True(t, ok, "damaged values still count as acting")
	assert.True(t, damaged.Expired(now), "and end the session")

	assert.Equal(t, now.Add(MaxActing), ClampUntil(now.Add(48*time.Hour), now))
	assert.Equal(t, now.Add(time.Minute), ClampUntil(now.Add(time.Minute), now))
	assert.ElementsMatch(t, SessionActKeys, keys(store))
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
