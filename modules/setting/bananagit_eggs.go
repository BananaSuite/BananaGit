// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

// BananaGit: the BananaSuite easter eggs (format banana-eggs/v1), a port of the suite's eggs.py.
// The rules, limits and error codes are the same; eggs-vectors.json (testdata) holds the cases every copy
// must agree on. "Clean" with strict errors is for saving an admin's list; reading what is stored ignores the
// errors, so a bad row never breaks a page.

import (
	"bytes"
	"context"
	"encoding/json" //nolint:depguard // json.Number is needed to tell 12 from 12.0 as eggs.py does
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"gitea.dev/modules/setting/config"
)

const (
	EggVersion      = 1
	EggMaxEggs      = 10
	EggMaxBytes     = 8192
	eggMinKeys      = 2
	eggMaxKeys      = 32
	eggMaxKeyLength = 16
	eggMaxEmoji     = 16 // UTF-16 code units, as the browser counts them
	eggMaxMessage   = 200
	eggBanana       = "\U0001F34C"
)

// EggEmojis is the editor's emoji picker (eggs.EMOJIS).
var EggEmojis = []string{"🍌", "🍎", "🍕", "🧀", "🍩", "🍪", "🎉", "🎈", "⭐", "✨", "💎", "🔥", "💡", "🧩", "🎯", "🎨", "🎸", "🚀",
	"🌈", "🌻", "🦄", "🐱", "🐶", "🐸", "🐧", "🦆", "👻", "🎃", "💀", "❤️", "📚", "✏️", "⚙️", "🔧", "📦", "🪄"}

var eggKonami = []string{"ArrowUp", "ArrowUp", "ArrowDown", "ArrowDown", "ArrowLeft", "ArrowRight", "ArrowLeft", "ArrowRight", "b", "a"}

type eggNumber struct {
	name      string
	low, high float64
	def       float64
	integer   bool
}

var (
	eggNumbers = []eggNumber{
		{"count", 1, 200, 30, true},
		{"speed_min", 0.3, 20, 1.5, false},
		{"speed_max", 0.3, 20, 3.5, false},
		{"delay", 10, 2000, 80, true},
	}
	eggUnlockDelay = eggNumber{"delay", 100, 30000, 2500, true}

	eggIDPattern       = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	eggBadgePattern    = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
	eggNamedKeyPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{1,15}$`)
)

// EggSprite is what falls: an emoji, or the mascot (BananaGit has no mascot sprite, so it falls as a banana).
type EggSprite struct {
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
}

type EggUnlock struct {
	Action string `json:"action"`
	Delay  int    `json:"delay"`
}

type Egg struct {
	ID       string    `json:"id"`
	Enabled  bool      `json:"enabled"`
	Keys     []string  `json:"keys"`
	Sprite   EggSprite `json:"sprite"`
	Count    int       `json:"count"`
	SpeedMin float64   `json:"speed_min"`
	SpeedMax float64   `json:"speed_max"`
	Delay    int       `json:"delay"`
	Unlock   EggUnlock `json:"unlock"`
	Record   bool      `json:"record"`
	Badge    *string   `json:"badge"`
	Message  string    `json:"message"`
}

type EggConfig struct {
	Version int    `json:"version"`
	Eggs    []*Egg `json:"eggs"`
}

// EggError is one problem of a list that cannot be saved; Index is -1 for the list itself.
type EggError struct {
	Index int
	Field string
	Code  string
}

// BananaGitEggs are the stored settings, edited on the admin "Configuration settings" page.
var BananaGitEggs = struct {
	Enabled *config.Option[bool] // master switch for key-sequence eggs
	Config  *config.Option[any]  // the egg list; read through EggsConfig, which cleans it
}{
	Enabled: config.NewOption[bool]("bananagit.easter_eggs_enabled").WithDefaultSimple(true),
	Config:  config.NewOption[any]("bananagit.easter_eggs"),
}

// DefaultEgg is eggs.default_egg() with a banana instead of the mascot.
func DefaultEgg() *Egg {
	return &Egg{
		ID: "konami", Enabled: true, Keys: append([]string(nil), eggKonami...),
		Sprite: EggSprite{Type: "emoji", Value: eggBanana},
		Count:  10, SpeedMin: 1.6, SpeedMax: 2.1, Delay: 80,
		Unlock: EggUnlock{Action: "none", Delay: 2500}, Record: true,
	}
}

// TemplateEgg is the egg the editor's "Add" starts from: the format's defaults, without keys.
func TemplateEgg() *Egg {
	return &Egg{
		Enabled: true, Keys: []string{}, Sprite: EggSprite{Type: "emoji", Value: eggBanana},
		Count: 30, SpeedMin: 1.5, SpeedMax: 3.5, Delay: 80,
		Unlock: EggUnlock{Action: "page", Delay: 2500}, Record: true,
	}
}

func DefaultEggConfig() *EggConfig {
	return &EggConfig{Version: EggVersion, Eggs: []*Egg{DefaultEgg()}}
}

// EggsEnabled reports the master switch.
func EggsEnabled(ctx context.Context) bool {
	if config.GetDynGetter() == nil {
		return true
	}
	return BananaGitEggs.Enabled.Value(ctx)
}

var eggCache struct {
	sync.Mutex
	revision int
	loaded   bool
	config   *EggConfig
}

// EggsConfig is the stored list, cleaned (never fails), or the default egg when nothing is stored.
// The result is shared: do not change it.
func EggsConfig(ctx context.Context) *EggConfig {
	dg := config.GetDynGetter()
	if dg == nil {
		return DefaultEggConfig()
	}
	rev := dg.GetRevision(ctx)
	eggCache.Lock()
	defer eggCache.Unlock()
	if eggCache.loaded && eggCache.revision == rev {
		return eggCache.config
	}
	cfg := DefaultEggConfig()
	if raw, has := dg.GetValue(ctx, BananaGitEggs.Config.DynKey()); has {
		cfg, _ = CleanEggs(raw)
	}
	eggCache.revision, eggCache.loaded, eggCache.config = rev, true, cfg
	return cfg
}

// FindEgg returns the egg with that id, or nil.
func (c *EggConfig) FindEgg(id string) *Egg {
	for _, egg := range c.Eggs {
		if egg.ID == id {
			return egg
		}
	}
	return nil
}

// Dumps is the compact JSON that is stored.
func (c *EggConfig) Dumps() string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(c)
	return strings.TrimSuffix(buf.String(), "\n")
}

// CleanEggs cleans stored or submitted JSON text (eggs.clean): the list keeps every valid egg, and the errors
// say what a strict save must refuse.
func CleanEggs(text string) (*EggConfig, []EggError) {
	var errs []EggError
	if len(text) > EggMaxBytes {
		errs = append(errs, EggError{-1, "list", "size"})
	}
	var raw any
	if strings.TrimFunc(text, pyIsSpace) == "" {
		raw = map[string]any{"version": json.Number("1"), "eggs": []any{}}
	} else if v, err := decodeEggJSON(text); err != nil {
		errs = append(errs, EggError{-1, "list", "json"})
		raw = map[string]any{}
	} else {
		raw = v
	}
	cfg, more := CleanEggsValue(raw)
	return cfg, append(errs, more...)
}

func decodeEggJSON(text string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, strconv.ErrSyntax // extra data after the value
	}
	return v, nil
}

// CleanEggsValue cleans an already decoded value (numbers as json.Number), a dict or a bare list.
func CleanEggsValue(raw any) (*EggConfig, []EggError) {
	var errs []EggError
	if list, ok := raw.([]any); ok {
		raw = map[string]any{"version": json.Number("1"), "eggs": list}
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		errs = append(errs, EggError{-1, "list", "type"})
		obj = map[string]any{}
	}
	if version, has := obj["version"]; has && !eggIsOne(version) {
		errs = append(errs, EggError{-1, "version", "invalid"})
	}
	items := []any{}
	if v, has := obj["eggs"]; has {
		if list, ok := v.([]any); ok {
			items = list
		} else {
			errs = append(errs, EggError{-1, "eggs", "type"})
		}
	}
	if len(items) > EggMaxEggs {
		errs = append(errs, EggError{-1, "eggs", "too_many"})
	}

	cfg := &EggConfig{Version: EggVersion, Eggs: []*Egg{}}
	seen := map[string]bool{}
	for index, item := range items {
		egg, problems := cleanEgg(item, index)
		errs = append(errs, problems...)
		if egg == nil {
			continue
		}
		if seen[egg.ID] {
			errs = append(errs, EggError{index, "id", "duplicate"})
			continue
		}
		seen[egg.ID] = true
		if len(cfg.Eggs) < EggMaxEggs {
			cfg.Eggs = append(cfg.Eggs, egg)
		}
	}
	return cfg, errs
}

// eggIsOne is Python's "value == 1" for JSON values (True == 1 == 1.0).
func eggIsOne(v any) bool {
	switch v := v.(type) {
	case bool:
		return v
	case json.Number:
		f, _ := strconv.ParseFloat(string(v), 64)
		return f == 1
	}
	return false
}

func eggNumberValue(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil && !math.IsInf(f, 0) { // like Python, 1e400 is infinity (and then out of range)
		return 0, false
	}
	return f, true
}

func cleanEggNumber(obj map[string]any, spec eggNumber, errs *[]EggError, index int, prefix string) float64 {
	value := spec.def
	if v, has := obj[spec.name]; has {
		if f, ok := eggNumberValue(v); !ok {
			*errs = append(*errs, EggError{index, prefix + spec.name, "type"})
		} else if f < spec.low || f > spec.high {
			*errs = append(*errs, EggError{index, prefix + spec.name, "range"})
			value = math.Min(spec.high, math.Max(spec.low, f))
		} else {
			value = f
		}
	}
	if spec.integer {
		return math.RoundToEven(value)
	}
	rounded, _ := strconv.ParseFloat(strconv.FormatFloat(value, 'f', 2, 64), 64) // round(value, 2)
	return rounded
}

// NormaliseEggKey is a KeyboardEvent.key as eggs store it (single characters lowercased), or "" when it
// cannot be one.
func NormaliseEggKey(v any) string {
	key, ok := v.(string)
	n := utf8.RuneCountInString(key)
	if !ok || key == "" || n > eggMaxKeyLength {
		return ""
	}
	if n == 1 {
		r, _ := utf8.DecodeRuneInString(key)
		if r < 32 || r == 127 {
			return ""
		}
		return strings.ToLower(key)
	}
	if eggNamedKeyPattern.MatchString(key) {
		return key
	}
	return ""
}

func cleanEgg(raw any, index int) (*Egg, []EggError) {
	var errs []EggError
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, []EggError{{index, "egg", "type"}}
	}
	egg := &Egg{}

	if id, ok := obj["id"].(string); ok && eggIDPattern.MatchString(id) {
		egg.ID = id
	} else {
		errs = append(errs, EggError{index, "id", "invalid"})
	}

	rawKeys, ok := obj["keys"].([]any)
	if !ok {
		errs = append(errs, EggError{index, "keys", "type"})
	}
	egg.Keys = make([]string, 0, len(rawKeys))
	badKey := false
	for _, k := range rawKeys {
		key := NormaliseEggKey(k)
		badKey = badKey || key == ""
		egg.Keys = append(egg.Keys, key)
	}
	if badKey {
		errs = append(errs, EggError{index, "keys", "invalid"})
	} else if len(egg.Keys) < eggMinKeys || len(egg.Keys) > eggMaxKeys {
		errs = append(errs, EggError{index, "keys", "length"})
	}

	egg.Sprite = EggSprite{Type: "emoji", Value: eggBanana}
	spriteType := "emoji"
	var sprite map[string]any
	if v, has := obj["sprite"]; has {
		sprite, _ = v.(map[string]any)
		t, _ := sprite["type"].(string)
		if sprite == nil || (t != "emoji" && t != "mascot") {
			errs = append(errs, EggError{index, "sprite", "invalid"})
			sprite = nil
		} else {
			spriteType = t
		}
	}
	if spriteType == "mascot" {
		egg.Sprite = EggSprite{Type: "mascot"}
	} else if sprite != nil {
		value, ok := sprite["value"].(string)
		trimmed := strings.TrimFunc(value, pyIsSpace)
		if !ok || trimmed == "" || utf16Len(trimmed) > eggMaxEmoji || strings.ContainsFunc(value, isControl) ||
			strings.ContainsAny(value, "<>") {
			errs = append(errs, EggError{index, "sprite.value", "invalid"})
		} else {
			egg.Sprite.Value = trimmed
		}
	}

	numbers := make([]float64, len(eggNumbers))
	for i, spec := range eggNumbers {
		numbers[i] = cleanEggNumber(obj, spec, &errs, index, "")
	}
	egg.Count, egg.SpeedMin, egg.SpeedMax, egg.Delay = int(numbers[0]), numbers[1], numbers[2], int(numbers[3])
	if egg.SpeedMin > egg.SpeedMax {
		egg.SpeedMin, egg.SpeedMax = egg.SpeedMax, egg.SpeedMin
	}

	unlock := map[string]any{"action": "page"}
	if v, has := obj["unlock"]; has {
		if u, ok := v.(map[string]any); ok {
			unlock = u
		} else {
			errs = append(errs, EggError{index, "unlock", "type"})
			unlock = map[string]any{}
		}
	}
	egg.Unlock.Action = "page"
	if v, has := unlock["action"]; has {
		if action, _ := v.(string); action == "page" || action == "toast" || action == "none" {
			egg.Unlock.Action = action
		} else {
			errs = append(errs, EggError{index, "unlock.action", "invalid"})
		}
	}
	egg.Unlock.Delay = int(cleanEggNumber(unlock, eggUnlockDelay, &errs, index, "unlock."))

	egg.Enabled, egg.Record = true, true
	for _, flag := range []struct {
		name string
		dst  *bool
	}{{"enabled", &egg.Enabled}, {"record", &egg.Record}} {
		if v, has := obj[flag.name]; has {
			if b, ok := v.(bool); ok {
				*flag.dst = b
			} else {
				errs = append(errs, EggError{index, flag.name, "type"})
			}
		}
	}

	switch badge := obj["badge"].(type) {
	case nil:
	case string:
		if badge == "" {
			break
		}
		if eggBadgePattern.MatchString(badge) {
			egg.Badge = &badge
		} else {
			errs = append(errs, EggError{index, "badge", "invalid"})
		}
	case json.Number:
		// a positive integer (not 12.0) becomes its text, as str(12) in Python
		s := string(badge)
		if !strings.ContainsAny(s, ".eE-") && s != "0" {
			egg.Badge = &s
		} else {
			errs = append(errs, EggError{index, "badge", "invalid"})
		}
	default:
		errs = append(errs, EggError{index, "badge", "invalid"})
	}

	message := ""
	if v, has := obj["message"]; has {
		s, ok := v.(string)
		if !ok || utf8.RuneCountInString(s) > eggMaxMessage ||
			strings.ContainsFunc(s, func(r rune) bool { return r < 32 && r != '\t' }) {
			errs = append(errs, EggError{index, "message", "invalid"})
			if ok {
				s = truncateRunes(s, eggMaxMessage)
			}
		}
		message = s
	}
	egg.Message = strings.Join(strings.FieldsFunc(message, pyIsSpace), " ")

	for _, e := range errs {
		if e.Field == "egg" || e.Field == "id" || e.Field == "keys" {
			return nil, errs
		}
	}
	return egg, errs
}

// pyIsSpace is Python's str.isspace for one character (str.split and str.strip use it).
func pyIsSpace(r rune) bool {
	switch {
	case r >= '\t' && r <= '\r', r >= 0x1c && r <= ' ', r == 0x85, r == 0xa0, r == 0x1680,
		r >= 0x2000 && r <= 0x200a, r == 0x2028, r == 0x2029, r == 0x202f, r == 0x205f, r == 0x3000:
		return true
	}
	return false
}

func isControl(r rune) bool { return r < 32 || r == 127 }

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xffff {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func truncateRunes(s string, n int) string {
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}

// EggClientEgg is an egg as the page gets it: no badge.
type EggClientEgg struct {
	ID       string    `json:"id"`
	Enabled  bool      `json:"enabled"`
	Keys     []string  `json:"keys"`
	Sprite   EggSprite `json:"sprite"`
	Count    int       `json:"count"`
	SpeedMin float64   `json:"speed_min"`
	SpeedMax float64   `json:"speed_max"`
	Delay    int       `json:"delay"`
	Unlock   EggUnlock `json:"unlock"`
	Record   bool      `json:"record"`
	Message  string    `json:"message"`
}

type EggClientCSRF struct {
	Header string `json:"header"`
	Token  string `json:"token"`
}

// EggClientConfig is what the page puts in <script type="application/json" id="banana-eggs">.
type EggClientConfig struct {
	Version      int               `json:"version"`
	Eggs         []EggClientEgg    `json:"eggs"`
	SignedIn     bool              `json:"signedIn"`
	FoundURL     string            `json:"foundUrl"`
	PageURL      string            `json:"pageUrl"`
	CSRF         *EggClientCSRF    `json:"csrf"`
	ReduceMotion bool              `json:"reduceMotion"`
	Strings      map[string]string `json:"strings"`
}

// ClientConfig is eggs.client_config: enabled eggs only, badges stay on the server. foundURL keeps a literal
// "{id}"; csrfHeader "" sends no header (BananaGit protects POSTs by checking the request's origin).
func (c *EggConfig) ClientConfig(foundURL, pageURL string, signedIn bool, csrfHeader, csrfToken string, strs map[string]string) *EggClientConfig {
	out := &EggClientConfig{Version: EggVersion, Eggs: []EggClientEgg{}, SignedIn: signedIn, FoundURL: foundURL,
		PageURL: pageURL, Strings: strs}
	if out.Strings == nil {
		out.Strings = map[string]string{}
	}
	if csrfHeader != "" {
		out.CSRF = &EggClientCSRF{Header: csrfHeader, Token: csrfToken}
	}
	for _, e := range c.Eggs {
		if e.Enabled {
			out.Eggs = append(out.Eggs, EggClientEgg{ID: e.ID, Enabled: e.Enabled, Keys: e.Keys, Sprite: e.Sprite,
				Count: e.Count, SpeedMin: e.SpeedMin, SpeedMax: e.SpeedMax, Delay: e.Delay, Unlock: e.Unlock,
				Record: e.Record, Message: e.Message})
		}
	}
	return out
}

// EggScriptJSON is JSON that is safe inside a <script type="application/json"> element (eggs.script_json).
func EggScriptJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "null"
	}
	var out strings.Builder
	for _, r := range strings.TrimSuffix(buf.String(), "\n") {
		switch r {
		case '<', '>', '&', 0x2028, 0x2029: // only inside strings in JSON text, where an escape means the same
			fmt.Fprintf(&out, "%cu%04x", '\\', r)
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}
