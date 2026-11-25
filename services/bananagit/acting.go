// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package bananagit is BananaGit's sign-in batch (SIGNIN-SPEC) on top of Gitea: sign-ins of hosting
// administrators acting as an account, BananaWiki Hosting sign-ins skipping the local two-step check, and the
// Account links tool.
package bananagit

import (
	"net/http"
	"time"

	auth_model "gitea.dev/models/auth"
	system_model "gitea.dev/models/system"
	user_model "gitea.dev/models/user"
	bananagit_module "gitea.dev/modules/bananagit"
	"gitea.dev/modules/log"
	"gitea.dev/modules/session"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/templates"
	"gitea.dev/modules/web/middleware"
	"gitea.dev/services/auth/source/oauth2"
	"gitea.dev/services/auth/source/oauth2/bananawiki"
	"gitea.dev/services/context"

	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
)

// HostingProviderName is the OAuth2 provider of BananaWiki Hosting (providers_custom.go).
const HostingProviderName = "bananawiki"

const tplActingRefused templates.TplName = "bananagit/acting_refused"

// IsHostingSource reports whether an authentication source signs in with BananaWiki Hosting.
func IsHostingSource(source *auth_model.Source) bool {
	if source == nil {
		return false
	}
	cfg, ok := source.Cfg.(*oauth2.Source)
	return ok && cfg.Provider == HostingProviderName
}

// SkipsLocal2FA reports whether a sign-in through this source skips BananaGit's own two-step check (SIGNIN-SPEC
// C2): BananaWiki Hosting sources, unless [bananagit] HOSTING_SKIPS_LOCAL_2FA = false. Password sign-ins never do.
func SkipsLocal2FA(source *auth_model.Source) bool {
	return setting.BananaGit.HostingSkipsLocal2FA && IsHostingSource(source)
}

// refuseSignIn ends an OAuth2 sign-in with a page that says why; the provider session is dropped so that the
// next attempt starts afresh at the portal.
func refuseSignIn(ctx *context.Context, reason string, args ...any) {
	_ = gothic.Logout(ctx.Resp, ctx.Req)
	ctx.Data["Title"] = ctx.Tr("bananagit.acting.refused_title")
	ctx.Data["BananaGitRefusal"] = ctx.Tr("bananagit.acting.refused."+reason, args...)
	ctx.HTML(http.StatusForbidden, tplActingRefused)
}

// CheckActingSignIn runs before an OAuth2 sign-in of an existing account (handleOAuth2SignIn). It returns the
// acting state to keep in the session, nil for an ordinary sign-in, and writes a refusal when the sign-in may
// not happen (the caller checks ctx.Written()).
func CheckActingSignIn(ctx *context.Context, source *auth_model.Source, u *user_model.User, gothUser goth.User) *bananagit_module.Acting {
	if !IsHostingSource(source) {
		return nil
	}
	act, err := bananawiki.ActingFromRawData(gothUser.RawData)
	if err != nil {
		log.Warn("BananaGit: refused a BananaWiki Hosting sign-in as %s from %s: %v", u.Name, ctx.RemoteAddr(), err)
		refuseSignIn(ctx, "invalid")
		return nil
	}
	if act == nil {
		return nil
	}
	bananawiki.StripActing(gothUser.RawData) // not stored with the account link

	now := time.Now()
	refuse := func(reason, why string) *bananagit_module.Acting {
		log.Warn("BananaGit: refused the sign-in of hosting administrator %s (%s) acting as %s from %s: %s", act.AdminName, act.AdminSub, u.Name, ctx.RemoteAddr(), why)
		refuseSignIn(ctx, reason)
		return nil
	}
	switch {
	case !setting.BananaGit.AllowActingAdmins:
		return refuse("disabled", "[bananagit] ALLOW_ACTING_ADMINS = false")
	case !now.Before(act.ExpiresAt):
		return refuse("expired", "act_expires_at has passed")
	case u.IsAdmin:
		return refuse("admin", "the account is a BananaGit administrator")
	}
	if !source.TwoFactorShouldSkip() && !SkipsLocal2FA(source) {
		has2FA, err := auth_model.HasTwoFactorOrWebAuthn(ctx, u.ID)
		if err != nil {
			ctx.ServerError("HasTwoFactorOrWebAuthn", err)
			return nil
		}
		if has2FA {
			return refuse("two_factor", "the account asks for BananaGit's own two-step check")
		}
	}

	acting := &bananagit_module.Acting{UID: u.ID, AdminSub: act.AdminSub, AdminName: act.AdminName, Until: bananagit_module.ClampUntil(act.ExpiresAt, now)}
	log.Info("BananaGit: hosting administrator %s (%s) signed in as %s from %s, acting until %s", act.AdminName, act.AdminSub, u.Name, ctx.RemoteAddr(), acting.Until.UTC().Format(time.RFC3339))
	if err := system_model.CreateNotice(ctx, system_model.NoticeTask, "BananaWiki Hosting administrator %s (%s) signed in as %s from %s, acting until %s",
		act.AdminName, act.AdminSub, u.Name, ctx.RemoteAddr(), acting.Until.UTC().Format(time.RFC3339)); err != nil {
		log.Error("CreateNotice: %v", err)
	}
	return acting
}

// ActingSessionKeys are deleted from the session on every OAuth2 sign-in before WithActing sets them again.
var ActingSessionKeys = bananagit_module.SessionActKeys

// WithActing adds the acting state to the session values of a sign-in. regenerateSession deletes
// ActingSessionKeys first, so an ordinary OAuth2 sign-in never keeps an earlier acting state.
func WithActing(acting *bananagit_module.Acting, values map[string]any) map[string]any {
	if acting != nil {
		for k, v := range acting.SessionValues() {
			values[k] = v
		}
	}
	return values
}

func sessionGetter(ctx *context.Context) func(string) any {
	return func(key string) any { return ctx.Session.Get(key) }
}

// IsActing reports whether the current session is acting.
func IsActing(ctx *context.Context) bool {
	if ctx.Session == nil {
		return false
	}
	_, ok := bananagit_module.ActingFromSession(sessionGetter(ctx))
	return ok
}

// RefuseLinkWhileActing runs where an OAuth2 sign-in found no account: while acting (in the userinfo or in the
// current session) nothing is linked and no account is created. It reports whether it refused.
func RefuseLinkWhileActing(ctx *context.Context, source *auth_model.Source, gothUser goth.User) bool {
	act, err := bananawiki.ActingFromRawData(gothUser.RawData)
	if !IsHostingSource(source) {
		act, err = nil, nil
	}
	switch {
	case err != nil:
		log.Warn("BananaGit: refused a BananaWiki Hosting sign-in from %s: %v", ctx.RemoteAddr(), err)
		refuseSignIn(ctx, "invalid")
	case act != nil:
		log.Warn("BananaGit: refused the sign-in of hosting administrator %s (%s) acting as unknown account %q from %s: no linking or new accounts while acting", act.AdminName, act.AdminSub, gothUser.UserID, ctx.RemoteAddr())
		refuseSignIn(ctx, "unknown_account")
	case IsActing(ctx):
		log.Warn("BananaGit: refused linking %q to the acting session of %s from %s", gothUser.UserID, ctx.Session.Get(session.KeyUname), ctx.RemoteAddr())
		refuseSignIn(ctx, "link")
	default:
		return false
	}
	return true
}

// ActingView is what the banner shows.
type ActingView struct {
	AdminName string
	UserName  string
	Until     time.Time
}

// ActingGuard runs on every web request after authentication: it shows the banner while acting, ends the
// session when acting is over, and refuses what an acting administrator may not do.
func ActingGuard(ctx *context.Context) {
	if ctx.Session == nil {
		return
	}
	act, ok := bananagit_module.ActingFromSession(sessionGetter(ctx))
	if !ok {
		return
	}
	uid, _ := ctx.Session.Get(session.KeyUID).(int64)
	if uid == 0 || uid != act.UID {
		// signed out or signed in again in another way since: the acting state is stale
		for _, k := range bananagit_module.SessionActKeys {
			_ = ctx.Session.Delete(k)
		}
		return
	}

	why := ""
	switch {
	case act.Expired(time.Now()):
		why = "expired"
	case !setting.BananaGit.AllowActingAdmins:
		why = "disabled"
	case ctx.Doer == nil || ctx.Doer.ID != act.UID:
		why = "signed_out"
	case ctx.Doer.IsAdmin:
		why = "admin"
	}
	if why != "" {
		endActing(ctx, act, why)
		return
	}

	ctx.Data["BananaGitActing"] = &ActingView{AdminName: act.AdminName, UserName: ctx.Doer.Name, Until: act.Until}
	if bananagit_module.ActingRefuses(ctx.Req.Method, ctx.Req.URL.Path) {
		log.Warn("BananaGit: refused %s %s to hosting administrator %s (%s) acting as %s", ctx.Req.Method, ctx.Req.URL.Path, act.AdminName, act.AdminSub, ctx.Doer.Name)
		ctx.Flash.Error(ctx.Tr("bananagit.acting.refused_action", act.AdminName))
		ctx.Redirect(setting.AppSubURL + "/user/settings")
	}
}

// endActing signs out, as the sign-out route does, and says why.
func endActing(ctx *context.Context, act *bananagit_module.Acting, why string) {
	name, _ := ctx.Session.Get(session.KeyUname).(string)
	log.Info("BananaGit: ended the session of hosting administrator %s (%s) acting as %s: %s", act.AdminName, act.AdminSub, name, why)
	_ = ctx.Session.Flush()
	_ = ctx.Session.Destroy(ctx.Resp, ctx.Req)
	ctx.DeleteSiteCookie(setting.CookieRememberName)
	middleware.DeleteRedirectToCookie(ctx.Resp)
	ctx.Doer, ctx.IsSigned = nil, false
	ctx.Flash.Info(ctx.Tr("bananagit.acting.ended", act.AdminName, name))
	ctx.Redirect(setting.AppSubURL + "/")
}
