[![Go Report Card](https://goreportcard.com/badge/github.com/AlessioDP/kpmenu)](https://goreportcard.com/report/github.com/AlessioDP/kpmenu) [![Travis CI](https://travis-ci.com/AlessioDP/kpmenu.svg?branch=master)](https://travis-ci.com/AlessioDP/kpmenu)
# Kpmenu
Kpmenu is a tool written in Go used to view a KeePass database via a dmenu, or rofi, menu.

## Features
*   Supports KDBX v3.1 and v4.0 (based on [gokeepasslib](https://github.com/tobischo/gokeepasslib))
*   Pretty fast database decode thanks to Go
*   Interfaced with dmenu or rofi
*   Customize dmenu/rofi with additional command arguments
*   Kpmenu can be started as a daemon, so you don't need to re-insert credentials
    *   By default the first instance enters daemon mode with a sliding-window idle timeout (default 600 seconds)
    *   Each successful access resets the idle timer; once idle for the timeout, the daemon exits cleanly and removes its socket
    *   You can start a permanent daemon with `--daemon` option (no per-call timeout, exits only on signal)
*   Automatically put selected value into the clipboard (for a custom time)
    *   xsel and wl-clipboard supported
    *   By default it will use xsel, you can override it via config or `--clipboardTool` option
    *   Hidden password typing

## Security model

The daemon's cached state contains the unlocked KeePass database. Several
layers protect it:

*   **Per-user UNIX domain socket** at `$XDG_RUNTIME_DIR/kpmenu.sock` (mode `0600`).
    No TCP listener; nothing on the network can reach the daemon.
*   **`SO_PEERCRED` peer-UID check** on every accepted connection. Connections
    from any UID other than the server's own are refused and logged.
*   **`PR_SET_DUMPABLE = 0`** at startup. A crash will not write a coredump
    containing the unlocked database.
*   **Sliding-window idle timeout** (default 600s, configurable via
    `CacheTimeout`). Each successful access resets the timer; once idle, the
    daemon exits and the next access requires the master password.
*   **Clean shutdown on `SIGTERM`/`SIGINT`**. The signal handler closes the
    listener and unlinks the socket file, so screen-lock or session-end
    scripts can call `pkill -TERM kpmenu` to forget the unlocked state.

### Limitations

Kpmenu cannot defend against an attacker who already has code execution
under the same user account. This is a fundamental limit of any
process-resident password manager: the unlocked database lives in the
daemon's heap, and a same-UID attacker can read it via `ptrace`, binary
replacement, or simply by connecting to the socket. Mitigations live
outside the daemon: short cache timeouts, prompt screen-lock, and not
running untrusted code as your user.

### `DoNotShowMenu`

The `DoNotShowMenu` option skips the top-level Show/Reload/Exit choice
and goes straight to the entry picker. The field picker still runs, so
each lookup is still gated by an interactive selection. Only enable in
trusted single-user contexts.

## Dependencies
*   `dmenu` or `rofi`
*   `xsel` or `wl-clipboard`
*   `go` (compile only)

## Usage
I created kpmenu to make an easy and fast way to access into my KeePass database. These are some commands that you can do:
```bash
# Open a database
kpmenu -d path/to/database.kdbx

# Open a database with a key
kpmenu -d path/to/database.kdbx -k path/to/database.key

# Open a database (credentials taken from config) with a password and rofi
kpmenu -p "mypassword" -r
```

## Installation
### From AUR
You can directly install the package [kpmenu](https://aur.archlinux.org/packages/kpmenu/).

### Compiling from source
If you do not set `$GOPATH`, go sources will be downloaded into `$HOME/go`.
```bash
# Clone repository
git clone https://github.com/AlessioDP/kpmenu
cd kpmenu

# Build
make build

# Install system-wide (default: /usr)
sudo make install

# Or install to a user-local prefix (no sudo)
make DESTDIR=$HOME/.local install
```

## Configuration
You can set options via `config` or cli arguments.

Kpmenu will check for `$HOME/.config/kpmenu/config`. You can start from either:

*   `resources/config.default` — the original defaults shipped upstream.
*   `resources/kpmenu.conf.example` — a security-recommended starting point (rofi + wl-clipboard + 600s sliding-window timeout).

```bash
cp resources/kpmenu.conf.example $HOME/.config/kpmenu/config
```

## Options
Options taken with `kpmenu --help`
```text
Usage of ./kpmenu:
      --argsEntry string            Additional arguments for dmenu at entry selection, separated by a space
      --argsField string            Additional arguments for dmenu at field selection, separated by a space
      --argsMenu string             Additional arguments for dmenu at menu selection, separated by a space
      --argsPassword string         Additional arguments for dmenu at password selection, separated by a space
      --cacheOneTime                Cache the database only the first time
      --cacheTimeout int            Timeout of cache in seconds
  -c, --clipboardTime int           Timeout of clipboard in seconds (0 = no timeout)
      --clipboardTool string        Choose which clipboard tool to use
      --daemon                      Start kpmenu directly as daemon
  -d, --database string             Path to the KeePass database
      --fieldOrder string           String order of fields to show on field selection
      --fillBlacklist string        String of blacklisted fields that won't be shown
      --fillOtherFields             Enable fill of remaining fields
  -k, --keyfile string              Path to the database keyfile
  -n, --nocache                     Disable caching of database
  -p, --password string             Password of the database
      --passwordBackground string   Color of dmenu background and text for password selection, used to hide password typing
  -r, --rofi                        Use rofi instead of dmenu
      --textEntry string            Label for entry selection
      --textField string            Label for field selection
      --textMenu string             Label for menu selection
      --textPassword string         Label for password selection
  -v, --version                     Show kpmenu version
```

## License
See the [LICENSE](https://github.com/AlessioDP/kpmenu/blob/master/LICENSE) file.
