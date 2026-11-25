// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

// BananaGit: the [bananagit] section. SIGNIN_MODE = hosting makes the BananaWiki Hosting OAuth2 source the only
// way to sign in on the web; WIKI_URL and CHAT_URL are links to the other BananaSuite apps on the home page;
// ALLOW_ACTING_ADMINS, HOSTING_SKIPS_LOCAL_2FA and PORTAL_LOOKUP_TOKEN belong to the sign-in batch
// (bananagit/docs/bananawiki-hosting-login.md).

import (
	"net/url"
	"strings"

	"gitea.dev/modules/log"
)

const (
	SignInModeBoth    = "both"    // Gitea's own sign-in methods, as configured in [service] and [openid]
	SignInModeHosting = "hosting" // only the BananaWiki Hosting OAuth2 source
)

var BananaGit = struct {
	SignInMode string
	WikiURL    string
	ChatURL    string

	AllowActingAdmins    bool   // accept sign-ins of hosting administrators acting as an account ("act" in userinfo)
	HostingSkipsLocal2FA bool   // sign-ins through the BananaWiki Hosting source skip BananaGit's own two-step check
	PortalLookupToken    string // portal API token (scope accounts:lookup) for the Account links tool; empty: manual sub only
}{
	SignInMode:           SignInModeBoth,
	AllowActingAdmins:    true,
	HostingSkipsLocal2FA: true,
}

// loadBananaGitFrom runs at the end of loadServiceFrom, so that SIGNIN_MODE overrides [service] and [openid].
func loadBananaGitFrom(rootCfg ConfigProvider) {
	sec := rootCfg.Section("bananagit")
	mode := strings.ToLower(strings.TrimSpace(sec.Key("SIGNIN_MODE").MustString(SignInModeBoth)))
	if mode != SignInModeBoth && mode != SignInModeHosting {
		LogStartupProblem(1, log.ERROR, "[bananagit] SIGNIN_MODE = %q is not valid (both or hosting), using both", mode)
		mode = SignInModeBoth
	}
	BananaGit.SignInMode = mode
	BananaGit.WikiURL = bananaGitAppURL(sec, "WIKI_URL")
	BananaGit.ChatURL = bananaGitAppURL(sec, "CHAT_URL")
	BananaGit.AllowActingAdmins = sec.Key("ALLOW_ACTING_ADMINS").MustBool(true)
	BananaGit.HostingSkipsLocal2FA = sec.Key("HOSTING_SKIPS_LOCAL_2FA").MustBool(true)
	BananaGit.PortalLookupToken = strings.TrimSpace(loadSecret(sec, "PORTAL_LOOKUP_TOKEN_URI", "PORTAL_LOOKUP_TOKEN"))

	if mode != SignInModeHosting {
		return
	}
	Service.EnablePasswordSignInForm = false
	Service.EnablePasskeyAuth = false
	Service.EnableOpenIDSignIn = false
	Service.EnableOpenIDSignUp = false
	Service.EnableAutoOAuth2Redirect = false // show the sign-in page with its button, not a silent redirect

	if !InstallLock {
		return
	}
	// DISABLE_REGISTRATION = true is a choice (accounts by administrators only): their owners connect them at the first sign-in
	if !Service.DisableRegistration && !Service.AllowOnlyExternalRegistration {
		LogStartupProblem(1, log.WARN, "[bananagit] SIGNIN_MODE = hosting, but the local sign-up page still creates password accounts: set [service] ALLOW_ONLY_EXTERNAL_REGISTRATION = true")
	}
}

// bananaGitAppURL reads an absolute http(s) URL; anything else is reported and left out.
func bananaGitAppURL(sec ConfigSection, key string) string {
	s := strings.TrimSpace(sec.Key(key).String())
	if s == "" {
		return ""
	}
	if u, err := url.Parse(s); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		LogStartupProblem(1, log.ERROR, "[bananagit] %s = %q is not an http(s) URL, the link is hidden", key, s)
		return ""
	}
	return s
}
