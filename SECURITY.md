# Security

modelpick reads the logins of your coding tools to ask each vendor how much
of your subscription is left. This page says exactly what it reads, stores
and sends, and how to report a problem. The
[operations guide](docs/operations.md) has the full detail.

## What it reads

- **The Claude Code login**: on macOS the Keychain item Claude Code made for
  the account's `config_dir`, read with `/usr/bin/security`; else
  `<config_dir>/.credentials.json`. modelpick uses the access token and the
  two expiry dates. The refresh token is in the same item; modelpick never
  uses or keeps it.
- **The Codex login**: `<codex_home>/auth.json`, its access token and account
  id. It also reads `<codex_home>/models_cache.json` for the model list.
- **An OpenCode Go key**: one line of your `key_file`.
- **Its own config file and cache.**

Credentials are read into memory for the one request that needs them.

## What it stores

Nothing secret. The config file says only where each login lives, and the
cache (`~/.cache/modelpick/`, mode `0700`, files `0600`) holds usage readings:
window lengths, percents and reset times, the time of the last call and its
error text. Error text never includes a credential or a response body, and a
renewal failure's text is scrubbed of anything that looks like a token.
modelpick writes no log file; internal errors go to stderr.

It never writes a vendor's files. When a Claude or Codex access token has
expired, it runs the vendor's own tool (`claude -p` with a one-word prompt,
or `codex app-server`), which renews its own login.

## What it sends, and to whom

One HTTPS `GET` per usage call, to the vendor whose login it is, at most once
per account per 15 minutes by default:

| Vendor | Endpoint | Sends |
|---|---|---|
| Anthropic | `https://api.anthropic.com/api/oauth/usage` | The Claude access token, `anthropic-beta: oauth-2025-04-20`, and `User-Agent: claude-cli/<version> (external, cli)`, the user agent of Claude Code itself with the version `claude --version` prints. |
| OpenAI | `https://chatgpt.com/backend-api/wham/usage` | The Codex access token, `ChatGPT-Account-Id`, `User-Agent: modelpick/<version>`. |
| OpenCode | `https://opencode.ai/zen/go/v1/usage` | The OpenCode Go key, `User-Agent: modelpick/<version>`. |

Redirects are never followed, so a credential cannot be sent to another host.
There is no telemetry and no other network call. `opencode models
opencode-go`, which modelpick runs to list models, may contact OpenCode
itself.

These usage endpoints are undocumented. Whether reading your login this way is
acceptable under each vendor's terms is your decision; see the README's
[Terms and credentials](README.md#terms-and-credentials).

## The local UI

`modelpick ui` serves a page that can change your config, so it accepts only
your browser:

- it listens on `127.0.0.1` only;
- it refuses a request whose `Host` is not `127.0.0.1:<port>` or
  `localhost:<port>` (DNS rebinding), and a cross-origin request;
- every request needs a random token, new for each run, which the printed URL
  carries once and the page then keeps in an `HttpOnly`, `SameSite=Strict`
  cookie;
- responses carry `Content-Security-Policy: default-src 'self';
  frame-ancestors 'none'`, `X-Content-Type-Options: nosniff` and
  `Cache-Control: no-store`.

The page never shows a credential.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting: on the repository's
**Security** tab, choose **Report a vulnerability**. Please do not open a
public issue for a security problem. Include the modelpick version
(`modelpick version`), your platform, and the steps that show the problem;
leave out real tokens and keys.
