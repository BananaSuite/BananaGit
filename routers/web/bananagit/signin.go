// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananagit

import (
	auth_model "gitea.dev/models/auth"
	"gitea.dev/models/db"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/log"
	"gitea.dev/modules/setting"
	"gitea.dev/routers/web/auth"
	bananagit_service "gitea.dev/services/bananagit"
	"gitea.dev/services/context"
)

// SignIn is what the sign-in related templates use for BananaWiki Hosting wording (link account page, Settings →
// Security → Linked accounts) and the acting banner; it is worked out only when a template asks.
type SignIn struct {
	ctx *context.Context
}

// PendingLink is the BananaWiki Hosting account waiting on the link account page.
type PendingLink struct {
	Username string
	Email    string
}

// Acting is the acting state for the banner, nil when not acting.
func (s *SignIn) Acting() *bananagit_service.ActingView {
	v, _ := s.ctx.Data["BananaGitActing"].(*bananagit_service.ActingView)
	return v
}

// HostingOnly reports [bananagit] SIGNIN_MODE = hosting.
func (s *SignIn) HostingOnly() bool {
	return setting.BananaGit.SignInMode == setting.SignInModeHosting
}

// PendingHosting is the account of the link account page when it comes from BananaWiki Hosting, else nil.
func (s *SignIn) PendingHosting() *PendingLink {
	data, ok := s.ctx.Session.Get("linkAccountData").(auth.LinkAccountData)
	if !ok {
		return nil
	}
	source, err := auth_model.GetSourceByID(s.ctx, data.AuthSourceID)
	if err != nil || !bananagit_service.IsHostingSource(source) {
		return nil
	}
	name := data.GothUser.NickName
	if name == "" {
		name = data.GothUser.Name
	}
	return &PendingLink{Username: name, Email: data.GothUser.Email}
}

// IsHosting reports whether an authentication source is BananaWiki Hosting.
func (s *SignIn) IsHosting(source *auth_model.Source) bool {
	return bananagit_service.IsHostingSource(source)
}

// LinkedAs is the hosting username the signed-in user's link to a source was made with, if known.
func (s *SignIn) LinkedAs(source *auth_model.Source) string {
	if s.ctx.Doer == nil || source == nil {
		return ""
	}
	links, err := db.Find[user_model.ExternalLoginUser](s.ctx, user_model.FindExternalUserOptions{UserID: s.ctx.Doer.ID, LoginSourceID: source.ID})
	if err != nil {
		log.Error("Find ExternalLoginUser: %v", err)
		return ""
	}
	for _, l := range links {
		if l.NickName != "" {
			return l.NickName
		}
	}
	return ""
}
