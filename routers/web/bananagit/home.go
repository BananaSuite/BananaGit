// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananagit

import (
	stdctx "context"

	"gitea.dev/modules/graceful"
	"gitea.dev/modules/log"
	"gitea.dev/modules/optional"
	"gitea.dev/modules/setting"
	"gitea.dev/services/auth/source/oauth2"
	"gitea.dev/services/context"
)

const hostingProviderName = "bananawiki" // the goth provider name of BananaWiki Hosting (providers_custom.go)

// hostingProvider returns the first active OAuth2 source that uses the BananaWiki Hosting provider, or nil.
func hostingProvider(ctx stdctx.Context) oauth2.Provider {
	providers, err := oauth2.GetOAuth2Providers(ctx, optional.Some(true))
	if err != nil {
		log.Error("GetOAuth2Providers: %v", err)
		return nil
	}
	for _, p := range providers {
		if p.Name() == hostingProviderName {
			return p
		}
	}
	return nil
}

// checkSignInMode warns once at startup (the routes are built after the database is ready) when
// SIGNIN_MODE = hosting leaves nobody a way to sign in on the web.
func checkSignInMode() {
	if setting.BananaGit.SignInMode != setting.SignInModeHosting {
		return
	}
	if hostingProvider(graceful.GetManager().ShutdownContext()) == nil {
		setting.LogStartupProblem(1, log.WARN, "[bananagit] SIGNIN_MODE = hosting, but there is no active BananaWiki Hosting authentication source: nobody can sign in on the web. Add one (Site Administration, Authentication Sources) or set SIGNIN_MODE = both")
	}
}

// Home is the data of the home page (templates/home.tmpl), worked out only when the page asks for it.
type Home struct {
	ctx      *context.Context
	looked   bool
	provider oauth2.Provider
}

// Description is [ui.meta] DESCRIPTION, which an administrator can change.
func (h *Home) Description() string {
	return setting.UI.Meta.Description
}

// HostingProvider is the BananaWiki Hosting sign-in source, nil when there is none.
func (h *Home) HostingProvider() oauth2.Provider {
	if !h.looked {
		h.looked = true
		h.provider = hostingProvider(h.ctx)
	}
	return h.provider
}

// ShowSignIn tells whether to link Gitea's sign-in page; in hosting mode only as a fallback without the source.
func (h *Home) ShowSignIn() bool {
	return setting.BananaGit.SignInMode == setting.SignInModeBoth || h.HostingProvider() == nil
}

// ShowExplore hides "Explore repositories" when exploring needs an account anyway.
func (h *Home) ShowExplore() bool {
	return !setting.Service.RequireSignInViewStrict && !setting.Service.Explore.RequireSigninView
}

func (h *Home) WikiURL() string { return setting.BananaGit.WikiURL }
func (h *Home) ChatURL() string { return setting.BananaGit.ChatURL }
