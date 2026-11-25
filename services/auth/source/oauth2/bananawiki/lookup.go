// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananawiki

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gitea.dev/modules/json"
)

// Portal account lookup for linking tools (SIGNIN-SPEC A3).
const (
	LookupPath       = "/api/v1/admin/accounts/lookup"
	LookupMaxEntries = 500 // per request, usernames and emails together

	maxLookupResponse = 4 << 20
	lookupTimeout     = 20 * time.Second
)

// Account statuses the portal reports.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusPending   = "pending"
)

// LookupAccount is one portal account found by the lookup. Email is set only when the portal verified it.
type LookupAccount struct {
	Sub           string `json:"sub"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Status        string `json:"status"`
}

// LookupResult is the merged answer to one or more lookup requests.
type LookupResult struct {
	Accounts []LookupAccount
	NotFound []string
}

// ErrLookupRefused is returned when the portal refuses the token (401/403) or the rate limit is reached (429).
var ErrLookupRefused = errors.New("bananawiki: the portal refused the account lookup")

// LookupClient asks the portal for accounts by username or verified email.
type LookupClient struct {
	BaseURL    string // the portal, e.g. https://hosting.bananawiki.com
	Token      string // portal API token of an administrator, scope accounts:lookup
	HTTPClient *http.Client
}

// PortalBaseURL derives the portal's base URL from the token endpoint of the OAuth2 source.
func PortalBaseURL(tokenURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(tokenURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("bananawiki: token URL %q is not an http(s) URL", tokenURL)
	}
	path := strings.TrimSuffix(u.Path, "/")
	path = strings.TrimSuffix(path, "/oauth/token")
	if strings.Contains(path, "/oauth/") {
		path = ""
	}
	return u.Scheme + "://" + u.Host + strings.TrimSuffix(path, "/"), nil
}

func (c *LookupClient) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{
		Timeout: lookupTimeout,
		// the token must only ever go to the configured portal
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Lookup asks for the given usernames and emails, at most LookupMaxEntries per request.
func (c *LookupClient) Lookup(ctx context.Context, usernames, emails []string) (*LookupResult, error) {
	if c.Token == "" {
		return nil, errors.New("bananawiki: no portal lookup token")
	}
	result := &LookupResult{}
	seen := map[string]bool{}
	for len(usernames) > 0 || len(emails) > 0 {
		n := min(len(usernames), LookupMaxEntries)
		batchUsers := usernames[:n]
		usernames = usernames[n:]
		m := min(len(emails), LookupMaxEntries-n)
		batchEmails := emails[:m]
		emails = emails[m:]

		part, err := c.lookupBatch(ctx, batchUsers, batchEmails)
		if err != nil {
			return nil, err
		}
		for _, a := range part.Accounts {
			if !seen[a.Sub] {
				seen[a.Sub] = true
				result.Accounts = append(result.Accounts, a)
			}
		}
		result.NotFound = append(result.NotFound, part.NotFound...)
	}
	return result, nil
}

func (c *LookupClient) lookupBatch(ctx context.Context, usernames, emails []string) (*LookupResult, error) {
	body, err := json.Marshal(map[string][]string{"usernames": nonNil(usernames), "emails": nonNil(emails)})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.BaseURL, "/")+LookupPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("bananawiki: account lookup: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return nil, fmt.Errorf("%w (HTTP %d)", ErrLookupRefused, resp.StatusCode)
	default:
		return nil, fmt.Errorf("bananawiki: account lookup answered HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxLookupResponse+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxLookupResponse {
		return nil, errors.New("bananawiki: account lookup answer is too large")
	}
	return parseLookup(data)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func parseLookup(data []byte) (*LookupResult, error) {
	var raw struct {
		Accounts []LookupAccount `json:"accounts"`
		NotFound []string        `json:"not_found"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("bananawiki: invalid account lookup answer: %w", err)
	}
	res := &LookupResult{NotFound: raw.NotFound}
	for _, a := range raw.Accounts {
		if !ValidSub(a.Sub) {
			continue // never link to an id that is not one
		}
		a.Username = strings.TrimSpace(a.Username)
		a.Status = strings.ToLower(strings.TrimSpace(a.Status))
		if a.Status == "" {
			a.Status = StatusPending // unknown means not usable
		}
		if !a.EmailVerified {
			a.Email = ""
		}
		a.Email = strings.TrimSpace(a.Email)
		res.Accounts = append(res.Accounts, a)
	}
	return res, nil
}
