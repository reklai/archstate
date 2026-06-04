# Archstate

Archstate reproduces an Arch-based machine from explicit packages plus selected config/home files.

```text
explicit pacman packages + explicit AUR packages + managed config/home symlinks
```

State lives in `~/.config/archstate` as plain files. `bootstrap` installs missing packages, uses `paru` or `yay` for AUR packages, and recreates managed symlinks.

## Requirements

- Arch Linux or an Arch-based distribution.
- `git`
- Go, for building from source.
- `pacman`
- Optional: `paru` or `yay` for AUR packages. If missing, bootstrap can install one explicitly.
- Optional: systemd user services for the auto-sync timer.

## Quick Start

Clone, build, and initialize:

```bash
git clone <repo-url> archstate
cd archstate
go build -o archstate ./cmd/archstate
./archstate init
```

`init` creates `~/.config/archstate` and installs the binary to:

```text
~/.local/bin/archstate
```

If `~/.local/bin` is not in `PATH`, Archstate prints the shell line to add. It does not edit shell files automatically.

`init` is safe to rerun. Existing Archstate state is preserved; missing default files/directories are created; the binary is installed or updated. Use `archstate init --no-install` to skip binary installation.

After installation, the command should work from anywhere:

```bash
archstate help
```

## Start Tracking This Machine

Record current explicit packages:

```bash
archstate sync
```

Track selected config and home entries:

```bash
archstate config add nvim
archstate config add mimeapps.list
archstate home add .zshrc
archstate home add .profile
```

Save a baseline:

```bash
archstate snapshot save baseline
```

Inspect what Archstate sees:

```bash
archstate status
archstate doctor
```

`sync` treats the current machine's explicit packages as source of truth:

```text
pacman -Qqen -> pacman.conf
pacman -Qqem -> aur.conf
```

Package entries use official package names as reported by pacman. The value after `=` is only a human description:

```text
ripgrep=search tool that recursively searches directories for a regex pattern
paru-bin=feature packed AUR helper
```

If `pacman.conf` and `aur.conf` already match the current machine and are already in Archstate's canonical format, `sync` exits without creating a snapshot or rewriting files.

## Reproduce a Machine

Install the Archstate CLI first. If `archstate` is already available, skip this step:

```bash
git clone <archstate-source-url> archstate
cd archstate
go build -o archstate ./cmd/archstate
./archstate install
```

Clone your Archstate repo into the expected location:

```bash
git clone <your-archstate-repo> ~/.config/archstate
cd ~/.config/archstate
```

Preview first:

```bash
archstate bootstrap --preview
```

Apply:

```bash
archstate bootstrap
```

If AUR packages are tracked and no helper is installed, choose one explicitly:

```bash
archstate bootstrap --aur-helper paru
```

or:

```bash
archstate bootstrap --aur-helper yay
```

`paru` maps to `paru-bin`; `yay` maps to `yay-bin` for helper bootstrap.

## Conflict Modes

Naked bootstrap fails on unmanaged config/home conflicts before package installs. That keeps package installs and file conflict resolution from being mixed together silently.

Preview conflicts:

```bash
archstate bootstrap --preview
```

Choose the local copy:

```bash
archstate bootstrap --adopt
```

`--adopt` saves the current local config/home entry into Archstate, then replaces the local entry with a managed symlink. It works whether the tracked copy already exists or not.

Choose the tracked copy:

```bash
archstate bootstrap --overwrite
```

`--overwrite` restores the tracked Archstate copy over the local entry, then replaces the local entry with a managed symlink. It fails if the tracked copy is missing.

## Safety and Recovery

`status` shows drift without changing anything:

```bash
archstate status
```

It reports:

- tracked native/AUR packages that are missing
- explicitly installed native/AUR packages that are not tracked
- managed config and home entries as ok, missing, conflict, or error

`doctor` checks, explains, and prescribes:

```bash
archstate doctor
```

Example shape:

```text
OK repo: ~/.config/archstate
ERROR AUR helper: paru/yay not found
  fix: archstate bootstrap --aur-helper paru
  fix: archstate bootstrap --aur-helper yay

ERROR config nvim: unmanaged local entry exists
  preview: archstate bootstrap --preview
  fix keep local: archstate bootstrap --adopt
  fix restore tracked: archstate bootstrap --overwrite

WARN package drift: explicit packages are not tracked
  inspect: archstate status
  accept current machine: archstate sync
```

Snapshots capture Archstate repo state only:

```text
pacman.conf
aur.conf
config.conf
home.conf
config/
home/
```

Manual snapshots:

```bash
archstate snapshot save baseline
archstate snapshot list
archstate snapshot restore <id>
archstate snapshot rm <id>
```

Automatic snapshots are created silently before risky Archstate mutations and pruned to the latest 5. Manual snapshots are kept until removed.

Snapshots do not capture installed packages, pacman cache, system files, or the full home directory.

Repo-state mutations take a per-repo lock so two Archstate commands do not rewrite state at the same time. If the Archstate repo is a Git worktree, destructive repo rewrites require a clean worktree first; commit or stash local changes before running `config add`, `config rm`, `home add`, `home rm`, `bootstrap --adopt`, `bootstrap --overwrite`, or `snapshot restore`. `sync` is allowed to rewrite package state with a dirty worktree because it treats the current machine as source of truth and creates an automatic snapshot first.

## Optional Auto-Sync

Manual `archstate sync` is the core workflow. If you want Archstate to keep package state fresh automatically, install the optional systemd user timer:

```bash
archstate service install
archstate service enable
archstate service status
```

`service install` installs/updates `~/.local/bin/archstate` and writes:

```text
~/.config/systemd/user/archstate-sync.service
~/.config/systemd/user/archstate-sync.timer
```

The timer runs:

```bash
archstate sync
```

Timer defaults:

```text
OnBootSec=5min
OnUnitActiveSec=1h
RandomizedDelaySec=10min
Persistent=true
```

This should be low stress because `sync` only reads the local pacman database, writes small state files, and skips snapshots/writes when package state is already current.

Avoid pacman hooks as the default automation path. Hooks run around root package transactions, AUR helpers differ, and the repo lives in the user's home directory. A user timer is easier to reason about and easier to disable.

Disable or remove it:

```bash
archstate service disable
archstate service uninstall
```

## Command Reference

Top-level help:

```bash
archstate help
```

Detailed command help:

```bash
archstate help bootstrap
archstate snapshot --help
archstate config -h
```

Command map:

```text
init       Create repo state and install archstate to ~/.local/bin.
install    Install or update archstate in ~/.local/bin.
sync       Rewrite package state from explicit pacman/AUR packages.
status     Show tracked state vs current machine drift.
config     Manage direct children of ~/.config.
home       Manage direct children of ~.
snapshot   Save, list, restore, or remove repo-state snapshots.
bootstrap  Install missing packages and recreate managed symlinks.
doctor     Diagnose repo health and print concrete fix commands.
service    Manage the optional systemd user sync timer.
```

## Managed Files

Config entries are direct children of `~/.config`:

```bash
archstate config add nvim
archstate config rm nvim
```

Mapping:

```text
~/.config/archstate/config/<value> -> ~/.config/<key>
```

Home entries are direct children of `~`:

```bash
archstate home add .zshrc
archstate home rm .zshrc
```

Mapping:

```text
~/.config/archstate/home/<value> -> ~/<key>
```

Nested paths are intentionally not supported:

```text
.ssh/config
some/deep/path
```

Use direct entries only. This keeps the model easy to inspect and hard to misuse.

## Repo Layout

```text
~/.config/archstate/
  .archstate-root
  pacman.conf
  aur.conf
  config.conf
  home.conf
  config/
    nvim/
    mimeapps.list
  home/
    .zshrc
    .profile
  .snapshots/
    manual-2026-06-04_14-30-11-baseline/
    auto-2026-06-04_14-42-03/
```

Every Archstate-written state file starts with:

```text
# Auto-generated by archstate.
# Treat this file as read-only state. Use archstate commands to update it.
# Manual edits may be overwritten.
```

Files are machine-formatted and alphabetized by key. `sync` treats installed explicit packages as authoritative and repairs package files.

## Design Rules

- Arch-only.
- Plain files over a database.
- No hidden shell edits.
- No daemon in the core workflow; auto-sync is an opt-in systemd user timer.
- Package removal stays with pacman/paru/yay; Archstate records explicit package state via `sync`.
- Reproducibility means installing missing explicit packages and recreating managed config/home symlinks.

## License

MIT. See [LICENSE](LICENSE).
