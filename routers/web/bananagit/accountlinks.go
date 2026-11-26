// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananagit

import (
	"errors"
	"net/http"
	"strings"

	user_model "gitea.dev/models/user"
	bananagit_module "gitea.dev/modules/bananagit"
	"gitea.dev/modules/log"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/templates"
	"gitea.dev/modules/web"
	"gitea.dev/services/auth/source/oauth2/bananawiki"
	bananagit_service "gitea.dev/services/bananagit"
	"gitea.dev/services/context"
)

const (
	tplAccountLinks  templates.TplName = "bananagit/account_links"
	accountLinksPath                   = "/-/admin/bananagit/account-links"
	accountLinksPage                   = 50
)

// registerAccountLinks adds the Site Administration page "Account links" (SIGNIN-SPEC A3 + B). Every route needs
// an administrator; POSTs are covered by Gitea's cross-origin protection like every admin form.
func registerAccountLinks(m *web.Router, adminReq func(*context.Context)) {
	m.Get(accountLinksPath, adminReq, AccountLinks)
	m.Post(accountLinksPath+"/preview", adminReq, AccountLinksPreview)
	m.Post(accountLinksPath+"/apply", adminReq, AccountLinksApply)
	m.Post(accountLinksPath+"/unlink", adminReq, AccountLinksUnlink)
}

func requireAdmin(ctx *context.Context) bool {
	if ctx.Doer == nil || !ctx.Doer.IsAdmin {
		ctx.HTTPError(http.StatusForbidden)
		return false
	}
	return true
}

// renderAccountLinks renders the page: the users and their link state, the forms and, when there is one, a preview.
func renderAccountLinks(ctx *context.Context, status int) {
	ctx.Data["Title"] = ctx.Tr("bananagit.links.title")
	ctx.Data["PageIsAdminBananaGitAccountLinks"] = true
	ctx.Data["EnableOAuth2"] = setting.OAuth2.Enabled
	ctx.Data["EnablePackages"] = setting.Packages.Enabled
	ctx.Data["Link"] = setting.AppSubURL + accountLinksPath
	ctx.Data["LookupAvailable"] = bananagit_service.LookupAvailable()
	ctx.Data["HostingOnly"] = setting.BananaGit.SignInMode == setting.SignInModeHosting
	ctx.Data["MaxListRows"] = bananagit_module.MaxListRows

	source, err := bananagit_service.HostingSource(ctx)
	if err != nil {
		ctx.ServerError("HostingSource", err)
		return
	}
	ctx.Data["Source"] = source
	if source == nil {
		ctx.HTML(status, tplAccountLinks)
		return
	}
	if portal, err := bananagit_service.PortalURL(source); err == nil {
		ctx.Data["PortalURL"] = portal
	}

	unlinked, unlinkedAdmins, err := bananagit_service.UnlinkedCounts(ctx, source)
	if err != nil {
		ctx.ServerError("UnlinkedCounts", err)
		return
	}
	ctx.Data["UnlinkedUsers"] = unlinked
	ctx.Data["UnlinkedAdmins"] = unlinkedAdmins

	opts := bananagit_service.ListOptions{
		Keyword:      ctx.FormTrim("q"),
		UnlinkedOnly: ctx.FormBool("unlinked"),
		Page:         max(ctx.FormInt("page"), 1),
		PageSize:     accountLinksPage,
	}
	users, total, err := bananagit_service.ListUsers(ctx, source, opts)
	if err != nil {
		ctx.ServerError("ListUsers", err)
		return
	}
	ctx.Data["Keyword"] = opts.Keyword
	ctx.Data["UnlinkedOnly"] = opts.UnlinkedOnly
	ctx.Data["Users"] = users
	ctx.Data["Total"] = total
	ctx.Data["Page"] = context.NewPagerBuilder(ctx).TotalCount(total).PerPageLimit(opts.PageSize).CurPage(opts.Page).Build()
	ctx.HTML(status, tplAccountLinks)
}

// AccountLinks shows the page.
func AccountLinks(ctx *context.Context) {
	if !requireAdmin(ctx) {
		return
	}
	renderAccountLinks(ctx, http.StatusOK)
}

// linkRequest reads a linking request from the form; the one-user form is a list of one line.
func linkRequest(ctx *context.Context) bananagit_module.Request {
	req := bananagit_module.Request{
		Mode:          ctx.Req.PostFormValue("mode"),
		IncludeAdmins: ctx.Req.PostFormValue("include_admins") != "",
		ListKind:      ctx.Req.PostFormValue("list_kind"),
		List:          ctx.Req.PostFormValue("list"),
	}
	if req.Mode == "single" {
		local := strings.TrimSpace(ctx.Req.PostFormValue("local"))
		target := strings.TrimSpace(ctx.Req.PostFormValue("target"))
		req.Mode, req.List = bananagit_module.ModeList, local+","+target
		if local == "" || target == "" {
			req.List = ""
		}
	}
	return req
}

func linkErrorText(ctx *context.Context, err error) string {
	switch {
	case errors.Is(err, bananagit_service.ErrNoLookup):
		return ctx.Locale.TrString("bananagit.links.error.no_lookup")
	case errors.Is(err, bananagit_module.ErrBadRequest):
		return ctx.Locale.TrString("bananagit.links.error.request")
	case errors.Is(err, bananagit_module.ErrListTooLong):
		return ctx.Locale.TrString("bananagit.links.error.list_too_long", bananagit_module.MaxListRows)
	case errors.Is(err, bananagit_service.ErrTooManyUsers):
		return ctx.Locale.TrString("bananagit.links.error.too_many_users", bananagit_service.MaxBulkUsers)
	case errors.Is(err, bananawiki.ErrLookupRefused):
		return ctx.Locale.TrString("bananagit.links.error.lookup_refused")
	}
	return ""
}

// previewError shows the page with what went wrong, keeping what was typed.
func previewError(ctx *context.Context, req bananagit_module.Request, err error) {
	text := linkErrorText(ctx, err)
	if text == "" {
		log.Error("BananaGit account links: %v", err)
		text = ctx.Locale.TrString("bananagit.links.error.lookup")
	}
	ctx.Data["LinkError"] = text
	ctx.Data["LinkRequest"] = req
	renderAccountLinks(ctx, http.StatusUnprocessableEntity)
}

// AccountLinksPreview computes and shows a preview. Nothing changes.
func AccountLinksPreview(ctx *context.Context) {
	if !requireAdmin(ctx) {
		return
	}
	source, err := bananagit_service.HostingSource(ctx)
	if err != nil {
		ctx.ServerError("HostingSource", err)
		return
	}
	if source == nil {
		ctx.Redirect(setting.AppSubURL + accountLinksPath)
		return
	}
	req := linkRequest(ctx)
	preview, err := bananagit_service.ComputePreview(ctx, source, req)
	if err != nil {
		previewError(ctx, req, err)
		return
	}
	ctx.Data["Preview"] = preview
	ctx.Data["LinkRequest"] = preview.Request
	renderAccountLinks(ctx, http.StatusOK)
}

// AccountLinksApply links the "will link" rows of the preview the administrator saw, and only if nothing changed.
func AccountLinksApply(ctx *context.Context) {
	if !requireAdmin(ctx) {
		return
	}
	source, err := bananagit_service.HostingSource(ctx)
	if err != nil {
		ctx.ServerError("HostingSource", err)
		return
	}
	if source == nil {
		ctx.Redirect(setting.AppSubURL + accountLinksPath)
		return
	}
	req := linkRequest(ctx)
	res, preview, err := bananagit_service.Apply(ctx, ctx.Doer, source, req, ctx.Req.PostFormValue("token"))
	if errors.Is(err, bananagit_service.ErrStalePreview) {
		ctx.Data["Preview"] = preview
		ctx.Data["LinkRequest"] = preview.Request
		ctx.Data["PreviewStale"] = true
		renderAccountLinks(ctx, http.StatusConflict)
		return
	}
	if err != nil {
		previewError(ctx, req, err)
		return
	}
	if len(res.Failed) > 0 {
		names := make([]string, 0, len(res.Failed))
		for _, r := range res.Failed {
			names = append(names, r.LocalName)
		}
		ctx.Flash.Warning(ctx.Tr("bananagit.links.applied_some", len(res.Linked), strings.Join(names, ", ")))
	} else {
		ctx.Flash.Success(ctx.Tr("bananagit.links.applied", len(res.Linked)))
	}
	ctx.Redirect(setting.AppSubURL + accountLinksPath)
}

// AccountLinksUnlink removes one user's BananaWiki Hosting link.
func AccountLinksUnlink(ctx *context.Context) {
	if !requireAdmin(ctx) {
		return
	}
	source, err := bananagit_service.HostingSource(ctx)
	if err != nil {
		ctx.ServerError("HostingSource", err)
		return
	}
	if source == nil {
		ctx.Redirect(setting.AppSubURL + accountLinksPath)
		return
	}
	u, err := bananagit_service.Unlink(ctx, ctx.Doer, source, ctx.FormInt64("uid"))
	switch {
	case err == nil:
		ctx.Flash.Success(ctx.Tr("bananagit.links.unlinked", u.Name))
	case user_model.IsErrUserNotExist(err):
		ctx.Flash.Error(ctx.Tr("bananagit.links.error.no_user"))
	case errors.Is(err, bananagit_service.ErrCreatedHere):
		ctx.Flash.Error(ctx.Tr("bananagit.links.error.created_here", u.Name))
	case errors.Is(err, bananagit_service.ErrNotLinked):
		ctx.Flash.Info(ctx.Tr("bananagit.links.error.not_linked", u.Name))
	default:
		ctx.ServerError("Unlink", err)
		return
	}
	ctx.Redirect(setting.AppSubURL + accountLinksPath)
}
