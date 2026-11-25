# Signing in with BananaWiki Hosting

BananaGit can let people sign in with their BananaWiki Hosting account. It
adds an OAuth2 provider, **BananaWiki Hosting**, to Gitea's authentication
sources (the code is in `services/auth/source/oauth2/bananawiki/`). Everything
else is Gitea's own OAuth2 sign-in: account linking, auto-registration and the
settings below work as they do for GitHub or GitLab.

What the provider does, following the portal's contract:

* it uses PKCE S256 and checks `state`;
* it asks for the `profile` and `email` scopes (plus any "Additional Scopes");
* it reads the profile with `Authorization: Bearer`, never with the token in
  the URL;
* it keys accounts on the portal's `sub` only, never on the user name or the
  email;
* it passes on the email only when the portal marks it verified.

The portal issues no refresh tokens, so Gitea's OAuth2 sync job never touches
these accounts. Admin rights stay local: the portal does not send them to apps.

## 1. Register the app on the portal

Sign in to BananaWiki Hosting and open **Account → Developer apps**. Create an
app with this redirect URI:

```text
https://<your BananaGit>/user/oauth2/<authentication name>/callback
```

* `<your BananaGit>` is Gitea's `ROOT_URL` (with its sub-path, if any).
* `<authentication name>` is the name you give the source in step 2:
  `BananaWiki` unless you change it. It also appears on the sign-in button
  ("Sign in with BananaWiki").
* The portal compares redirect URIs exactly: same letter case, no trailing
  slash. Once you type the name in step 2, the admin form shows the exact URI
  under "Tips".

Copy the client ID (`bwa_…`) and the client secret; the secret is shown only
once. Until a hosting administrator has reviewed the app, only its owner and
hosting administrators can sign in with it, which is enough to test it.

## 2. Add the authentication source

In **Site Administration → Identity & Access → Authentication Sources → Add
Authentication Source**:

| Field | Value |
| --- | --- |
| Authentication Type | OAuth2 |
| Authentication Name | `BananaWiki` (the name used in the redirect URI; filled in when you pick the provider). Only letters, digits, `.`, `-` and `_` are accepted: a name that needs escaping, such as `BananaWiki Auth`, would give a redirect URI the portal refuses |
| OAuth2 Provider | BananaWiki Hosting |
| Client ID (Key) / Client Secret | from step 1 |
| Icon URL | optional; without it a book icon is shown |
| Use Custom URLs Instead of Default URLs | checked, with the defaults `https://hosting.bananawiki.com/oauth/authorize`, `/oauth/token` and `/oauth/userinfo`; change them only for a self-hosted portal |
| Skip local 2FA | unchecked |
| Additional Scopes | empty |
| This Authentication Source is Activated | checked |

The same from the command line (as the service's user, with the service's
`app.ini`):

```sh
/opt/bananagit/current admin auth add-oauth --config <app.ini> \
  --name BananaWiki --provider bananawiki --key bwa_... --secret ...
```

After a secret rotation on the portal, update it in the admin form or with
`admin auth update-oauth --id <id> --secret ...` (`admin auth list` shows the
id).

People with an existing BananaGit account link it in **Settings → Security →
Manage Linked Accounts → Link Account**. Someone signing in with an unknown
portal account gets Gitea's "link account" page: create a new account, or
connect an existing one by entering its password (and its two-factor code).

An administrator can also link accounts: **Site Administration → Identity &
Access → Account links** (next section).

### Account links

The page lists the users, which of them are linked to a BananaWiki Hosting
account, and how many active users (and administrators) are not linked yet,
with the exact `SIGNIN_MODE = hosting` step. From there an administrator can:

* link one user: a BananaGit user and a hosting username (looked up at the
  portal) or a hosting account id (`sub`, typed in);
* link many users at once: same username (case does not matter), same
  verified email, or both;
* link from a pasted list, one `local_username,hosting_username` (or
  `local_username,hosting_sub`) per line, at most 500 lines;
* unlink a user (not an account that was created by signing in with
  BananaWiki: that link is its only way in).

Every run shows a preview first: *will link*, *already linked*, *conflict*
(hosting account linked to someone else, user linked to another hosting
account, several candidates, suspended or pending hosting account),
*not found* and *skipped* (administrators unless "Include administrators" is
ticked, disabled accounts). **Apply** links only the *will link* rows of that
preview: if anything changed in between, nothing is linked and the new
preview is shown. Links are the same rows Gitea writes when a user links an
account (`external_login_user`). Every link and unlink goes to the server log
and to **System Notices** (who, which accounts, which rule).

Looking accounts up needs a portal API token of a hosting administrator with
the `accounts:lookup` scope:

```ini
[bananagit]
PORTAL_LOOKUP_TOKEN = ...
; or, read from a file:
; PORTAL_LOOKUP_TOKEN_URI = file:/etc/bananagit/portal-lookup-token
```

The portal's address comes from the token URL of the BananaWiki Hosting
source. Without a token the page only accepts account ids (`sub`) typed in.

### Two-step codes

Sign-ins through the BananaWiki Hosting source do not ask for BananaGit's own
two-step code: the portal asks for its own. Password sign-ins still ask.
`[bananagit] HOSTING_SKIPS_LOCAL_2FA = false` keeps asking after hosting
sign-ins too. Linking an existing account on the link-account page always asks
for its password and two-step code.

### Hosting administrators acting as an account

When the portal lets an administrator act as an account (portal setting
`admin_app_signin`), its sign-in tells BananaGit who is acting and until when.
BananaGit then:

* shows "*admin* is signed in as *user*" on every page;
* ends the session at the time the portal gave (at most 12 hours);
* refuses changing the password, two-step, emails, linked accounts, deleting
  the account, access tokens and OAuth2 applications, SSH, GPG and deploy
  keys, and authorising OAuth2 applications;
* never links or creates an account: an unknown hosting account is refused;
* refuses acting as a BananaGit administrator, and as an account that asks for
  BananaGit's own two-step code;
* records every such sign-in in the server log and in **System Notices**.

`[bananagit] ALLOW_ACTING_ADMINS = false` refuses these sign-ins altogether
(sessions already open end at their next request).

## 3. Optional: BananaWiki Hosting as the only way in

### SIGNIN_MODE = hosting

One BananaGit setting does it (read before you switch: [Do not lock yourself
out](#do-not-lock-yourself-out)):

```ini
[bananagit]
; default: both
SIGNIN_MODE = hosting

[service]
; no local sign-up page; keep DISABLE_REGISTRATION = false, or nobody new can get in
ALLOW_ONLY_EXTERNAL_REGISTRATION = true

[oauth2_client]
ENABLE_AUTO_REGISTRATION = true
USERNAME = nickname
ACCOUNT_LINKING = login
```

Restart BananaGit. With `hosting`, whatever `app.ini` says, BananaGit turns
off:

* the password form (`[service] ENABLE_PASSWORD_SIGNIN_FORM`), for
  administrators too;
* passkeys (`ENABLE_PASSKEY_AUTHENTICATION`);
* OpenID sign-in and sign-up (`[openid] ENABLE_OPENID_SIGNIN`,
  `ENABLE_OPENID_SIGNUP`);
* the automatic redirect (`ENABLE_AUTO_OAUTH2_REDIRECT`): the sign-in page
  and the home page show a "Sign in with BananaWiki" button.

It does not touch Git: SSH keys and access tokens over HTTPS work as before.
Git with a password over HTTPS follows `[service] ENABLE_BASIC_AUTHENTICATION`
as usual (see the strict option below). Any other active OAuth2 source still
shows its own button, so do not add one.

At startup BananaGit warns in the log, and on the admin dashboard, when there
is no active BananaWiki Hosting source, when registration settings keep new
people out (`DISABLE_REGISTRATION`, `ALLOW_ONLY_INTERNAL_REGISTRATION`), or
when the local sign-up page is still open.

To go back: set `SIGNIN_MODE = both` (or remove it) and restart; the
`[service]` and `[openid]` settings apply again.

### Setting by setting

The same with Gitea's own settings in `app.ini`, if you want to pick them one
by one:

```ini
[service]
; hide the password form and refuse password sign-in on /user/login
ENABLE_PASSWORD_SIGNIN_FORM = false
; new accounts only through an external source (no local sign-up page)
ALLOW_ONLY_EXTERNAL_REGISTRATION = true
; keep DISABLE_REGISTRATION = false, or nobody new can get in
; optional, strict: no passwords over HTTP Basic either (tokens still work)
ENABLE_BASIC_AUTHENTICATION = false

[oauth2_client]
; create the account on first sign-in
ENABLE_AUTO_REGISTRATION = true
; user name from the portal's preferred_username
USERNAME = nickname
; on a name or email clash: show the link-account page
ACCOUNT_LINKING = login
; the provider passes only emails the portal has verified
REGISTER_EMAIL_CONFIRM = false
```

Notes:

* **`ACCOUNT_LINKING = login`, not `auto`.** With `auto`, Gitea links a new
  portal account to the existing BananaGit account that has the same user
  name or email, without asking for its password. Portal user names and
  emails must not decide who owns an account; with `login` the person has to
  prove they own the BananaGit account (password and two-factor code).
* **Email confirmation.** `[oauth2_client] REGISTER_EMAIL_CONFIRM` (it
  defaults to `[service] REGISTER_EMAIL_CONFIRM`) applies to auto-registered
  accounts, whose email the portal has already verified. A portal account
  without a verified email cannot be auto-registered: the person gets the
  link-account page and types an email, and that account follows
  `[service] REGISTER_EMAIL_CONFIRM`; keep that on if mail is set up.
  `[service] REGISTER_MANUAL_CONFIRM = true` holds every new account until an
  administrator activates it.
* **Taken names.** If the portal name is already used in BananaGit, the
  person gets the link-account page and can choose another name.
* **Passkeys.** Local accounts with a passkey can still sign in with it. Set
  `[service] ENABLE_PASSKEY_AUTHENTICATION = false` (and keep
  `ENABLE_OPENID_SIGNIN = false`) to remove that too; with only one OAuth2
  source left, the login page then goes straight to the portal.
* **Git over HTTPS.** Accounts created through the portal have no password:
  use SSH keys or personal access tokens (Settings → Applications).

### Do not lock yourself out

With `SIGNIN_MODE = hosting` or `ENABLE_PASSWORD_SIGNIN_FORM = false`,
password sign-in is refused for everyone, administrators included. Before
switching:

1. Sign in as an administrator with your password and link your account to
   your portal account (Settings → Security → Manage Linked Accounts → Link
   Account → BananaWiki). Check that you can sign in through the portal.
2. Keep a way back that does not depend on the portal (it can be down, the
   app secret can be rotated, the app can be suspended):
   * a passkey on a local administrator account, if passkeys stay enabled
     (`SIGNIN_MODE = hosting` turns them off); or
   * shell access to the server: set `[bananagit] SIGNIN_MODE = both` and
     `ENABLE_PASSWORD_SIGNIN_FORM = true` again and restart BananaGit, and if
     needed reset a password with
     `/opt/bananagit/current admin user change-password --config <app.ini>
     --username <admin> --password ...` (as the service's user);
     `admin user create --admin ...` creates a new administrator.

A portal account that is suspended (`403 account_suspended`) can no longer
sign in, but BananaGit sessions already open stay valid until they expire;
disable the BananaGit account too when you need to cut access at once.
