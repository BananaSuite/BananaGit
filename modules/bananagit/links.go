// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananagit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Linking modes of the Account links tool (SIGNIN-SPEC B).
const (
	ModeUsername = "username" // same username, case-insensitive
	ModeEmail    = "email"    // same verified email
	ModeBoth     = "both"     // username and verified email both match the same hosting account
	ModeList     = "list"     // pasted list local_username,hosting_username (or hosting_sub); one row links one user
)

// What the second column of a list names.
const (
	ListByUsername = "username"
	ListBySub      = "sub"
)

// MaxListRows bounds a pasted list, like the portal bounds one lookup.
const MaxListRows = 500

// Row states of a preview.
const (
	StateWillLink      = "will_link"
	StateAlreadyLinked = "already_linked"
	StateConflict      = "conflict"
	StateNotFound      = "not_found"
	StateSkipped       = "skipped"
)

// Row reasons, each a locale key under bananagit.links.reason.
const (
	ReasonAdmin              = "admin"                // skipped: administrators need "include administrators"
	ReasonDisabled           = "disabled"             // skipped: inactive or prohibited local account
	ReasonHostingLinkedOther = "hosting_linked_other" // conflict: hosting account linked to another local user
	ReasonLocalLinkedOther   = "local_linked_other"   // conflict: local user linked to another hosting account
	ReasonSeveral            = "several_candidates"   // conflict: more than one hosting account fits
	ReasonSameTarget         = "same_target"          // conflict: one hosting account chosen for several local users
	ReasonSameLocal          = "same_local"           // conflict: one local user named in several list rows
	ReasonSuspended          = "suspended"            // conflict: hosting account suspended
	ReasonPending            = "pending"              // conflict: hosting account not active yet
	ReasonNoMatch            = "no_match"             // not found at the portal
	ReasonNoLocalEmail       = "no_local_email"       // not found: the local user has no verified email
	ReasonOnlyUsername       = "only_username"        // not found (both): the email did not match
	ReasonOnlyEmail          = "only_email"           // not found (both): the username did not match
	ReasonUnknownLocal       = "unknown_local"        // not found: no such local user
	ReasonInvalidLine        = "invalid_line"         // not found: the list line cannot be read
	ReasonUnchecked          = "unchecked"            // will link: a sub entered by hand, not checked with the portal
)

// Request is what the administrator asked for; it is all an Apply needs to recompute the preview.
type Request struct {
	Mode          string
	IncludeAdmins bool
	ListKind      string
	List          string
}

// LocalUser is a BananaGit account as the planner sees it.
type LocalUser struct {
	ID        int64
	Name      string
	Email     string // primary email, only when activated
	IsAdmin   bool
	Disabled  bool
	LinkedSub string // the hosting account it is linked to, or ""
}

// Account is a hosting account (from the portal lookup, or a sub entered by hand with Unchecked).
type Account struct {
	Sub       string
	Username  string
	Email     string
	Status    string // active, suspended, pending
	Unchecked bool
}

// Row is one line of a preview.
type Row struct {
	Line      int    `json:"line,omitempty"` // list mode: line number in the pasted list
	UserID    int64  `json:"uid,omitempty"`
	LocalName string `json:"local"`
	Sub       string `json:"sub,omitempty"`
	Hosting   string `json:"hosting,omitempty"`
	Email     string `json:"email,omitempty"`
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	Other     string `json:"other,omitempty"` // the other local user (hosting_linked_other) or sub (local_linked_other)
}

// ListEntry is one line of a pasted list.
type ListEntry struct {
	Line   int
	Local  string
	Target string
	Err    bool
}

var (
	ErrListTooLong = fmt.Errorf("at most %d rows", MaxListRows)
	ErrBadRequest  = errors.New("invalid linking request")
)

// Normalize checks the request and puts it in its canonical form.
func (r *Request) Normalize() error {
	r.Mode = strings.TrimSpace(strings.ToLower(r.Mode))
	switch r.Mode {
	case ModeUsername, ModeEmail, ModeBoth:
		r.ListKind, r.List = "", ""
	case ModeList:
		if r.ListKind != ListByUsername && r.ListKind != ListBySub {
			return ErrBadRequest
		}
		r.List = strings.TrimSpace(strings.ReplaceAll(r.List, "\r\n", "\n"))
		if r.List == "" {
			return ErrBadRequest
		}
	default:
		return ErrBadRequest
	}
	return nil
}

// ParseList reads "local_username,hosting_username" (or ",hosting_sub") lines; commas, semicolons or tabs separate
// the columns, empty lines and lines starting with # are ignored, and a first line naming the columns is a header.
func ParseList(list, kind string) ([]ListEntry, error) {
	var entries []ListEntry
	first := true
	for i, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == ';' || r == '\t' })
		for j := range fields {
			fields[j] = strings.Trim(strings.TrimSpace(fields[j]), `"`)
		}
		if first && len(fields) > 0 && strings.EqualFold(fields[0], "local_username") {
			first = false
			continue
		}
		first = false
		e := ListEntry{Line: i + 1}
		if len(fields) != 2 || fields[0] == "" || fields[1] == "" || len(fields[0]) > 255 {
			e.Err = true
			if len(fields) > 0 {
				e.Local = fields[0]
			}
		} else {
			e.Local, e.Target = fields[0], fields[1]
			if kind == ListBySub && !ValidSub(e.Target) || kind == ListByUsername && len(e.Target) > 255 {
				e.Err = true
			}
		}
		if len(e.Local) > 255 {
			e.Local = e.Local[:255]
		}
		entries = append(entries, e)
		if len(entries) > MaxListRows {
			return nil, ErrListTooLong
		}
	}
	return entries, nil
}

// LookupKeys are what to ask the portal for: usernames and emails.
func LookupKeys(req Request, users []LocalUser, entries []ListEntry) (usernames, emails []string) {
	add := func(list []string, seen map[string]bool, v string) []string {
		k := strings.ToLower(v)
		if v == "" || seen[k] {
			return list
		}
		seen[k] = true
		return append(list, v)
	}
	seenU, seenE := map[string]bool{}, map[string]bool{}
	switch req.Mode {
	case ModeList:
		if req.ListKind == ListByUsername {
			for _, e := range entries {
				if !e.Err {
					usernames = add(usernames, seenU, e.Target)
				}
			}
		}
	default:
		for _, u := range users {
			if u.Disabled || u.IsAdmin && !req.IncludeAdmins && u.LinkedSub == "" {
				continue // skipped anyway
			}
			if req.Mode == ModeUsername || req.Mode == ModeBoth {
				usernames = add(usernames, seenU, u.Name)
			}
			if req.Mode == ModeEmail || req.Mode == ModeBoth {
				emails = add(emails, seenE, u.Email)
			}
		}
	}
	return usernames, emails
}

// Input is everything the planner works from.
type Input struct {
	Users     []LocalUser      // bulk: every local user; list: the users the list names
	Entries   []ListEntry      // list mode
	Accounts  []Account        // portal lookup answer (list by sub: the subs, Unchecked)
	LinkOwner map[string]int64 // sub -> local user linked to it, for every link of the source
	UserNames map[int64]string // names of the users in LinkOwner, to explain conflicts
}

// Plan computes the preview rows. Nothing is linked here.
func Plan(req Request, in Input) []Row {
	byName := map[string][]Account{}
	byEmail := map[string][]Account{}
	bySub := map[string]Account{}
	for _, a := range in.Accounts {
		byName[strings.ToLower(a.Username)] = append(byName[strings.ToLower(a.Username)], a)
		if a.Email != "" {
			byEmail[strings.ToLower(a.Email)] = append(byEmail[strings.ToLower(a.Email)], a)
		}
		bySub[a.Sub] = a
	}

	var rows []Row
	if req.Mode == ModeList {
		rows = planList(req, in, byName, bySub)
	} else {
		users := append([]LocalUser(nil), in.Users...)
		sort.SliceStable(users, func(i, j int) bool { return strings.ToLower(users[i].Name) < strings.ToLower(users[j].Name) })
		for _, u := range users {
			rows = append(rows, planUser(req, in, u, byName, byEmail))
		}
	}

	// one hosting account chosen for several local users: none of them is linked
	targets := map[string]int{}
	for _, r := range rows {
		if r.State == StateWillLink {
			targets[r.Sub]++
		}
	}
	for i := range rows {
		if rows[i].State == StateWillLink && targets[rows[i].Sub] > 1 {
			rows[i].State, rows[i].Reason = StateConflict, ReasonSameTarget
		}
	}
	return rows
}

func planUser(req Request, in Input, u LocalUser, byName, byEmail map[string][]Account) Row {
	row := Row{UserID: u.ID, LocalName: u.Name}
	var cand []Account
	reason := ReasonNoMatch
	switch req.Mode {
	case ModeUsername:
		cand = byName[strings.ToLower(u.Name)]
	case ModeEmail:
		if u.Email == "" {
			reason = ReasonNoLocalEmail
		} else {
			cand = byEmail[strings.ToLower(u.Email)]
		}
	case ModeBoth:
		named := byName[strings.ToLower(u.Name)]
		var mailed []Account
		if u.Email != "" {
			mailed = byEmail[strings.ToLower(u.Email)]
		}
		switch {
		case len(named) == 0 && len(mailed) == 0:
			if u.Email == "" {
				reason = ReasonNoLocalEmail
			}
		case len(mailed) == 0:
			reason = iif(u.Email == "", ReasonNoLocalEmail, ReasonOnlyUsername)
		case len(named) == 0:
			reason = ReasonOnlyEmail
		default:
			cand = unionBySub(named, mailed)
		}
	}
	return judge(req, in, u, row, cand, reason)
}

func planList(req Request, in Input, byName map[string][]Account, bySub map[string]Account) []Row {
	users := map[string]LocalUser{}
	for _, u := range in.Users {
		users[strings.ToLower(u.Name)] = u
	}
	count := map[string]int{}
	for _, e := range in.Entries {
		if !e.Err {
			count[strings.ToLower(e.Local)]++
		}
	}
	var rows []Row
	for _, e := range in.Entries {
		row := Row{Line: e.Line, LocalName: e.Local}
		if e.Err {
			row.State, row.Reason = StateNotFound, ReasonInvalidLine
			rows = append(rows, row)
			continue
		}
		u, ok := users[strings.ToLower(e.Local)]
		if !ok {
			row.State, row.Reason = StateNotFound, ReasonUnknownLocal
			row.Hosting = e.Target
			rows = append(rows, row)
			continue
		}
		row.UserID, row.LocalName = u.ID, u.Name
		if count[strings.ToLower(e.Local)] > 1 {
			row.State, row.Reason = StateConflict, ReasonSameLocal
			rows = append(rows, row)
			continue
		}
		var cand []Account
		if req.ListKind == ListBySub {
			if a, ok := bySub[e.Target]; ok {
				cand = []Account{a}
			} else {
				cand = []Account{{Sub: e.Target, Unchecked: true}}
			}
		} else {
			cand = byName[strings.ToLower(e.Target)]
			row.Hosting = e.Target
		}
		rows = append(rows, judge(req, in, u, row, cand, ReasonNoMatch))
	}
	return rows
}

// judge turns the candidates of one local user into a row.
func judge(req Request, in Input, u LocalUser, row Row, cand []Account, noMatch string) Row {
	if u.LinkedSub != "" {
		row.Sub = u.LinkedSub
		for _, a := range cand {
			if a.Sub == u.LinkedSub {
				row.Hosting = a.Username
				row.State = StateAlreadyLinked
				return row
			}
		}
		if len(cand) > 0 {
			row.State, row.Reason, row.Other = StateConflict, ReasonLocalLinkedOther, u.LinkedSub
			row.Sub, row.Hosting = cand[0].Sub, cand[0].Username
			return row
		}
		row.State = StateAlreadyLinked
		return row
	}
	if u.Disabled {
		row.State, row.Reason = StateSkipped, ReasonDisabled
		return row
	}
	if u.IsAdmin && !req.IncludeAdmins {
		row.State, row.Reason = StateSkipped, ReasonAdmin
		return row
	}
	switch len(cand) {
	case 0:
		row.State, row.Reason = StateNotFound, noMatch
		return row
	case 1:
	default:
		row.State, row.Reason = StateConflict, ReasonSeveral
		return row
	}
	a := cand[0]
	row.Sub, row.Hosting, row.Email = a.Sub, a.Username, a.Email
	if !a.Unchecked && a.Status != "active" {
		row.State, row.Reason = StateConflict, iif(a.Status == "suspended", ReasonSuspended, ReasonPending)
		return row
	}
	if owner, ok := in.LinkOwner[a.Sub]; ok && owner != u.ID {
		row.State, row.Reason, row.Other = StateConflict, ReasonHostingLinkedOther, in.UserNames[owner]
		return row
	}
	row.State = StateWillLink
	if a.Unchecked {
		row.Reason = ReasonUnchecked
	}
	return row
}

func unionBySub(lists ...[]Account) []Account {
	seen := map[string]bool{}
	var out []Account
	for _, list := range lists {
		for _, a := range list {
			if !seen[a.Sub] {
				seen[a.Sub] = true
				out = append(out, a)
			}
		}
	}
	return out
}

func iif(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// PreviewToken identifies one preview exactly: Apply proceeds only when the recomputed preview has the same token.
func PreviewToken(sourceID int64, req Request, rows []Row) string {
	data, _ := json.Marshal(struct {
		Source int64   `json:"source"`
		Req    Request `json:"req"`
		Rows   []Row   `json:"rows"`
	}{sourceID, req, rows})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Counts tallies the rows by state.
func Counts(rows []Row) map[string]int {
	c := map[string]int{}
	for _, r := range rows {
		c[r.State]++
	}
	return c
}
