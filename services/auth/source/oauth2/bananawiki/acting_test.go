// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananawiki

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActingFromUserinfo(t *testing.T) {
	parse := func(t *testing.T, profile string) (map[string]any, error) {
		user, err := fetchProfile(t, http.StatusOK, profile)
		return user.RawData, err
	}

	t.Run("NotActing", func(t *testing.T) {
		raw, err := parse(t, `{"sub":"acct-1","preferred_username":"alice"}`)
		require.NoError(t, err)
		act, err := ActingFromRawData(raw)
		require.NoError(t, err)
		assert.Nil(t, act)
	})
	t.Run("NullAct", func(t *testing.T) {
		raw, err := parse(t, `{"sub":"acct-1","act":null}`)
		require.NoError(t, err)
		act, err := ActingFromRawData(raw)
		require.NoError(t, err)
		assert.Nil(t, act)
	})
	t.Run("Acting", func(t *testing.T) {
		raw, err := parse(t, `{"sub":"acct-1","preferred_username":"alice","act":{"sub":"acct-root","preferred_username":"root"},"act_expires_at":1795000000}`)
		require.NoError(t, err)
		act, err := ActingFromRawData(raw)
		require.NoError(t, err)
		require.NotNil(t, act)
		assert.Equal(t, "acct-root", act.AdminSub)
		assert.Equal(t, "root", act.AdminName)
		assert.Equal(t, time.Unix(1795000000, 0), act.ExpiresAt)

		StripActing(raw)
		assert.NotContains(t, raw, ClaimAct)
		assert.NotContains(t, raw, ClaimActExpiresAt)
		assert.Equal(t, "acct-1", raw["sub"])
	})
	t.Run("AdminNameFallsBackToSub", func(t *testing.T) {
		act, err := ActingFromRawData(map[string]any{"act": map[string]any{"sub": "acct-root"}, "act_expires_at": "1795000000"})
		require.NoError(t, err)
		assert.Equal(t, "acct-root", act.AdminName)
	})
	for name, profile := range map[string]string{
		"ActNotAnObject":    `{"sub":"acct-1","act":"root","act_expires_at":1795000000}`,
		"ActWithoutSub":     `{"sub":"acct-1","act":{"preferred_username":"root"},"act_expires_at":1795000000}`,
		"ActWithNumericSub": `{"sub":"acct-1","act":{"sub":7},"act_expires_at":1795000000}`,
		"WithoutExpiry":     `{"sub":"acct-1","act":{"sub":"acct-root"}}`,
		"FractionalExpiry":  `{"sub":"acct-1","act":{"sub":"acct-root"},"act_expires_at":1795000000.5}`,
		"NegativeExpiry":    `{"sub":"acct-1","act":{"sub":"acct-root"},"act_expires_at":-1}`,
		"ExpiryNotANumber":  `{"sub":"acct-1","act":{"sub":"acct-root"},"act_expires_at":"soon"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parse(t, profile)
			require.ErrorIs(t, err, ErrInvalidActing, "an incomplete act claim must fail the sign-in")
		})
	}
}

func TestValidSub(t *testing.T) {
	assert.True(t, ValidSub("3f0c9a"))
	assert.True(t, ValidSub(strings.Repeat("a", MaxSubLength)))
	assert.False(t, ValidSub(""))
	assert.False(t, ValidSub(strings.Repeat("a", MaxSubLength+1)))
	assert.False(t, ValidSub("a b"))
	assert.False(t, ValidSub("a\nb"))
	assert.False(t, ValidSub("ä"))
}
