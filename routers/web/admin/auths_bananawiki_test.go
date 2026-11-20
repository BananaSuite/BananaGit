// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package admin

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBananaWikiSourceName(t *testing.T) {
	for _, name := range []string{"BananaWiki", "Banana-Wiki_2.0"} {
		assert.True(t, bananaWikiSourceName.MatchString(name), name)
	}
	for _, name := range []string{"BananaWiki Auth", "", "a/b", "é", "Banana%20Wiki"} {
		assert.False(t, bananaWikiSourceName.MatchString(name), name)
	}
}
