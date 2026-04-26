# kpmenu

A small Go tool that exposes a KeePass database through `dmenu` or `rofi`.
Type the master password once, fuzzy-pick an entry, fuzzy-pick a field — the
value is copied to the clipboard for a configurable window.

This repository is a security-hardened fork of
[AlessioDP/kpmenu](https://github.com/AlessioDP/kpmenu) (MIT). The original
design and the OTP/TOTP feature are AlessioDP's; this fork tightens the
client-server transport, hardens the daemon process, and replaces the cache
timeout semantics. See "Differences from upstream" below.

## Features

* KDBX v3.1 and v4.0 (via [gokeepasslib](https://github.com/tobischo/gokeepasslib))
* dmenu or rofi front-end, with passable extra args per prompt step
* Daemon mode: enter the master password once, fuzzy-pick repeatedly until idle
* OTP/TOTP support — entries with an `otp` URI or `TOTP Seed` field surface a
  "Generate OTP" pseudo-field that emits the current 6-digit code
* xsel and wl-clipboard supported

## Security model

The daemon's cached state contains the unlocked KeePass database. The
following design choices protect it:

* **Per-user UNIX domain socket** at `$XDG_RUNTIME_DIR/kpmenu.sock` (mode `0600`).
  No TCP listener; the daemon is unreachable from the network.
* **`SO_PEERCRED` peer-UID check** on every accepted connection. Any
  connection from a UID other than the server's own is refused and logged.
* **`PR_SET_DUMPABLE = 0`** at startup. A crash will not write a coredump
  containing the unlocked database.
* **Sliding-window idle timeout** (default 600 seconds). Each successful
  access resets the timer; once idle, the daemon exits and the next access
  requires the master password.
* **Clean shutdown on `SIGTERM`/`SIGINT`**. The signal handler closes the
  listener and unlinks the socket file, so a screen-lock or session-end
  script can call `pkill -TERM kpmenu` to drop the unlocked state.

## Dependencies

* `dmenu` or `rofi`
* `xsel` or `wl-clipboard`
* `go` (compile only, 1.21+)

## Usage

```bash
# Open a database
kpmenu -d path/to/database.kdbx

# Open a database with a key file
kpmenu -d path/to/database.kdbx -k path/to/database.key

# Use rofi (overrides config)
kpmenu -r

# Disable OTP detection (useful if the raw secret is wanted)
kpmenu --nootp
```

## Installation

### From source

```bash
git clone https://github.com/felsd/kpmenu
cd kpmenu

# Build
make build

# Install system-wide (default prefix: /usr)
sudo make install

# Or install to a user-local prefix (no sudo)
make DESTDIR=$HOME/.local install
```

`$HOME/.local/bin` should appear before `/usr/bin` in `$PATH` for the
user-local install to take precedence.

## Configuration

kpmenu reads `$HOME/.config/kpmenu/config` (TOML). Two starter files ship
in `resources/`:

* `kpmenu.conf.example` — security-recommended defaults (rofi, wl-clipboard,
  600 s sliding-window timeout).
* `config.default` — the original upstream defaults.

```bash
cp resources/kpmenu.conf.example $HOME/.config/kpmenu/config
```

## Options

```text
Usage of kpmenu:
      --argsEntry string            Additional arguments for dmenu at entry selection, separated by a space
      --argsField string            Additional arguments for dmenu at field selection, separated by a space
      --argsMenu string             Additional arguments for dmenu at menu selection, separated by a space
      --argsPassword string         Additional arguments for dmenu at password selection, separated by a space
      --argsSeparator string        Separator char for custom args (default " ")
      --cacheOneTime                Cache the database only the first time
      --cacheTimeout int            Timeout of cache in seconds (default 600)
  -c, --clipboardTime int           Timeout of clipboard in seconds (0 = no timeout) (default 15)
      --clipboardTool string        Choose which clipboard tool to use (default "xsel")
      --daemon                      Start kpmenu directly as daemon
  -d, --database string             Path to the KeePass database
      --doNotShowMenu               Flag for skipping the menu selection
      --fieldOrder string           String order of fields to show on field selection (default "Password UserName URL")
      --fillBlacklist string        String of blacklisted fields that won't be shown
      --fillOtherFields             Enable fill of remaining fields (default true)
  -k, --keyfile string              Path to the database keyfile
  -n, --nocache                     Disable caching of database
      --nootp                       Disable OTP handling
  -p, --password string             Password of the database
      --passwordBackground string   Color of dmenu background and text for password selection, used to hide password typing (default "black")
      --rememberLastEntry           Remember last selected entry
  -r, --rofi                        Use rofi instead of dmenu
      --showNotifications           Show desktop notifications
      --textEntry string            Label for entry selection (default "Entry")
      --textField string            Label for field selection (default "Field")
      --textMenu string             Label for menu selection (default "Select")
      --textPassword string         Label for password selection (default "Password")
  -v, --version                     Show kpmenu version
```

## Differences from upstream

Forked from `AlessioDP/kpmenu` at v1.2.1 (2019). Subsequent changes in this
fork:

* IPC: TCP `:0` listener replaced with per-user UNIX domain socket and
  `SO_PEERCRED` UID check.
* Process: `PR_SET_DUMPABLE = 0`; `SIGTERM`/`SIGINT` handler with socket
  unlink.
* Cache timeout: fixed expiry replaced with a sliding window. Default
  raised from 60 s to 600 s.
* Per-iteration accept-loop close (file descriptors no longer accumulate).
* `go.mod` updated to Go 1.21.

OTP/TOTP support and the `--nootp` flag are ports of upstream's
[PR #7](https://github.com/AlessioDP/kpmenu/pull/7) (Sean E. Russell) and
follow-up commits by AlessioDP.

Upstream features not yet ported here:

* Custom prompt and clipboard executables (`--customPromptMenu`, `--customClipboardCopy`, etc.)
* `wofi` as a first-class menu choice and the `-m` menu flag
* Updated `viper` and `gokeepasslib` versions

## License

MIT — see [LICENSE](./LICENSE). Original copyright AlessioDP; modifications
under the same terms.
