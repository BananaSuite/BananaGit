// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananagit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseList(t *testing.T) {
	entries, err := ParseList("local_username,hosting_username\n\n# comment\nalice, Alice\nbob;bobby\n\"carol\"\t\"carol\"\nbroken\n,x\n", ListByUsername)
	require.NoError(t, err)
	require.Len(t, entries, 5)
	assert.Equal(t, ListEntry{Line: 4, Local: "alice", Target: "Alice"}, entries[0])
	assert.Equal(t, ListEntry{Line: 5, Local: "bob", Target: "bobby"}, entries[1])
	assert.Equal(t, ListEntry{Line: 6, Local: "carol", Target: "carol"}, entries[2])
	assert.True(t, entries[3].Err)
	assert.Equal(t, "broken", entries[3].Local)
	assert.True(t, entries[4].Err)

	entries, err = ParseList("alice,acct-1\nbob,has space\n", ListBySub)
	require.NoError(t, err)
	assert.False(t, entries[0].Err)
	assert.True(t, entries[1].Err, "not a valid sub")

	_, err = ParseList(strings.Repeat("a,b\n", MaxListRows+1), ListByUsername)
	require.ErrorIs(t, err, ErrListTooLong)
	_, err = ParseList(strings.Repeat("a,b\n", MaxListRows), ListByUsername)
	require.NoError(t, err)
}

func TestRequestNormalize(t *testing.T) {
	r := Request{Mode: " Username ", List: "x", ListKind: "sub"}
	require.NoError(t, r.Normalize())
	assert.Equal(t, Request{Mode: ModeUsername}, r)
	r = Request{Mode: ModeList, ListKind: ListBySub, List: "a,b\r\n"}
	require.NoError(t, r.Normalize())
	assert.Equal(t, "a,b", r.List)
	require.Error(t, (&Request{Mode: ModeList, ListKind: "x", List: "a,b"}).Normalize())
	require.Error(t, (&Request{Mode: ModeList, ListKind: ListBySub, List: " "}).Normalize())
	require.Error(t, (&Request{Mode: "all"}).Normalize())
}

// planner fixtures: local users and portal accounts

var (
	users = []LocalUser{
		{ID: 1, Name: "root", Email: "root@example.org", IsAdmin: true},
		{ID: 2, Name: "alice", Email: "alice@example.org"},
		{ID: 3, Name: "Bob", Email: "bob@example.org"},
		{ID: 4, Name: "carol", Email: "carol@example.org", LinkedSub: "acct-carol"},
		{ID: 5, Name: "dave", Email: "dave@example.org", LinkedSub: "acct-old-dave"},
		{ID: 6, Name: "erin", Email: "", Disabled: true},
		{ID: 7, Name: "frank", Email: "frank@example.org"},
		{ID: 8, Name: "grace", Email: "grace@example.org"},
		{ID: 9, Name: "heidi", Email: ""},
		{ID: 10, Name: "ivan", Email: "ivan@example.org"},
		{ID: 11, Name: "judy", Email: "judy@example.org"},
	}
	accounts = []Account{
		{Sub: "acct-root", Username: "root", Email: "root@example.org", Status: "active"},
		{Sub: "acct-alice", Username: "alice", Email: "alice@example.org", Status: "active"},
		{Sub: "acct-bob", Username: "bob", Email: "bob@example.org", Status: "active"},
		{Sub: "acct-carol", Username: "carol", Email: "carol@example.org", Status: "active"},
		{Sub: "acct-dave", Username: "dave", Email: "dave@example.org", Status: "active"},
		{Sub: "acct-frank", Username: "frank", Email: "frank@example.org", Status: "suspended"},
		{Sub: "acct-grace", Username: "grace", Email: "grace@example.org", Status: "pending"},
		{Sub: "acct-heidi", Username: "heidi", Status: "active"},
		{Sub: "acct-ivan", Username: "ivan", Email: "ivan.other@example.org", Status: "active"},
		{Sub: "acct-ivan2", Username: "ivan2", Email: "ivan@example.org", Status: "active"},
		{Sub: "acct-judy", Username: "judy", Email: "judy@example.org", Status: "active"},
	}
	linkOwner = map[string]int64{"acct-carol": 4, "acct-old-dave": 5, "acct-judy": 99}
	userNames = map[int64]string{4: "carol", 5: "dave", 99: "zed"}
)

func plan(t *testing.T, req Request, in Input) map[string]Row {
	t.Helper()
	require.NoError(t, req.Normalize())
	if in.Users == nil {
		in.Users = users
	}
	if in.Accounts == nil {
		in.Accounts = accounts
	}
	if req.Mode == ModeList && in.Entries == nil {
		entries, err := ParseList(req.List, req.ListKind)
		require.NoError(t, err)
		in.Entries = entries
	}
	in.LinkOwner, in.UserNames = linkOwner, userNames
	out := map[string]Row{}
	for _, r := range Plan(req, in) {
		key := r.LocalName
		if r.Line > 0 {
			key = r.LocalName + "#" + string(rune('0'+r.Line))
		}
		out[key] = r
	}
	return out
}

func assertRow(t *testing.T, rows map[string]Row, key, state, reason string) Row {
	t.Helper()
	r, ok := rows[key]
	require.True(t, ok, "row %s", key)
	assert.Equal(t, state, r.State, "state of %s", key)
	assert.Equal(t, reason, r.Reason, "reason of %s", key)
	return r
}

func TestPlanByUsername(t *testing.T) {
	rows := plan(t, Request{Mode: ModeUsername}, Input{})
	assertRow(t, rows, "root", StateSkipped, ReasonAdmin)
	r := assertRow(t, rows, "alice", StateWillLink, "")
	assert.Equal(t, "acct-alice", r.Sub)
	assert.Equal(t, int64(2), r.UserID)
	assertRow(t, rows, "Bob", StateWillLink, "") // case-insensitive
	assertRow(t, rows, "carol", StateAlreadyLinked, "")
	r = assertRow(t, rows, "dave", StateConflict, ReasonLocalLinkedOther)
	assert.Equal(t, "acct-old-dave", r.Other)
	assertRow(t, rows, "erin", StateSkipped, ReasonDisabled)
	assertRow(t, rows, "frank", StateConflict, ReasonSuspended)
	assertRow(t, rows, "grace", StateConflict, ReasonPending)
	assertRow(t, rows, "heidi", StateWillLink, "")
	r = assertRow(t, rows, "judy", StateConflict, ReasonHostingLinkedOther)
	assert.Equal(t, "zed", r.Other)

	rows = plan(t, Request{Mode: ModeUsername, IncludeAdmins: true}, Input{})
	assertRow(t, rows, "root", StateWillLink, "")

	rows = plan(t, Request{Mode: ModeUsername}, Input{Accounts: []Account{}})
	assertRow(t, rows, "alice", StateNotFound, ReasonNoMatch)
	assertRow(t, rows, "carol", StateAlreadyLinked, "")

	// the portal answering two accounts for one name
	rows = plan(t, Request{Mode: ModeUsername}, Input{Accounts: []Account{
		{Sub: "a1", Username: "alice", Status: "active"}, {Sub: "a2", Username: "ALICE", Status: "active"},
	}})
	assertRow(t, rows, "alice", StateConflict, ReasonSeveral)
}

func TestPlanByEmail(t *testing.T) {
	rows := plan(t, Request{Mode: ModeEmail}, Input{})
	assertRow(t, rows, "alice", StateWillLink, "")
	assertRow(t, rows, "heidi", StateNotFound, ReasonNoLocalEmail)
	r := assertRow(t, rows, "ivan", StateWillLink, "")
	assert.Equal(t, "acct-ivan2", r.Sub, "by email, whatever the username")
}

func TestPlanByBoth(t *testing.T) {
	rows := plan(t, Request{Mode: ModeBoth}, Input{})
	assertRow(t, rows, "alice", StateWillLink, "")
	assertRow(t, rows, "ivan", StateConflict, ReasonSeveral) // username and email point to different accounts
	assertRow(t, rows, "heidi", StateNotFound, ReasonNoLocalEmail)

	rows = plan(t, Request{Mode: ModeBoth}, Input{Accounts: []Account{
		{Sub: "acct-alice", Username: "alice", Email: "", Status: "active"},
		{Sub: "acct-x", Username: "someone", Email: "bob@example.org", Status: "active"},
	}})
	assertRow(t, rows, "alice", StateNotFound, ReasonOnlyUsername)
	assertRow(t, rows, "Bob", StateNotFound, ReasonOnlyEmail)
}

func TestPlanSameTarget(t *testing.T) {
	rows := plan(t, Request{Mode: ModeList, ListKind: ListByUsername, List: "alice,alice\nBob,alice\n"}, Input{})
	assertRow(t, rows, "alice#1", StateConflict, ReasonSameTarget)
	assertRow(t, rows, "Bob#2", StateConflict, ReasonSameTarget)
}

func TestPlanList(t *testing.T) {
	list := "alice,acct-alice\nnobody,acct-x\nheidi,acct-new\nheidi,acct-new2\ncarol,acct-carol\nbad line\nroot,acct-root\n"
	req := Request{Mode: ModeList, ListKind: ListBySub, List: list}
	entries, err := ParseList(list, ListBySub)
	require.NoError(t, err)
	rows := plan(t, req, Input{Entries: entries, Accounts: []Account{}})
	r := assertRow(t, rows, "alice#1", StateWillLink, ReasonUnchecked)
	assert.Equal(t, "acct-alice", r.Sub)
	assertRow(t, rows, "nobody#2", StateNotFound, ReasonUnknownLocal)
	assertRow(t, rows, "heidi#3", StateConflict, ReasonSameLocal)
	assertRow(t, rows, "heidi#4", StateConflict, ReasonSameLocal)
	assertRow(t, rows, "carol#5", StateAlreadyLinked, "")
	assertRow(t, rows, "bad line#6", StateNotFound, ReasonInvalidLine)
	assertRow(t, rows, "root#7", StateSkipped, ReasonAdmin)

	// a hand-entered sub that someone else already uses
	rows = plan(t, Request{Mode: ModeList, ListKind: ListBySub, List: "alice,acct-judy"}, Input{Entries: []ListEntry{{Line: 1, Local: "alice", Target: "acct-judy"}}, Accounts: []Account{}})
	assertRow(t, rows, "alice#1", StateConflict, ReasonHostingLinkedOther)

	// by hosting username, checked with the portal
	entries, err = ParseList("alice,ALICE\nBob,frank\nheidi,missing\n", ListByUsername)
	require.NoError(t, err)
	rows = plan(t, Request{Mode: ModeList, ListKind: ListByUsername, List: "x"}, Input{Entries: entries})
	assertRow(t, rows, "alice#1", StateWillLink, "")
	assertRow(t, rows, "Bob#2", StateConflict, ReasonSuspended)
	assertRow(t, rows, "heidi#3", StateNotFound, ReasonNoMatch)
}

func TestLookupKeys(t *testing.T) {
	u, e := LookupKeys(Request{Mode: ModeBoth}, users, nil)
	assert.NotContains(t, u, "root", "administrators are skipped")
	assert.NotContains(t, u, "erin", "disabled users are skipped")
	assert.Contains(t, u, "alice")
	assert.Contains(t, u, "carol", "linked users are looked up to find conflicts")
	assert.Contains(t, e, "alice@example.org")
	assert.NotContains(t, e, "")

	u, e = LookupKeys(Request{Mode: ModeUsername, IncludeAdmins: true}, users, nil)
	assert.Contains(t, u, "root")
	assert.Empty(t, e)

	u, e = LookupKeys(Request{Mode: ModeList, ListKind: ListByUsername}, nil, []ListEntry{{Local: "a", Target: "x"}, {Local: "b", Target: "X"}, {Err: true, Target: "y"}})
	assert.Equal(t, []string{"x"}, u)
	assert.Empty(t, e)
	u, _ = LookupKeys(Request{Mode: ModeList, ListKind: ListBySub}, nil, []ListEntry{{Local: "a", Target: "acct"}})
	assert.Empty(t, u)
}

func TestPreviewToken(t *testing.T) {
	req := Request{Mode: ModeUsername}
	rows := Plan(req, Input{Users: users, Accounts: accounts, LinkOwner: linkOwner, UserNames: userNames})
	token := PreviewToken(1, req, rows)
	assert.Equal(t, token, PreviewToken(1, req, Plan(req, Input{Users: users, Accounts: accounts, LinkOwner: linkOwner, UserNames: userNames})), "same data, same token")
	assert.NotEqual(t, token, PreviewToken(2, req, rows), "another source")

	// someone linked alice in between: the preview is stale
	owners := map[string]int64{"acct-alice": 42}
	for k, v := range linkOwner {
		owners[k] = v
	}
	stale := Plan(req, Input{Users: users, Accounts: accounts, LinkOwner: owners, UserNames: userNames})
	assert.NotEqual(t, token, PreviewToken(1, req, stale))

	// an account suspended in between
	changed := append([]Account(nil), accounts...)
	changed[2].Status = "suspended"
	assert.NotEqual(t, token, PreviewToken(1, req, Plan(req, Input{Users: users, Accounts: changed, LinkOwner: linkOwner, UserNames: userNames})))
	assert.Equal(t, 4, Counts(rows)[StateWillLink]) // alice, Bob, heidi, ivan
}
