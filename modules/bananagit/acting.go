// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package bananagit holds the plain logic of BananaGit's sign-in batch (SIGNIN-SPEC): what an acting hosting
// administrator may not do, and the Account links planner. It has no database or web dependencies.
package bananagit

import (
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Session keys of a sign-in made by a hosting administrator acting as the account.
const (
	SessionActUID   = "bananagit_act_uid"   // the account acted as; the keys only count while it is the session's user
	SessionActSub   = "bananagit_act_sub"   // the administrator's portal account id
	SessionActName  = "bananagit_act_name"  // the administrator's portal username
	SessionActUntil = "bananagit_act_until" // Unix seconds: the session ends then
)

// SessionActKeys are all acting keys, deleted on every sign-in before an acting one sets them again.
var SessionActKeys = []string{SessionActUID, SessionActSub, SessionActName, SessionActUntil}

// MaxActing caps how long an acting session can last, whatever the portal says.
const MaxActing = 12 * time.Hour

// Acting is an acting session as stored in the session.
type Acting struct {
	UID       int64
	AdminSub  string
	AdminName string
	Until     time.Time
}

// Expired reports whether the session must end.
func (a *Acting) Expired(now time.Time) bool {
	return !now.Before(a.Until)
}

// SessionValues are the values to store for an acting sign-in.
func (a *Acting) SessionValues() map[string]any {
	return map[string]any{
		SessionActUID:   a.UID,
		SessionActSub:   a.AdminSub,
		SessionActName:  a.AdminName,
		SessionActUntil: a.Until.Unix(),
	}
}

// ActingFromSession rebuilds the acting state from session values; ok is false when there is none.
func ActingFromSession(get func(key string) any) (a *Acting, present bool) {
	if get(SessionActUID) == nil && get(SessionActUntil) == nil && get(SessionActSub) == nil {
		return nil, false
	}
	uid, _ := get(SessionActUID).(int64)
	until, _ := get(SessionActUntil).(int64)
	sub, _ := get(SessionActSub).(string)
	name, _ := get(SessionActName).(string)
	// present but damaged values still count as acting, with an expiry in the past: the session ends
	return &Acting{UID: uid, AdminSub: sub, AdminName: name, Until: time.Unix(until, 0)}, true
}

// ClampUntil keeps the portal's act_expires_at within MaxActing from now.
func ClampUntil(until, now time.Time) time.Time {
	if limit := now.Add(MaxActing); until.After(limit) {
		return limit
	}
	return until
}

type actingRule struct {
	prefix    string
	segment   bool // match whole path segments only
	allowRead bool // GET and HEAD stay possible
	exactRead bool // only GET/HEAD of the prefix itself stays possible
}

// actingRules are what an acting administrator may not do (SIGNIN-SPEC A2): password, two-step, emails, sign-in
// methods and linked accounts, deleting the account, access tokens, OAuth2 applications and grants, SSH/GPG keys,
// signing in elsewhere or linking accounts.
var actingRules = []actingRule{
	{prefix: "/user/settings/account", segment: true, allowRead: true}, // password, emails, deleting the account
	{prefix: "/user/settings/change_password", segment: true},
	{prefix: "/user/settings/security", segment: true, exactRead: true}, // two-step, passkeys, OpenID, linked accounts
	{prefix: "/user/settings/applications", segment: true, allowRead: true},
	{prefix: "/user/settings/keys", segment: true, allowRead: true},
	{prefix: "/user/settings/packages/chef/regenerate_keypair", segment: true},
	{prefix: "/user/oauth2", segment: true},           // signing in with or connecting another source
	{prefix: "/user/link_account"},                    // also link_account_signin and link_account_signup
	{prefix: "/login/oauth/authorize", segment: true}, // BananaGit as an OAuth2 provider: would hand out tokens
	{prefix: "/login/oauth/grant", segment: true},
}

var deployKeysPath = regexp.MustCompile(`^/[^/]+/[^/]+/settings/keys(/|$)`)

// ActingRefuses reports whether a request is refused while acting.
func ActingRefuses(method, urlPath string) bool {
	p := strings.ToLower(path.Clean("/" + urlPath))
	read := method == http.MethodGet || method == http.MethodHead
	for _, r := range actingRules {
		match := strings.HasPrefix(p, r.prefix)
		if match && r.segment {
			match = len(p) == len(r.prefix) || p[len(r.prefix)] == '/'
		}
		if !match {
			continue
		}
		switch {
		case r.allowRead:
			return !read
		case r.exactRead:
			return !read || p != r.prefix
		default:
			return true
		}
	}
	if deployKeysPath.MatchString(p) {
		return !read // a deploy key would keep access to the repository after the session ends
	}
	return false
}

// ValidSub reports whether s looks like a portal account id: 1..64 printable ASCII characters without spaces.
func ValidSub(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
