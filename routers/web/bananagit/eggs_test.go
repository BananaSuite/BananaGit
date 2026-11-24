// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananagit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEggRateLimiter(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	l := newRateLimiter(10, time.Minute)
	l.now = func() time.Time { return now }
	for i := range 10 {
		assert.True(t, l.Allow(1), "request %d", i+1)
	}
	assert.False(t, l.Allow(1))
	assert.True(t, l.Allow(2), "accounts are counted apart")
	now = now.Add(61 * time.Second)
	assert.True(t, l.Allow(1))
	assert.Len(t, l.hits[1], 1)
}

func TestEggBadgeSlug(t *testing.T) {
	assert.True(t, eggBadgeSlug("gold-1.v2"))
	assert.False(t, eggBadgeSlug(""))
	assert.False(t, eggBadgeSlug("with space"))
}
