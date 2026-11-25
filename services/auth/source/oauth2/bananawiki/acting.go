// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananawiki

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"gitea.dev/modules/bananagit"
)

// Claims of a sign-in by a hosting administrator acting as the account (SIGNIN-SPEC A2).
const (
	ClaimAct          = "act"
	ClaimActExpiresAt = "act_expires_at"
)

// MaxSubLength is the longest account id the portal issues.
const MaxSubLength = 64

// Acting is the administrator behind a sign-in made while acting as an account.
type Acting struct {
	AdminSub  string
	AdminName string
	ExpiresAt time.Time
}

// ErrInvalidActing is returned for an "act" claim that cannot be trusted to be complete.
var ErrInvalidActing = errors.New("bananawiki: invalid act claim in user information")

// ValidSub reports whether s looks like a portal account id: 1..64 printable ASCII characters without spaces.
func ValidSub(s string) bool {
	return bananagit.ValidSub(s)
}

// ActingFromRawData reads "act" and "act_expires_at" from the userinfo. It returns nil without "act" (or with
// "act": null), and ErrInvalidActing when the claim is there but incomplete: such a sign-in must be refused, never
// treated as an ordinary one.
func ActingFromRawData(raw map[string]any) (*Acting, error) {
	v, ok := raw[ClaimAct]
	if !ok || v == nil {
		return nil, nil //nolint:nilnil // not acting
	}
	act, ok := v.(map[string]any)
	if !ok {
		return nil, ErrInvalidActing
	}
	sub, _ := act["sub"].(string)
	if !ValidSub(sub) {
		return nil, ErrInvalidActing
	}
	name, _ := act["preferred_username"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		name = sub
	}
	expires, ok := unixSeconds(raw[ClaimActExpiresAt])
	if !ok {
		return nil, ErrInvalidActing
	}
	return &Acting{AdminSub: sub, AdminName: name, ExpiresAt: time.Unix(expires, 0)}, nil
}

// unixSeconds accepts a JSON number (or a string of digits) of whole Unix seconds.
func unixSeconds(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n <= 0 || n > math.MaxInt32*4 || n != math.Trunc(n) {
			return 0, false
		}
		return int64(n), true
	case int64:
		return n, n > 0
	case int:
		return int64(n), n > 0
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		return i, err == nil && i > 0
	}
	return 0, false
}

// StripActing removes the acting claims, so that they are not stored with the account link.
func StripActing(raw map[string]any) {
	delete(raw, ClaimAct)
	delete(raw, ClaimActExpiresAt)
}
