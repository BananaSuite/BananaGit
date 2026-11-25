// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananagit

import (
	"testing"

	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/services/auth/source/oauth2"

	"github.com/stretchr/testify/assert"
)

func TestHomeShowSignIn(t *testing.T) {
	defer test.MockVariableValue(&setting.BananaGit)()
	hosting := &oauth2.AuthSourceProvider{}

	for _, c := range []struct {
		mode     string
		provider oauth2.Provider
		want     bool
	}{
		{setting.SignInModeBoth, nil, true},
		{setting.SignInModeBoth, hosting, true},
		{setting.SignInModeHosting, hosting, false},
		{setting.SignInModeHosting, nil, true}, // no BananaWiki source: keep a way to the sign-in page
	} {
		setting.BananaGit.SignInMode = c.mode
		h := &Home{looked: true, provider: c.provider}
		assert.Equal(t, c.want, h.ShowSignIn(), "%s, provider %v", c.mode, c.provider != nil)
	}
}
