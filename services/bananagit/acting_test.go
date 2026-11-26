// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananagit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	auth_model "gitea.dev/models/auth"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	bananagit_module "gitea.dev/modules/bananagit"
	"gitea.dev/modules/session"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/services/auth/source/oauth2"
	"gitea.dev/services/contexttest"

	"github.com/markbates/goth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func actingRaw(expires time.Time) map[string]any {
	return map[string]any{
		"sub": "acct-user2", "preferred_username": "user2",
		"act":            map[string]any{"sub": "acct-root", "preferred_username": "root"},
		"act_expires_at": float64(expires.Unix()),
	}
}

func TestActingSignIn(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.BananaGit)()
	source := addHostingSource(t, "https://portal.example/oauth/token")
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	user24 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 24}) // has two-step codes
	soon := time.Now().Add(30 * time.Minute)

	signIn := func(u *user_model.User, raw map[string]any) (*bananagit_module.Acting, int) {
		ctx, resp := contexttest.MockContext(t, "/user/oauth2/BananaWiki/callback", contexttest.MockContextOption{SessionStore: newMemStore(nil)})
		act := CheckActingSignIn(ctx, source, u, goth.User{UserID: "acct-user2", RawData: raw})
		if !ctx.Written() {
			return act, 0
		}
		return act, resp.Code
	}

	t.Run("Ordinary", func(t *testing.T) {
		act, code := signIn(user2, map[string]any{"sub": "acct-user2"})
		assert.Nil(t, act)
		assert.Zero(t, code)
	})
	t.Run("Acting", func(t *testing.T) {
		raw := actingRaw(soon)
		act, code := signIn(user2, raw)
		require.NotNil(t, act)
		assert.Zero(t, code)
		assert.Equal(t, int64(2), act.UID)
		assert.Equal(t, "root", act.AdminName)
		assert.Equal(t, soon.Unix(), act.Until.Unix())
		assert.NotContains(t, raw, "act", "not stored with the link")
		values := WithActing(act, map[string]any{"uid": int64(2)})
		assert.Equal(t, int64(2), values[bananagit_module.SessionActUID])
		assert.Len(t, WithActing(nil, map[string]any{"uid": int64(2)}), 1)
	})
	t.Run("Expired", func(t *testing.T) {
		act, code := signIn(user2, actingRaw(time.Now().Add(-time.Second)))
		assert.Nil(t, act)
		assert.Equal(t, http.StatusForbidden, code)
	})
	t.Run("Disabled", func(t *testing.T) {
		setting.BananaGit.AllowActingAdmins = false
		defer func() { setting.BananaGit.AllowActingAdmins = true }()
		_, code := signIn(user2, actingRaw(soon))
		assert.Equal(t, http.StatusForbidden, code)
	})
	t.Run("AdminTarget", func(t *testing.T) {
		_, code := signIn(admin, actingRaw(soon))
		assert.Equal(t, http.StatusForbidden, code)
	})
	t.Run("Invalid", func(t *testing.T) {
		raw := actingRaw(soon)
		delete(raw, "act_expires_at")
		_, code := signIn(user2, raw)
		assert.Equal(t, http.StatusForbidden, code)
	})
	t.Run("LocalTwoStep", func(t *testing.T) {
		setting.BananaGit.HostingSkipsLocal2FA = true
		act, code := signIn(user24, actingRaw(soon))
		assert.NotNil(t, act, "the portal handles two-step for hosting sign-ins")
		assert.Zero(t, code)

		setting.BananaGit.HostingSkipsLocal2FA = false
		defer func() { setting.BananaGit.HostingSkipsLocal2FA = true }()
		_, code = signIn(user24, actingRaw(soon))
		assert.Equal(t, http.StatusForbidden, code, "an acting administrator cannot pass the owner's own two-step check")
	})
	t.Run("OtherSourcesIgnoreAct", func(t *testing.T) {
		other := &auth_model.Source{Type: auth_model.OAuth2, Name: "Other", Cfg: &oauth2.Source{Provider: "gitea"}}
		ctx, _ := contexttest.MockContext(t, "/", contexttest.MockContextOption{SessionStore: newMemStore(nil)})
		assert.Nil(t, CheckActingSignIn(ctx, other, user2, goth.User{RawData: actingRaw(soon)}))
		assert.False(t, ctx.Written())
	})
}

func TestSkipsLocal2FA(t *testing.T) {
	defer test.MockVariableValue(&setting.BananaGit)()
	hosting := &auth_model.Source{Type: auth_model.OAuth2, Cfg: &oauth2.Source{Provider: HostingProviderName}}
	other := &auth_model.Source{Type: auth_model.OAuth2, Cfg: &oauth2.Source{Provider: "gitea"}}
	setting.BananaGit.HostingSkipsLocal2FA = true
	assert.True(t, SkipsLocal2FA(hosting))
	assert.False(t, SkipsLocal2FA(other), "only BananaWiki Hosting sign-ins")
	assert.False(t, SkipsLocal2FA(&auth_model.Source{Type: auth_model.PAM}), "never password sign-ins")
	assert.False(t, SkipsLocal2FA(nil))
	setting.BananaGit.HostingSkipsLocal2FA = false
	assert.False(t, SkipsLocal2FA(hosting))
}

func TestRefuseLinkWhileActing(t *testing.T) {
	hosting := &auth_model.Source{Type: auth_model.OAuth2, Name: "BananaWiki", Cfg: &oauth2.Source{Provider: HostingProviderName}}
	soon := time.Now().Add(time.Hour)

	ctx, _ := contexttest.MockContext(t, "/user/oauth2/BananaWiki/callback", contexttest.MockContextOption{SessionStore: newMemStore(nil)})
	assert.False(t, RefuseLinkWhileActing(ctx, hosting, goth.User{UserID: "acct-new", RawData: map[string]any{"sub": "acct-new"}}))
	assert.False(t, ctx.Written())

	ctx, resp := contexttest.MockContext(t, "/user/oauth2/BananaWiki/callback", contexttest.MockContextOption{SessionStore: newMemStore(nil)})
	assert.True(t, RefuseLinkWhileActing(ctx, hosting, goth.User{UserID: "acct-new", RawData: actingRaw(soon)}), "an unknown sub while acting")
	assert.Equal(t, http.StatusForbidden, resp.Code)

	acting := (&bananagit_module.Acting{UID: 2, AdminSub: "acct-root", AdminName: "root", Until: soon}).SessionValues()
	acting[session.KeyUID] = int64(2)
	ctx, resp = contexttest.MockContext(t, "/user/oauth2/BananaWiki/callback", contexttest.MockContextOption{SessionStore: newMemStore(acting)})
	assert.True(t, RefuseLinkWhileActing(ctx, hosting, goth.User{UserID: "acct-new", RawData: map[string]any{"sub": "acct-new"}}), "connecting to an acting session")
	assert.Equal(t, http.StatusForbidden, resp.Code)
}

func TestActingGuard(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.BananaGit)()
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

	actingSession := func(until time.Time, uid int64) *memStore {
		values := (&bananagit_module.Acting{UID: 2, AdminSub: "acct-root", AdminName: "root", Until: until}).SessionValues()
		values[session.KeyUID] = uid
		return newMemStore(values)
	}
	guard := func(reqPath string, store *memStore, doer *user_model.User) (*httptest.ResponseRecorder, map[string]any) {
		ctx, resp := contexttest.MockContext(t, reqPath, contexttest.MockContextOption{SessionStore: store})
		ctx.Doer, ctx.IsSigned = doer, doer != nil
		ActingGuard(ctx)
		return resp, ctx.Data
	}
	soon := time.Now().Add(30 * time.Minute)

	t.Run("Banner", func(t *testing.T) {
		resp, data := guard("GET /user/settings/keys", actingSession(soon, 2), user2)
		assert.Equal(t, http.StatusOK, resp.Code, "reading the page is fine")
		view, ok := data["BananaGitActing"].(*ActingView)
		require.True(t, ok)
		assert.Equal(t, "root", view.AdminName)
		assert.Equal(t, "user2", view.UserName)
	})
	t.Run("RefusedActions", func(t *testing.T) {
		for _, p := range []string{
			"POST /user/settings/keys", "POST /user/settings/account", "POST /user/settings/account/email",
			"POST /user/settings/account/delete", "POST /user/settings/security/two_factor/disable",
			"POST /user/settings/security/account_link", "POST /user/settings/applications",
			"GET /user/oauth2/BananaWiki", "POST /user2/repo1/settings/keys",
		} {
			store := actingSession(soon, 2)
			resp, _ := guard(p, store, user2)
			assert.Equal(t, http.StatusSeeOther, resp.Code, p)
			assert.Equal(t, "/user/settings", test.RedirectURL(resp), p)
			assert.False(t, store.destroyed, p)
		}
	})
	t.Run("Expiry", func(t *testing.T) {
		store := actingSession(time.Now().Add(-time.Second), 2)
		resp, data := guard("GET /", store, user2)
		assert.Equal(t, http.StatusSeeOther, resp.Code)
		assert.Equal(t, "/", test.RedirectURL(resp))
		assert.True(t, store.destroyed, "the session ends at act_expires_at")
		assert.Nil(t, data["BananaGitActing"])
	})
	t.Run("SettingTurnedOff", func(t *testing.T) {
		setting.BananaGit.AllowActingAdmins = false
		defer func() { setting.BananaGit.AllowActingAdmins = true }()
		store := actingSession(soon, 2)
		guard("GET /", store, user2)
		assert.True(t, store.destroyed)
	})
	t.Run("BecameAdmin", func(t *testing.T) {
		store := actingSession(soon, 1)
		store.data[bananagit_module.SessionActUID] = int64(1)
		guard("GET /", store, admin)
		assert.True(t, store.destroyed)
	})
	t.Run("StaleKeysOfAnotherSignIn", func(t *testing.T) {
		store := actingSession(soon, 5) // signed in as user5 since, with a password
		resp, data := guard("POST /user/settings/keys", store, &user_model.User{ID: 5, Name: "user5"})
		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Nil(t, data["BananaGitActing"])
		assert.Nil(t, store.Get(bananagit_module.SessionActUID), "the stale acting keys are removed")
		assert.False(t, store.destroyed)
	})
	t.Run("NotActing", func(t *testing.T) {
		resp, data := guard("POST /user/settings/keys", newMemStore(map[string]any{session.KeyUID: int64(2)}), user2)
		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Nil(t, data["BananaGitActing"])
	})
}
