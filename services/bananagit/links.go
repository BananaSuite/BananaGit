// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananagit

import (
	"context"
	"errors"
	"fmt"
	"strings"

	auth_model "gitea.dev/models/auth"
	"gitea.dev/models/db"
	system_model "gitea.dev/models/system"
	user_model "gitea.dev/models/user"
	bananagit_module "gitea.dev/modules/bananagit"
	"gitea.dev/modules/log"
	"gitea.dev/modules/optional"
	"gitea.dev/modules/setting"
	"gitea.dev/services/auth/source/oauth2"
	"gitea.dev/services/auth/source/oauth2/bananawiki"
	"gitea.dev/services/externalaccount"

	"github.com/markbates/goth"
	"xorm.io/builder"
)

// MaxBulkUsers bounds a bulk run; beyond it the pasted list is the way.
const MaxBulkUsers = 5000

var (
	ErrNoLookup      = errors.New("no portal lookup token")
	ErrTooManyUsers  = fmt.Errorf("more than %d users", MaxBulkUsers)
	ErrCreatedHere   = errors.New("the account was created by signing in with this source")
	ErrNotLinked     = errors.New("the account is not linked")
	ErrStalePreview  = errors.New("the preview is out of date")
	errNotIndividual = errors.New("not an individual user")
)

// HostingSource is the BananaWiki Hosting authentication source the tool works on: the active one with the
// lowest id, or nil.
func HostingSource(ctx context.Context) (*auth_model.Source, error) {
	sources, err := db.Find[auth_model.Source](ctx, auth_model.FindSourcesOptions{
		IsActive:  optional.Some(true),
		LoginType: auth_model.OAuth2,
	})
	if err != nil {
		return nil, err
	}
	var found *auth_model.Source
	for _, s := range sources {
		if IsHostingSource(s) && (found == nil || s.ID < found.ID) {
			found = s
		}
	}
	return found, nil
}

// LookupAvailable reports whether the portal lookup is configured ([bananagit] PORTAL_LOOKUP_TOKEN).
func LookupAvailable() bool {
	return setting.BananaGit.PortalLookupToken != ""
}

// PortalURL is the portal of a BananaWiki Hosting source, from its token URL.
func PortalURL(source *auth_model.Source) (string, error) {
	tokenURL := bananawiki.TokenURL
	if cfg, ok := source.Cfg.(*oauth2.Source); ok && cfg.CustomURLMapping != nil && cfg.CustomURLMapping.TokenURL != "" {
		tokenURL = cfg.CustomURLMapping.TokenURL
	}
	return bananawiki.PortalBaseURL(tokenURL)
}

func lookupClient(source *auth_model.Source) (*bananawiki.LookupClient, error) {
	if !LookupAvailable() {
		return nil, ErrNoLookup
	}
	base, err := PortalURL(source)
	if err != nil {
		return nil, err
	}
	return &bananawiki.LookupClient{BaseURL: base, Token: setting.BananaGit.PortalLookupToken}, nil
}

// linkState is every link of the source: by account id and by user.
type linkState struct {
	owner  map[string]int64 // sub -> user
	byUser map[int64]string // user -> sub
	signUp map[int64]bool   // users created by signing in with the source (their LoginName is the sub)
}

func loadLinkState(ctx context.Context, sourceID int64) (*linkState, error) {
	st := &linkState{owner: map[string]int64{}, byUser: map[int64]string{}, signUp: map[int64]bool{}}
	var links []*user_model.ExternalLoginUser
	if err := db.GetEngine(ctx).Cols("external_id", "user_id").Where("login_source_id = ?", sourceID).Find(&links); err != nil {
		return nil, err
	}
	for _, l := range links {
		st.owner[l.ExternalID] = l.UserID
		if _, ok := st.byUser[l.UserID]; !ok {
			st.byUser[l.UserID] = l.ExternalID
		}
	}
	var created []*user_model.User
	if err := db.GetEngine(ctx).Cols("id", "login_name").Where("login_type = ? AND login_source = ?", auth_model.OAuth2, sourceID).Find(&created); err != nil {
		return nil, err
	}
	for _, u := range created {
		if u.LoginName == "" {
			continue
		}
		st.signUp[u.ID] = true
		if _, ok := st.owner[u.LoginName]; !ok {
			st.owner[u.LoginName] = u.ID
		}
		st.byUser[u.ID] = u.LoginName
	}
	return st, nil
}

// localUsers loads individual users: all of them (names == nil) or the named ones.
func localUsers(ctx context.Context, names []string) ([]*user_model.User, error) {
	sess := db.GetEngine(ctx).Where("`type` = ?", user_model.UserTypeIndividual).OrderBy("id")
	if names != nil {
		lower := make([]string, 0, len(names))
		for _, n := range names {
			lower = append(lower, strings.ToLower(n))
		}
		if len(lower) == 0 {
			return nil, nil
		}
		sess = sess.In("lower_name", lower)
	} else {
		sess = sess.Limit(MaxBulkUsers + 1)
	}
	var users []*user_model.User
	if err := sess.Find(&users); err != nil {
		return nil, err
	}
	if names == nil && len(users) > MaxBulkUsers {
		return nil, ErrTooManyUsers
	}
	return users, nil
}

// verifiedPrimaryEmails maps users to their primary email when it is activated.
func verifiedPrimaryEmails(ctx context.Context, users []*user_model.User) (map[int64]string, error) {
	out := map[int64]string{}
	ids := make([]int64, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	for len(ids) > 0 {
		n := min(len(ids), 500)
		var emails []*user_model.EmailAddress
		if err := db.GetEngine(ctx).Where("is_primary = ? AND is_activated = ?", true, true).In("uid", ids[:n]).Find(&emails); err != nil {
			return nil, err
		}
		for _, e := range emails {
			out[e.UID] = e.Email
		}
		ids = ids[n:]
	}
	return out, nil
}

// Preview is a computed preview and the token that identifies it.
type Preview struct {
	Request bananagit_module.Request
	Rows    []bananagit_module.Row
	Counts  map[string]int
	Token   string
}

// ComputePreview works out what a request would do. Nothing is changed.
func ComputePreview(ctx context.Context, source *auth_model.Source, req bananagit_module.Request) (*Preview, error) {
	if err := req.Normalize(); err != nil {
		return nil, err
	}
	needsLookup := req.Mode != bananagit_module.ModeList || req.ListKind == bananagit_module.ListByUsername
	if needsLookup && !LookupAvailable() {
		return nil, ErrNoLookup
	}

	var entries []bananagit_module.ListEntry
	var names []string
	if req.Mode == bananagit_module.ModeList {
		var err error
		if entries, err = bananagit_module.ParseList(req.List, req.ListKind); err != nil {
			return nil, err
		}
		names = []string{}
		for _, e := range entries {
			if e.Local != "" {
				names = append(names, e.Local)
			}
		}
	}
	users, err := localUsers(ctx, names)
	if err != nil {
		return nil, err
	}
	emails, err := verifiedPrimaryEmails(ctx, users)
	if err != nil {
		return nil, err
	}
	st, err := loadLinkState(ctx, source.ID)
	if err != nil {
		return nil, err
	}

	in := bananagit_module.Input{Entries: entries, LinkOwner: st.owner, UserNames: map[int64]string{}}
	for _, u := range users {
		in.Users = append(in.Users, bananagit_module.LocalUser{
			ID: u.ID, Name: u.Name, Email: emails[u.ID], IsAdmin: u.IsAdmin,
			Disabled: !u.IsActive || u.ProhibitLogin, LinkedSub: st.byUser[u.ID],
		})
	}

	if needsLookup {
		usernames, mails := bananagit_module.LookupKeys(req, in.Users, entries)
		if len(usernames)+len(mails) > 0 {
			client, err := lookupClient(source)
			if err != nil {
				return nil, err
			}
			res, err := client.Lookup(ctx, usernames, mails)
			if err != nil {
				return nil, err
			}
			for _, a := range res.Accounts {
				in.Accounts = append(in.Accounts, bananagit_module.Account{Sub: a.Sub, Username: a.Username, Email: a.Email, Status: a.Status})
			}
		}
	}

	// names of the other users that conflicts point at
	var otherIDs []int64
	for _, uid := range st.owner {
		otherIDs = append(otherIDs, uid)
	}
	if err := fillUserNames(ctx, in.UserNames, otherIDs); err != nil {
		return nil, err
	}

	rows := bananagit_module.Plan(req, in)
	return &Preview{Request: req, Rows: rows, Counts: bananagit_module.Counts(rows), Token: bananagit_module.PreviewToken(source.ID, req, rows)}, nil
}

func fillUserNames(ctx context.Context, names map[int64]string, ids []int64) error {
	for len(ids) > 0 {
		n := min(len(ids), 500)
		var users []*user_model.User
		if err := db.GetEngine(ctx).Cols("id", "name").In("id", ids[:n]).Find(&users); err != nil {
			return err
		}
		for _, u := range users {
			names[u.ID] = u.Name
		}
		ids = ids[n:]
	}
	return nil
}

// ApplyResult tells what an Apply did.
type ApplyResult struct {
	Linked []bananagit_module.Row
	Failed []bananagit_module.Row
}

// Apply links the "will link" rows of the preview identified by token. When the data changed since that preview
// it links nothing and returns the new preview with ErrStalePreview.
func Apply(ctx context.Context, doer *user_model.User, source *auth_model.Source, req bananagit_module.Request, token string) (*ApplyResult, *Preview, error) {
	preview, err := ComputePreview(ctx, source, req)
	if err != nil {
		return nil, nil, err
	}
	if token == "" || token != preview.Token {
		return nil, preview, ErrStalePreview
	}
	res := &ApplyResult{}
	var audit []string
	for _, row := range preview.Rows {
		if row.State != bananagit_module.StateWillLink {
			continue
		}
		if err := linkOne(ctx, source, row, doer, preview.Request.Mode); err != nil {
			log.Warn("BananaGit account links: linking %s to %s failed: %v", row.LocalName, row.Sub, err)
			res.Failed = append(res.Failed, row)
			continue
		}
		log.Info("BananaGit account links: %s linked %s to BananaWiki Hosting account %s (%s), rule %s", doer.Name, row.LocalName, row.Hosting, row.Sub, preview.Request.Mode)
		res.Linked = append(res.Linked, row)
		audit = append(audit, fmt.Sprintf("%s → %s (%s)", row.LocalName, displayHosting(row), row.Sub))
	}
	if len(res.Linked) > 0 {
		if err := system_model.CreateNotice(ctx, system_model.NoticeTask, "%s linked %d BananaGit accounts to BananaWiki Hosting accounts (source %s, rule %s): %s",
			doer.Name, len(res.Linked), source.Name, preview.Request.Mode, strings.Join(audit, ", ")); err != nil {
			log.Error("CreateNotice: %v", err)
		}
	}
	return res, preview, nil
}

func displayHosting(row bananagit_module.Row) string {
	if row.Hosting != "" {
		return row.Hosting
	}
	return "?"
}

// linkOne writes the link the way a user-made link is written (externalaccount.LinkAccountToUser).
func linkOne(ctx context.Context, source *auth_model.Source, row bananagit_module.Row, doer *user_model.User, rule string) error {
	u, err := user_model.GetUserByID(ctx, row.UserID)
	if err != nil {
		return err
	}
	if !u.IsIndividual() {
		return errNotIndividual
	}
	raw := map[string]any{"sub": row.Sub, "bananagit_linked_by": doer.Name, "bananagit_link_rule": rule}
	if row.Hosting != "" {
		raw["preferred_username"] = row.Hosting
	}
	return externalaccount.LinkAccountToUser(ctx, source.ID, u, goth.User{
		Provider: source.Name,
		UserID:   row.Sub,
		NickName: row.Hosting,
		Email:    row.Email,
		RawData:  raw,
	})
}

// Unlink removes the BananaWiki Hosting link of a user.
func Unlink(ctx context.Context, doer *user_model.User, source *auth_model.Source, userID int64) (*user_model.User, error) {
	u, err := user_model.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.LoginType == auth_model.OAuth2 && u.LoginSource == source.ID {
		return u, ErrCreatedHere
	}
	var links []*user_model.ExternalLoginUser
	if err := db.GetEngine(ctx).Where("user_id = ? AND login_source_id = ?", u.ID, source.ID).Find(&links); err != nil {
		return u, err
	}
	if len(links) == 0 {
		return u, ErrNotLinked
	}
	if _, err := user_model.RemoveAccountLink(ctx, u, source.ID); err != nil {
		return u, err
	}
	subs := make([]string, 0, len(links))
	for _, l := range links {
		subs = append(subs, l.ExternalID)
	}
	log.Info("BananaGit account links: %s unlinked %s from BananaWiki Hosting account %s", doer.Name, u.Name, strings.Join(subs, ", "))
	if err := system_model.CreateNotice(ctx, system_model.NoticeTask, "%s unlinked BananaGit account %s from BananaWiki Hosting account %s (source %s)",
		doer.Name, u.Name, strings.Join(subs, ", "), source.Name); err != nil {
		log.Error("CreateNotice: %v", err)
	}
	return u, nil
}

// UserLink is one user on the Account links page.
type UserLink struct {
	User     *user_model.User
	Sub      string
	Hosting  string // the hosting username stored with the link
	SignedUp bool   // created by signing in with the source: the link cannot be removed here
}

// ListOptions filter the users on the page.
type ListOptions struct {
	Keyword      string
	UnlinkedOnly bool
	Page         int
	PageSize     int
}

func notLinkedCond(sourceID int64) builder.Cond {
	return builder.NotIn("id", builder.Select("user_id").From("external_login_user").Where(builder.Eq{"login_source_id": sourceID})).
		And(builder.Not{builder.Eq{"login_type": auth_model.OAuth2, "login_source": sourceID}})
}

// ListUsers lists individual users with their link state, by name.
func ListUsers(ctx context.Context, source *auth_model.Source, opts ListOptions) ([]*UserLink, int64, error) {
	cond := builder.NewCond().And(builder.Eq{"`type`": user_model.UserTypeIndividual})
	if kw := strings.ToLower(strings.TrimSpace(opts.Keyword)); kw != "" {
		cond = cond.And(builder.Or(builder.Like{"lower_name", kw}, builder.Like{"LOWER(email)", kw}))
	}
	if opts.UnlinkedOnly {
		cond = cond.And(notLinkedCond(source.ID))
	}
	total, err := db.GetEngine(ctx).Where(cond).Count(new(user_model.User))
	if err != nil {
		return nil, 0, err
	}
	var users []*user_model.User
	if err := db.GetEngine(ctx).Where(cond).OrderBy("lower_name").Limit(opts.PageSize, (max(opts.Page, 1)-1)*opts.PageSize).Find(&users); err != nil {
		return nil, 0, err
	}
	ids := make([]int64, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	links := map[int64]*user_model.ExternalLoginUser{}
	if len(ids) > 0 {
		var rows []*user_model.ExternalLoginUser
		if err := db.GetEngine(ctx).Cols("external_id", "user_id", "nick_name").Where("login_source_id = ?", source.ID).In("user_id", ids).Find(&rows); err != nil {
			return nil, 0, err
		}
		for _, r := range rows {
			links[r.UserID] = r
		}
	}
	out := make([]*UserLink, 0, len(users))
	for _, u := range users {
		ul := &UserLink{User: u}
		if l, ok := links[u.ID]; ok {
			ul.Sub, ul.Hosting = l.ExternalID, l.NickName
		}
		if u.LoginType == auth_model.OAuth2 && u.LoginSource == source.ID {
			ul.SignedUp = true
			if ul.Sub == "" {
				ul.Sub = u.LoginName
			}
		}
		out = append(out, ul)
	}
	return out, total, nil
}

// UnlinkedCounts counts active users that cannot sign in with BananaWiki Hosting yet, and the administrators
// among them: what SIGNIN_MODE = hosting would lock out of the web.
func UnlinkedCounts(ctx context.Context, source *auth_model.Source) (users, admins int64, err error) {
	cond := builder.Eq{"`type`": user_model.UserTypeIndividual, "is_active": true, "prohibit_login": false}.And(notLinkedCond(source.ID))
	if users, err = db.GetEngine(ctx).Where(cond).Count(new(user_model.User)); err != nil {
		return 0, 0, err
	}
	admins, err = db.GetEngine(ctx).Where(cond.And(builder.Eq{"is_admin": true})).Count(new(user_model.User))
	return users, admins, err
}
