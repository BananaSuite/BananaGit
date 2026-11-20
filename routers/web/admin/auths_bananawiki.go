// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package admin

import (
	"regexp"

	"gitea.dev/modules/templates"
	"gitea.dev/services/auth/source/oauth2"
	"gitea.dev/services/context"
	"gitea.dev/services/forms"
)

// The portal compares redirect URIs exactly, and the source name is a path segment of
// the callback: a name that needs escaping ("BananaWiki Auth") gives a URI nobody registers.
var bananaWikiSourceName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// checkBananaWikiSourceName renders the form again with an error when a BananaWiki Hosting
// source has a name that does not fit a redirect URI as it is. It reports whether it did.
func checkBananaWikiSourceName(ctx *context.Context, config *oauth2.Source, form forms.AuthenticationForm, tpl templates.TplName) bool {
	if config.Provider != "bananawiki" || bananaWikiSourceName.MatchString(form.Name) {
		return false
	}
	ctx.Data["Err_Name"] = true
	ctx.RenderWithErrDeprecated(ctx.Tr("admin.auths.bananawiki_invalid_name"), tpl, form)
	return true
}
