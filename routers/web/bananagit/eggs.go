// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package bananagit holds BananaGit's own web routes and page data: the BananaSuite easter eggs (banana-eggs/v1),
// the home page, the acting banner and guard, and the Account links admin page.
package bananagit

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"gitea.dev/models/db"
	system_model "gitea.dev/models/system"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/log"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/setting/config"
	"gitea.dev/modules/templates"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
	"gitea.dev/modules/web"
	bananagit_service "gitea.dev/services/bananagit"
	"gitea.dev/services/context"
)

const (
	tplEggs           templates.TplName = "bananagit/eggs"
	tplConfigSettings templates.TplName = "admin/config_settings/config_settings"

	eggFindPrefix = "bananagit.egg." // user_setting key of a find; the value is the Unix time of the first find
	eggsPagePath  = "/-/bananagit/eggs"
	eggsAdminPath = "/-/admin/config/bananagit-eggs"
)

// editorStrings are the shared editor's string keys (SPEC.md), each under "bananagit.eggs.editor.".
var editorStrings = []string{"title", "add", "remove", "preview", "reset", "record", "stop", "konami", "keys",
	"keys_hint", "keys_empty", "key_space", "id", "enabled", "sprite", "sprite_emoji", "sprite_mascot", "emoji",
	"emoji_pick", "count", "speed_min", "speed_max", "delay", "unlock", "unlock_page", "unlock_toast", "unlock_none",
	"unlock_delay", "record_find", "badge", "badge_none", "message", "limit"}

// RegisterRoutes adds BananaGit's routes, the page data every template can use and the acting guard (BananaGit's
// one hook in routers/web/web.go). The middlewares are web.go's own.
func RegisterRoutes(m *web.Router, optSignIn, reqSignIn, adminReq func(*context.Context)) {
	checkSignInMode()
	m.AfterRouting(pageData, bananagit_service.ActingGuard)
	m.Get(eggsPagePath, optSignIn, EggsPage)
	m.Post(eggsPagePath+"/{id}/found", reqSignIn, EggFound)
	m.Post(eggsAdminPath, adminReq, AdminSave)
	registerAccountLinks(m, adminReq)
}

func pageData(ctx *context.Context) {
	ctx.Data["BananaGitEggs"] = &Eggs{ctx: ctx}
	ctx.Data["BananaGitHome"] = &Home{ctx: ctx}
	ctx.Data["BananaGitSignIn"] = &SignIn{ctx: ctx}
}

// Eggs is what templates use (base/head through bananagit/eggs_head, the admin settings section); everything
// is worked out only when a template asks for it.
type Eggs struct {
	ctx *context.Context
}

// On reports the master switch.
func (e *Eggs) On() bool {
	return setting.EggsEnabled(e.ctx)
}

// HeadJSON is the page config of <script type="application/json" id="banana-eggs">, safe for <script>.
func (e *Eggs) HeadJSON() template.JS {
	ctx := e.ctx
	client := setting.EggsConfig(ctx).ClientConfig(
		setting.AppSubURL+eggsPagePath+"/{id}/found", setting.AppSubURL+eggsPagePath, ctx.IsSigned,
		"", "", // no CSRF token: BananaGit checks the origin of every POST (cross-origin protection)
		map[string]string{
			"found":     ctx.Locale.TrString("bananagit.eggs.found"),
			"open_page": ctx.Locale.TrString("bananagit.eggs.open_page"),
		})
	return template.JS(setting.EggScriptJSON(client)) //nolint:gosec // escaped for <script> by EggScriptJSON
}

// AdminStoredJSON is the list the editor starts from: what was just submitted, or what is stored.
func (e *Eggs) AdminStoredJSON() string {
	if v, ok := e.ctx.Data["BananaGitEggsSubmitted"].(string); ok {
		return v
	}
	return setting.EggsConfig(e.ctx).Dumps()
}

// AdminEnabled is the master switch as the form shows it.
func (e *Eggs) AdminEnabled() bool {
	if v, ok := e.ctx.Data["BananaGitEggsSubmittedEnabled"].(bool); ok {
		return v
	}
	return e.On()
}

// AdminErrors are the reasons the last save was refused.
func (e *Eggs) AdminErrors() []string {
	v, _ := e.ctx.Data["BananaGitEggsErrors"].([]string)
	return v
}

type editorBadge struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// AdminEditorOptions is the shared editor's options script (SPEC.md "Editor input").
func (e *Eggs) AdminEditorOptions() template.JS {
	ctx := e.ctx
	strs := make(map[string]string, len(editorStrings))
	for _, key := range editorStrings {
		strs[key] = ctx.Locale.TrString("bananagit.eggs.editor." + key)
	}
	badges := []editorBadge{}
	list, _, err := user_model.SearchBadges(ctx, &user_model.SearchBadgeOptions{ListOptions: db.ListOptionsAll, OrderBy: "slug ASC"})
	if err != nil {
		log.Error("SearchBadges: %v", err)
	}
	for _, b := range list {
		if !eggBadgeSlug(b.Slug) {
			continue // the egg format cannot name it
		}
		label := b.Slug
		if b.Description != "" {
			label += " · " + b.Description
		}
		badges = append(badges, editorBadge{Value: b.Slug, Label: label})
	}
	options := map[string]any{
		"strings":     strs,
		"emojis":      setting.EggEmojis,
		"badges":      badges,
		"mascot":      false,
		"egg":         setting.TemplateEgg(),
		"reset":       setting.DefaultEggConfig().Eggs,
		"buttonClass": "ui small basic button",
	}
	return template.JS(setting.EggScriptJSON(options)) //nolint:gosec // escaped for <script> by EggScriptJSON
}

func eggBadgeSlug(slug string) bool {
	if slug == "" || len(slug) > 64 {
		return false
	}
	for _, r := range slug {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.:-", r)) {
			return false
		}
	}
	return true
}

// eggErrorText turns one EggError into a sentence for the admin.
func eggErrorText(ctx *context.Context, e setting.EggError) string {
	if e.Field == "egg" {
		return ctx.Locale.TrString("bananagit.eggs.error_egg", e.Index+1)
	}
	if e.Index < 0 {
		return ctx.Locale.TrString("bananagit.eggs.error_list", ctx.Locale.TrString("bananagit.eggs.code."+e.Field+"."+e.Code))
	}
	label := e.Field
	if key, ok := map[string]string{
		"id": "id", "keys": "keys", "sprite": "sprite", "sprite.value": "emoji", "count": "count",
		"speed_min": "speed_min", "speed_max": "speed_max", "delay": "delay", "unlock": "unlock",
		"unlock.action": "unlock", "unlock.delay": "unlock_delay", "enabled": "enabled", "record": "record_find",
		"badge": "badge", "message": "message",
	}[e.Field]; ok {
		label = ctx.Locale.TrString("bananagit.eggs.editor." + key)
	}
	return ctx.Locale.TrString("bananagit.eggs.error", e.Index+1, label, ctx.Locale.TrString("bananagit.eggs.code."+e.Code))
}

// AdminSave stores the list from the admin settings page, cleaned strictly: anything wrong is shown next to the
// form, with the admin's edits kept.
func AdminSave(ctx *context.Context) {
	if ctx.Doer == nil || !ctx.Doer.IsAdmin {
		ctx.HTTPError(http.StatusForbidden)
		return
	}
	raw := ctx.Req.PostFormValue("config")
	enabled := ctx.Req.PostFormValue("enabled") != ""
	cfg, errs := setting.CleanEggs(raw)
	if len(errs) > 0 {
		texts := make([]string, 0, len(errs))
		for _, e := range errs {
			texts = append(texts, eggErrorText(ctx, e))
		}
		ctx.Data["BananaGitEggsErrors"] = texts
		ctx.Data["BananaGitEggsSubmitted"] = raw
		ctx.Data["BananaGitEggsSubmittedEnabled"] = enabled
		ctx.Data["Title"] = ctx.Tr("admin.config_settings")
		ctx.Data["PageIsAdminConfig"] = true
		ctx.Data["PageIsAdminConfigSettings"] = true
		ctx.Data["EnableOAuth2"] = setting.OAuth2.Enabled
		ctx.Data["EnablePackages"] = setting.Packages.Enabled
		ctx.HTML(http.StatusUnprocessableEntity, tplConfigSettings)
		return
	}
	if err := system_model.SetSettings(ctx, map[string]string{
		setting.BananaGitEggs.Config.DynKey():  cfg.Dumps(),
		setting.BananaGitEggs.Enabled.DynKey(): strconv.FormatBool(enabled),
	}); err != nil {
		ctx.ServerError("SetSettings", err)
		return
	}
	config.GetDynGetter().InvalidateCache()
	log.Info("BananaGit easter eggs changed by %s (%d eggs, on: %t)", ctx.Doer.Name, len(cfg.Eggs), enabled)
	ctx.Flash.Success(ctx.Tr("bananagit.eggs.admin.saved"))
	ctx.Redirect(setting.AppSubURL + "/-/admin/config/settings#bananagit-eggs")
}

func eggJSONError(ctx *context.Context, status int) {
	ctx.JSON(status, map[string]any{"ok": false})
}

// EggFound records a signed-in user's find: idempotent, answers {"ok": true, "new": true|false}.
func EggFound(ctx *context.Context) {
	if ctx.Doer == nil {
		eggJSONError(ctx, http.StatusUnauthorized)
		return
	}
	if !setting.EggsEnabled(ctx) {
		eggJSONError(ctx, http.StatusNotFound)
		return
	}
	egg := setting.EggsConfig(ctx).FindEgg(ctx.PathParam("id"))
	if egg == nil || !egg.Enabled {
		eggJSONError(ctx, http.StatusNotFound)
		return
	}
	if !findLimiter.Allow(ctx.Doer.ID) {
		eggJSONError(ctx, http.StatusTooManyRequests)
		return
	}
	if !egg.Record {
		ctx.JSON(http.StatusOK, map[string]any{"ok": true, "new": false})
		return
	}
	isNew, err := recordFind(ctx, ctx.Doer.ID, egg.ID)
	if err != nil {
		log.Error("BananaGit easter egg %q: recording the find of user %d: %v", egg.ID, ctx.Doer.ID, err)
		eggJSONError(ctx, http.StatusInternalServerError)
		return
	}
	if isNew {
		log.Info("BananaGit easter egg %q found by %s", egg.ID, ctx.Doer.Name)
	}
	if egg.Badge != nil && isNew {
		// only the first find: a badge an administrator took away stays away
		err := user_model.AddUserBadge(ctx, ctx.Doer, &user_model.Badge{Slug: *egg.Badge})
		switch {
		case err == nil:
			log.Info("BananaGit easter egg %q: badge %q awarded to %s", egg.ID, *egg.Badge, ctx.Doer.Name)
		case errors.Is(err, util.ErrAlreadyExist):
		case errors.Is(err, util.ErrNotExist):
			log.Warn("BananaGit easter egg %q: badge %q does not exist", egg.ID, *egg.Badge)
		default:
			log.Error("BananaGit easter egg %q: awarding badge %q: %v", egg.ID, *egg.Badge, err)
		}
	}
	ctx.JSON(http.StatusOK, map[string]any{"ok": true, "new": isNew})
}

// recordFind stores the first find of an egg; it is new only for the request whose row was inserted.
func recordFind(ctx *context.Context, userID int64, eggID string) (bool, error) {
	key := eggFindPrefix + eggID
	row := &user_model.Setting{UserID: userID, SettingKey: key}
	if has, err := db.GetEngine(ctx).Exist(row); err != nil || has {
		return false, err
	}
	row.SettingValue = strconv.FormatInt(int64(timeutil.TimeStampNow()), 10)
	if _, err := db.GetEngine(ctx).Insert(row); err != nil {
		// a concurrent request may have won the unique (user, key) index
		if has, existErr := db.GetEngine(ctx).Exist(&user_model.Setting{UserID: userID, SettingKey: key}); existErr == nil && has {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// EggRow is one egg on the easter egg page.
type EggRow struct {
	ID      string
	Emoji   string
	Message string
	Found   timeutil.TimeStamp
}

// EggsPage lists every enabled egg: found ones with the date, the others as "?". Signed-out visitors get a
// sign-in hint, and eggs-page.js shows what this browser found.
func EggsPage(ctx *context.Context) {
	if !setting.EggsEnabled(ctx) {
		ctx.NotFound(nil)
		return
	}
	var rows []EggRow
	var keys []string
	for _, egg := range setting.EggsConfig(ctx).Eggs {
		if !egg.Enabled {
			continue
		}
		emoji := egg.Sprite.Value
		if egg.Sprite.Type != "emoji" || emoji == "" {
			emoji = "🍌" // "mascot": BananaGit has no mascot sprite
		}
		rows = append(rows, EggRow{ID: egg.ID, Emoji: emoji, Message: egg.Message})
		keys = append(keys, eggFindPrefix+egg.ID)
	}
	found := 0
	if ctx.Doer != nil && len(keys) > 0 {
		finds, err := user_model.GetSettings(ctx, ctx.Doer.ID, keys)
		if err != nil {
			ctx.ServerError("GetSettings", err)
			return
		}
		for i := range rows {
			if s, ok := finds[eggFindPrefix+rows[i].ID]; ok {
				unix, _ := strconv.ParseInt(s.SettingValue, 10, 64)
				rows[i].Found = timeutil.TimeStamp(max(unix, 1))
				found++
			}
		}
	}
	ctx.Data["Title"] = ctx.Tr("bananagit.eggs.title")
	ctx.Data["BananaGitEggRows"] = rows
	ctx.Data["BananaGitEggsFound"] = found
	ctx.Data["BananaGitEggsTotal"] = len(rows)
	ctx.HTML(http.StatusOK, tplEggs)
}
