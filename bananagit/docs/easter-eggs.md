# Easter eggs

BananaGit has the BananaSuite easter eggs (format `banana-eggs/v1`, shared with BananaWiki, the hosting
portal and BananaChat): key sequences that, typed anywhere on the site, drop emoji down the page and then
open the easter egg page, show a message or do nothing.

## For administrators

*Site Administration → Configuration → Settings → Easter eggs* edits up to 10 eggs (key recorder, emoji,
numbers, what happens afterwards, an optional badge and message) and has the on/off switch. Saving refuses a
list with problems and lists them above the editor; nothing is lost. Without a saved list there is one egg:
the Konami code with a short banana shower. Turned off, pages carry no egg script at all and the routes below
answer 404.

Badges are Gitea's own (*Site Administration → Badges*); an egg can award one, shown on the finder's profile.
Finds are cosmetic and easy to forge, so do not give badges any meaning beyond fun.

| | |
| --- | --- |
| `GET /-/bananagit/eggs` | the easter egg page: every egg that is on, found ones with the date. Signed-out visitors see a sign-in hint and what this browser found |
| `POST /-/bananagit/eggs/{id}/found` | records a signed-in user's find (body `{}`), about 10 per minute per user, answers `{"ok": true, "new": true\|false}` |
| `POST /-/admin/config/bananagit-eggs` | the admin form (administrators only) |

Like every BananaGit POST, both are protected by Gitea's cross-origin check, so no CSRF token is sent.

## Storage

No migration: the list is the system setting `bananagit.easter_eggs` (JSON, cleaned again whenever it is
read, so a bad value never breaks a page), the switch is `bananagit.easter_eggs_enabled` (default on), and a
find is the user setting `bananagit.egg.<id>` holding the Unix time of the first find.

## Keeping in step with the suite

`public/assets/bananagit/banana-eggs.js`, `banana-eggs.css` and `banana-eggs-admin.js` are byte-identical
copies of BananaWiki's files, and `modules/setting/testdata/bananagit-eggs-vectors.json` of its
`eggs-vectors.json`. When the suite changes them, copy them over unchanged and run
`go test ./modules/setting/ -run Egg`: `modules/setting/bananagit_eggs.go` is the Go port of `eggs.py` and
must still reproduce every vector.
