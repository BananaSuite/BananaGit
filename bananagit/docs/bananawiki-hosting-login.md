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
* `<authentication name>` is the name you give the source in step 2. Pick
  one without spaces, e.g. `BananaWiki`: it also appears on the sign-in
  button ("Sign in with BananaWiki").
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
| Authentication Name | `BananaWiki` (the name used in the redirect URI) |
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

## 3. Optional: BananaWiki Hosting as the only way in

All with existing Gitea settings in `app.ini`:

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

With `ENABLE_PASSWORD_SIGNIN_FORM = false`, password sign-in is refused for
everyone, administrators included. Before switching:

1. Sign in as an administrator with your password and link your account to
   your portal account (Settings → Security → Manage Linked Accounts → Link
   Account → BananaWiki). Check that you can sign in through the portal.
2. Keep a way back that does not depend on the portal (it can be down, the
   app secret can be rotated, the app can be suspended):
   * a passkey on a local administrator account, if passkeys stay enabled; or
   * shell access to the server: set `ENABLE_PASSWORD_SIGNIN_FORM = true`
     again and restart BananaGit, and if needed reset a password with
     `/opt/bananagit/current admin user change-password --config <app.ini>
     --username <admin> --password ...` (as the service's user);
     `admin user create --admin ...` creates a new administrator.

A portal account that is suspended (`403 account_suspended`) can no longer
sign in, but BananaGit sessions already open stay valid until they expire;
disable the BananaGit account too when you need to cut access at once.
