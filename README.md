# modelpick

If you pay for several LLM coding subscriptions (Claude, ChatGPT/Codex,
OpenCode Go, often more than one account of each), modelpick decides which to
use right now: for a kind of work you name, it reads each account's live usage
and answers with the vendor, account, model and thinking level, as JSON.
Whoever calls it (you, a script, an agent launcher) acts on the answer;
modelpick never starts a coding session itself.

## What it looks like

```
$ modelpick pick coder
```

```json
{
  "status": "ok",
  "profile": "coder",
  "vendor": "codex",
  "account": "codex-main",
  "model": "gpt-6.1-sol",
  "thinking": "high",
  "account_ref": {"vendor": "codex", "codex_home": "/home/you/.codex"},
  "load_balance": null,
  "reason": null,
  "skipped": [
    {"account": "go-1", "model": "opencode-go/deepseek-v4.1", "why": "in schedule deepseek-peak"},
    {"account": "claude-main", "model": "claude-opus-5-5", "why": "7d window 58% >= pace 52.9% (day 3 of 7, +10)"}
  ],
  "errors": [],
  "warnings": []
}
```

The first choice of the `coder` profile was skipped because it is DeepSeek's
peak hour, the second because its weekly window is ahead of pace, so the
third was picked. [How modelpick decides](docs/concepts.md#a-worked-example)
walks through this example. With no profile, `pick` prints every profile's
answer, here for the profiles of [`examples/config.json`](examples/config.json):

```
$ modelpick pick
PROFILE          VENDOR       ACCOUNT     MODEL                      THINKING
coder-difficult  codex        codex-main  gpt-6.1-sol                high
reviewer         opencode-go  go-2        opencode-go/deepseek-v4.1  -
tester           opencode-go  go-2        opencode-go/deepseek-v4.1  -

For JSON, name a profile: modelpick pick <profile>
```

## Install

```
go install github.com/meatlover/modelpick/cmd/modelpick@latest
```

Linux and macOS, amd64 and arm64. Prebuilt archives, building from a checkout,
and a first config are in [getting started](docs/getting-started.md).

## Design decisions

- **No background polling.** modelpick reads an account's usage only when a
  command needs it (in the UI: Refresh, Diagnose, or the first reading of an
  account that has none), and at most once per account per 15 minutes.
  Between calls it answers from the last reading and says how old it is.
  See [the call floor](docs/concepts.md#the-call-floor).
- **It only answers.** It never starts, stops or reconfigures Claude Code,
  Codex or OpenCode. Whoever calls it acts on the JSON.
- **No stored credentials.** Logins are read in memory, only for the usage
  call. modelpick never writes a vendor's files and never handles a refresh
  token; an expired login is renewed by the vendor's own tool. See
  [Terms and credentials](#terms-and-credentials).
- **No automatic re-login.** A Claude login lasts until a date fixed when you
  logged in, and only an interactive `/login` extends it. `doctor` warns
  before that date; it cannot log in for you. See
  [expired logins](docs/concepts.md#expired-logins).
- **Each pick stands alone.** A pick keeps no state from earlier picks. A
  [load balancing group](docs/concepts.md#load-balancing-groups) picks a
  usable member at random; weighting by usage was considered and dropped.
- **No switching a running tool's login.** A launcher gives each process its
  own account through the environment instead; see
  [building a launcher](docs/reference.md#building-a-launcher).
- **Network calls go only to the vendors.** One usage call per account to
  its vendor, and no telemetry. The vendor tools modelpick runs (`opencode
  models`, a login renewal) contact their own vendor. See
  [what it sends](SECURITY.md#what-it-sends-and-to-whom).

## Documentation

- [Getting started](docs/getting-started.md): requirements, install, first config, first pick, uninstall.
- [How modelpick decides](docs/concepts.md): accounts, windows, caps, schedules, groups, a worked example.
- [Configuration guide](docs/configuration.md): every field of the config file.
- [Reference](docs/reference.md): every command and flag, the JSON outputs, exit codes, environment variables, writing a launcher.
- [Operations guide](docs/operations.md): files, network calls, child processes, the UI, renewal, troubleshooting.
- [Architecture guide](docs/architecture.md): packages, layering, the pick and store flow, adding a vendor.
- [Releasing](docs/release.md): cutting a release, what the release job checks, making the repository public.
- [Contributing](CONTRIBUTING.md): the gate, hooks, browser tests, the live smoke test.
- [Security](SECURITY.md): what modelpick reads, stores and sends; reporting a vulnerability.
- [Changelog](CHANGELOG.md): what changed in each release.

## Terms and credentials

Read this before you use modelpick.

- modelpick reads each tool's stored login (Claude Code's Keychain item or
  credentials file, Codex's `auth.json`, your OpenCode Go key file) **in
  memory**, only to call that vendor's usage endpoint.
- It **stores no credentials**: not in its cache, its output or its logs.
- It **never writes any vendor file** and never handles a refresh token. When a
  usage call is due and a Claude or Codex login has expired, it asks the
  vendor's own tool to renew it: `claude -p` with a tiny prompt (a few tokens
  of that Claude subscription, about 10 seconds) or `codex app-server` (no
  quota, about 3 seconds). The tool renews its own login. If the refresh
  token itself has expired, log in again (`/login` in Claude Code,
  `codex login`).
- Its usage call to Anthropic **identifies itself as Claude Code**: it sends
  `User-Agent: claude-cli/<version> (external, cli)`, with the version your
  installed `claude --version` prints, and the `anthropic-beta:
  oauth-2025-04-20` header. The calls to OpenAI and OpenCode send
  `User-Agent: modelpick/<version>`, and the OpenAI call also sends your
  ChatGPT account id (`ChatGPT-Account-Id`). [SECURITY.md](SECURITY.md) lists
  every request.

All three usage endpoints are undocumented and may change without notice; a
changed response is reported as "usage unknown", never as 0%.

Anthropic's terms say third-party tools "may not collect, store, or
intermediate" Claude login tokens. modelpick reads the token in memory and
stores nothing, but whether that is acceptable under your terms with each
vendor is your decision. You decide whether to use it.

## License

MIT, copyright Meatlover LLC. See [LICENSE](LICENSE).
