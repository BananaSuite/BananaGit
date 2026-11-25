// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"os"
	"testing"

	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadBananaGitTestConfig(t *testing.T, ini string) {
	cfg, err := NewConfigProviderFromData(ini)
	require.NoError(t, err)
	loadServiceFrom(cfg)
}

func TestBananaGitSignInMode(t *testing.T) {
	defer test.MockVariableValue(&Service)()
	defer test.MockVariableValue(&BananaGit)()
	defer test.MockVariableValue(&InstallLock, true)()
	defer test.MockVariableValue(&StartupProblems, nil)()

	const methods = `
[service]
ENABLE_PASSWORD_SIGNIN_FORM = true
ENABLE_PASSKEY_AUTHENTICATION = true
ENABLE_AUTO_OAUTH2_REDIRECT = true
ALLOW_ONLY_EXTERNAL_REGISTRATION = true
[openid]
ENABLE_OPENID_SIGNIN = true
ENABLE_OPENID_SIGNUP = true
`
	t.Run("default is both, nothing changes", func(t *testing.T) {
		loadBananaGitTestConfig(t, methods)
		assert.Equal(t, SignInModeBoth, BananaGit.SignInMode)
		assert.True(t, Service.EnablePasswordSignInForm)
		assert.True(t, Service.EnablePasskeyAuth)
		assert.True(t, Service.EnableOpenIDSignIn)
		assert.True(t, Service.EnableOpenIDSignUp)
		assert.True(t, Service.EnableAutoOAuth2Redirect)
	})

	t.Run("hosting turns the other methods off", func(t *testing.T) {
		StartupProblems = nil
		loadBananaGitTestConfig(t, methods+"[bananagit]\nSIGNIN_MODE = Hosting\n")
		assert.Equal(t, SignInModeHosting, BananaGit.SignInMode)
		assert.False(t, Service.EnablePasswordSignInForm)
		assert.False(t, Service.EnablePasskeyAuth)
		assert.False(t, Service.EnableOpenIDSignIn)
		assert.False(t, Service.EnableOpenIDSignUp)
		assert.False(t, Service.EnableAutoOAuth2Redirect)
		assert.Empty(t, StartupProblems)
	})

	t.Run("an unknown mode falls back to both", func(t *testing.T) {
		StartupProblems = nil
		loadBananaGitTestConfig(t, methods+"[bananagit]\nSIGNIN_MODE = wiki\n")
		assert.Equal(t, SignInModeBoth, BananaGit.SignInMode)
		assert.True(t, Service.EnablePasswordSignInForm)
		assert.Len(t, StartupProblems, 1)
	})

	t.Run("hosting warns when new people cannot get an account", func(t *testing.T) {
		StartupProblems = nil
		loadBananaGitTestConfig(t, "[service]\nDISABLE_REGISTRATION = true\n[bananagit]\nSIGNIN_MODE = hosting\n")
		require.Len(t, StartupProblems, 1)
		assert.Contains(t, StartupProblems[0], "DISABLE_REGISTRATION")
	})

	t.Run("hosting warns when local sign-up stays open", func(t *testing.T) {
		StartupProblems = nil
		loadBananaGitTestConfig(t, "[bananagit]\nSIGNIN_MODE = hosting\n")
		require.Len(t, StartupProblems, 1)
		assert.Contains(t, StartupProblems[0], "ALLOW_ONLY_EXTERNAL_REGISTRATION")
	})
}

func TestBananaGitAppURLs(t *testing.T) {
	defer test.MockVariableValue(&Service)()
	defer test.MockVariableValue(&BananaGit)()
	defer test.MockVariableValue(&StartupProblems, nil)()

	cases := map[string]string{
		"":                                 "",
		"https://wiki.example.org":         "https://wiki.example.org",
		" http://chat.example.org/x?y ":    "http://chat.example.org/x?y",
		"javascript:alert(1)":              "",
		"//wiki.example.org":               "",
		"ftp://wiki.example.org":           "",
		"https://user:pw@wiki.example.org": "",
		"https://":                         "",
	}
	for in, want := range cases {
		loadBananaGitTestConfig(t, "[bananagit]\nWIKI_URL = "+in+"\nCHAT_URL = "+in+"\n")
		assert.Equal(t, want, BananaGit.WikiURL, "WIKI_URL = %q", in)
		assert.Equal(t, want, BananaGit.ChatURL, "CHAT_URL = %q", in)
	}
}

func TestBananaGitSignInBatch(t *testing.T) {
	defer test.MockVariableValue(&Service)()
	defer test.MockVariableValue(&BananaGit)()
	defer test.MockVariableValue(&StartupProblems, nil)()

	t.Run("defaults", func(t *testing.T) {
		loadBananaGitTestConfig(t, "")
		assert.True(t, BananaGit.AllowActingAdmins)
		assert.True(t, BananaGit.HostingSkipsLocal2FA)
		assert.Empty(t, BananaGit.PortalLookupToken)
	})

	t.Run("set", func(t *testing.T) {
		loadBananaGitTestConfig(t, "[bananagit]\nALLOW_ACTING_ADMINS = false\nHOSTING_SKIPS_LOCAL_2FA = false\nPORTAL_LOOKUP_TOKEN = \" bwt_abc \"\n")
		assert.False(t, BananaGit.AllowActingAdmins)
		assert.False(t, BananaGit.HostingSkipsLocal2FA)
		assert.Equal(t, "bwt_abc", BananaGit.PortalLookupToken)
	})

	t.Run("token from a file", func(t *testing.T) {
		file := t.TempDir() + "/token"
		require.NoError(t, os.WriteFile(file, []byte("bwt_from_file\n"), 0o600))
		loadBananaGitTestConfig(t, "[bananagit]\nPORTAL_LOOKUP_TOKEN_URI = file:"+file+"\n")
		assert.Equal(t, "bwt_from_file", BananaGit.PortalLookupToken)
	})
}
