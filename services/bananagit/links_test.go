// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananagit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	auth_model "gitea.dev/models/auth"
	"gitea.dev/models/db"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	bananagit_module "gitea.dev/modules/bananagit"
	"gitea.dev/modules/json"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePortal answers the account lookup from a fixed list.

type fakePortal struct {
	srv      *httptest.Server
	mu       sync.Mutex
	accounts []map[string]any
	calls    int
}

func newFakePortal(t *testing.T) *fakePortal {
	fp := &fakePortal{}
	fp.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fp.mu.Lock()
		defer fp.mu.Unlock()
		fp.calls++
		if r.URL.Path != "/api/v1/admin/accounts/lookup" || r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var req struct {
			Usernames []string `json:"usernames"`
			Emails    []string `json:"emails"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var found []map[string]any
		for _, a := range fp.accounts {
			for _, u := range req.Usernames {
				if strings.EqualFold(u, a["username"].(string)) {
					found = append(found, a)
				}
			}
			for _, e := range req.Emails {
				if email, _ := a["email"].(string); email != "" && strings.EqualFold(e, email) {
					found = append(found, a)
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"accounts": found, "not_found": []string{}})
	}))
	t.Cleanup(fp.srv.Close)
	return fp
}

func link(t *testing.T, source *auth_model.Source, uid int64, sub string) {
	require.NoError(t, user_model.LinkExternalToUser(t.Context(), &user_model.User{ID: uid}, &user_model.ExternalLoginUser{
		ExternalID: sub, UserID: uid, LoginSourceID: source.ID, Provider: source.Name,
	}))
}

func linkOf(t *testing.T, source *auth_model.Source, sub string) *user_model.ExternalLoginUser {
	l, has, err := user_model.GetExternalLogin(t.Context(), source.ID, sub)
	require.NoError(t, err)
	if !has {
		return nil
	}
	return l
}

func rowsByName(p *Preview) map[string]bananagit_module.Row {
	out := map[string]bananagit_module.Row{}
	for _, r := range p.Rows {
		out[r.LocalName] = r
	}
	return out
}

func TestAccountLinks(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.BananaGit.PortalLookupToken, "test-token")()

	fp := newFakePortal(t)
	fp.accounts = []map[string]any{
		{"sub": "acct-user1", "username": "user1", "status": "active"},
		{"sub": "acct-user2", "username": "User2", "email": "user2@example.com", "email_verified": true, "status": "active"},
		{"sub": "acct-user4", "username": "user4", "status": "suspended"},
		{"sub": "acct-user5", "username": "user5", "status": "active"},
		{"sub": "acct-user8", "username": "user8", "status": "pending"},
		{"sub": "acct-user10", "username": "user10", "status": "active"},
	}
	source := addHostingSource(t, fp.srv.URL+"/oauth/token")
	ctx := t.Context()
	found, err := HostingSource(ctx)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, source.ID, found.ID)

	portal, err := PortalURL(found)
	require.NoError(t, err)
	assert.Equal(t, fp.srv.URL, portal)

	// user5 is linked to another hosting account, acct-user10 is linked to user11
	link(t, source, 5, "acct-old-user5")
	link(t, source, 11, "acct-user10")

	before, beforeAdmins, err := UnlinkedCounts(ctx, source)
	require.NoError(t, err)
	assert.Positive(t, before)
	assert.Positive(t, beforeAdmins, "user1 is an unlinked administrator")

	req := bananagit_module.Request{Mode: bananagit_module.ModeUsername}
	preview, err := ComputePreview(ctx, source, req)
	require.NoError(t, err)
	rows := rowsByName(preview)
	assert.Equal(t, bananagit_module.StateSkipped, rows["user1"].State, "administrators are skipped")
	assert.Equal(t, bananagit_module.StateWillLink, rows["user2"].State)
	assert.Equal(t, "acct-user2", rows["user2"].Sub)
	assert.Equal(t, bananagit_module.ReasonSuspended, rows["user4"].Reason)
	assert.Equal(t, bananagit_module.ReasonLocalLinkedOther, rows["user5"].Reason)
	assert.Equal(t, bananagit_module.ReasonPending, rows["user8"].Reason)
	assert.Equal(t, bananagit_module.ReasonHostingLinkedOther, rows["user10"].Reason)
	assert.Equal(t, "user11", rows["user10"].Other)
	assert.Equal(t, bananagit_module.StateAlreadyLinked, rows["user11"].State)
	_, hasOrg := rows["org3"]
	assert.False(t, hasOrg, "organizations are not users")
	assert.Equal(t, 1, preview.Counts[bananagit_module.StateWillLink])

	t.Run("StalePreview", func(t *testing.T) {
		// the hosting account becomes suspended between preview and apply
		fp.mu.Lock()
		fp.accounts[1]["status"] = "suspended"
		fp.mu.Unlock()
		_, fresh, err := Apply(ctx, &user_model.User{Name: "user1"}, source, req, preview.Token)
		require.ErrorIs(t, err, ErrStalePreview)
		assert.Equal(t, bananagit_module.ReasonSuspended, rowsByName(fresh)["user2"].Reason)
		assert.Nil(t, linkOf(t, source, "acct-user2"))
		fp.mu.Lock()
		fp.accounts[1]["status"] = "active"
		fp.mu.Unlock()

		_, _, err = Apply(ctx, &user_model.User{Name: "user1"}, source, req, "")
		require.ErrorIs(t, err, ErrStalePreview, "no token, no apply")
	})

	t.Run("Apply", func(t *testing.T) {
		res, _, err := Apply(ctx, &user_model.User{Name: "user1"}, source, req, preview.Token)
		require.NoError(t, err)
		require.Len(t, res.Linked, 1)
		assert.Empty(t, res.Failed)
		got := linkOf(t, source, "acct-user2")
		require.NotNil(t, got)
		assert.EqualValues(t, 2, got.UserID)
		assert.Equal(t, "User2", got.NickName)
		assert.Equal(t, "BananaWiki", got.Provider)

		// the same preview cannot be applied twice: it changed
		_, _, err = Apply(ctx, &user_model.User{Name: "user1"}, source, req, preview.Token)
		require.ErrorIs(t, err, ErrStalePreview)

		after, _, err := UnlinkedCounts(ctx, source)
		require.NoError(t, err)
		assert.Equal(t, before-1, after)
	})

	t.Run("ByEmailAndList", func(t *testing.T) {
		p, err := ComputePreview(ctx, source, bananagit_module.Request{Mode: bananagit_module.ModeEmail})
		require.NoError(t, err)
		assert.Equal(t, bananagit_module.StateAlreadyLinked, rowsByName(p)["user2"].State)

		listReq := bananagit_module.Request{Mode: bananagit_module.ModeList, ListKind: bananagit_module.ListBySub, List: "user1,acct-root\nuser9,acct-nine\nnobody,acct-x", IncludeAdmins: true}
		p, err = ComputePreview(ctx, source, listReq)
		require.NoError(t, err)
		rows := rowsByName(p)
		assert.Equal(t, bananagit_module.StateWillLink, rows["user1"].State)
		assert.Equal(t, bananagit_module.ReasonUnchecked, rows["user1"].Reason)
		assert.Equal(t, bananagit_module.ReasonUnknownLocal, rows["nobody"].Reason)
	})

	t.Run("NoToken", func(t *testing.T) {
		defer test.MockVariableValue(&setting.BananaGit.PortalLookupToken, "")()
		_, err := ComputePreview(ctx, source, bananagit_module.Request{Mode: bananagit_module.ModeUsername})
		require.ErrorIs(t, err, ErrNoLookup)
		_, err = ComputePreview(ctx, source, bananagit_module.Request{Mode: bananagit_module.ModeList, ListKind: bananagit_module.ListByUsername, List: "a,b"})
		require.ErrorIs(t, err, ErrNoLookup)
		calls := fp.calls
		_, err = ComputePreview(ctx, source, bananagit_module.Request{Mode: bananagit_module.ModeList, ListKind: bananagit_module.ListBySub, List: "user2,acct-user2"})
		require.NoError(t, err, "a sub entered by hand needs no lookup")
		assert.Equal(t, calls, fp.calls)
	})

	t.Run("Unlink", func(t *testing.T) {
		u, err := Unlink(ctx, &user_model.User{Name: "user1"}, source, 2)
		require.NoError(t, err)
		assert.Equal(t, "user2", u.Name)
		assert.Nil(t, linkOf(t, source, "acct-user2"))

		_, err = Unlink(ctx, &user_model.User{Name: "user1"}, source, 2)
		require.ErrorIs(t, err, ErrNotLinked)

		// an account created by signing in with the source keeps its link
		_, err = db.GetEngine(ctx).ID(4).Cols("login_type", "login_source", "login_name").Update(&user_model.User{LoginType: auth_model.OAuth2, LoginSource: source.ID, LoginName: "acct-user4"})
		require.NoError(t, err)
		_, err = Unlink(ctx, &user_model.User{Name: "user1"}, source, 4)
		require.ErrorIs(t, err, ErrCreatedHere)

		list, _, err := ListUsers(ctx, source, ListOptions{Keyword: "user4", PageSize: 10, Page: 1})
		require.NoError(t, err)
		require.NotEmpty(t, list)
		assert.True(t, list[0].SignedUp)
		assert.Equal(t, "acct-user4", list[0].Sub)

		unlinked, _, err := ListUsers(ctx, source, ListOptions{UnlinkedOnly: true, PageSize: 100, Page: 1})
		require.NoError(t, err)
		for _, ul := range unlinked {
			assert.NotContains(t, []string{"user4", "user5", "user11"}, ul.User.Name)
		}
	})
}
