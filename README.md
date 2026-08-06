# gitpass

A password manager that stores each credential as its own age-encrypted file in
a git repository. Usernames, passwords, TOTP, URLs, tags and notes, synced
through any git host you already have.

Not a wrapper around `pass` or GPG. Its own format, its own sync.

```
┌ gitpass ──────────────────────────────────┐
│ › bank.example                            │      github.com
│   a@b.c                                   │      user      alice
│   github.com                              │      password  ••••••••••••
│   alice  [work]                           │      totp      745065  ███░░░░░░  9s
│   laptop-only                             │      tags      work
└───────────────────────────────────────────┘
  p password · u user · t code · space reveal · e edit · d delete · s sync
```

## Why one file per entry

Git is the sync layer, so the storage format is designed around what git can
merge. go-git — the pure-Go implementation that makes an Android build possible
without the NDK — supports only fast-forward merges; there is no three-way
merge and no conflict resolution.

Rather than work around that, each entry is its own file named by a random id:

```
identity.age            your key, encrypted with your passphrase
entries/3f9a2b….age     one age file per entry, plaintext is JSON
```

Two devices editing *different* entries touch different files, so there is
nothing to merge. Two devices editing the *same* entry are resolved by the
`updated_at` timestamp inside the blob, deterministically, so both devices reach
the same answer regardless of who syncs first. Git only ever sees a linear
history of complete snapshots.

Deleting writes a tombstone rather than removing the file, which makes a delete
just another update — no special case, and no entry resurrected by another
device's stale copy.

Entry names live *inside* the encrypted blob, never in filenames or commit
messages. `git log` on your vault leaks nothing but timing and entry count.

## Install

```sh
go install github.com/RizkyChandra/gitpass/cmd/gitpass@latest
```

Or grab a binary from [releases](../../releases). Verify it against
`checksums.txt` — it is a password manager.

## Use

```sh
gitpass init                              # create a vault
gitpass remote git@github.com:you/vault.git
gitpass sync                              # push it
gitpass                                   # open the TUI
```

On a second machine:

```sh
gitpass clone git@github.com:you/vault.git
gitpass                                   # same passphrase, everything is there
```

### Keys

| Where | Key | Does |
|---|---|---|
| list | `enter` `a` `/` `s` `q` | open · add · filter · sync · quit |
| detail | `p` `u` `t` | copy password · username · TOTP code |
| detail | `space` `e` `d` `esc` | reveal · edit · delete · back |
| edit | `tab` `ctrl+g` `ctrl+s` `esc` | next field · generate password · save · cancel |

Copied secrets are wiped from the clipboard after 30 seconds, unless something
else has written to it in the meantime.

### Scripting

```sh
gitpass get github          # print a password
gitpass totp github         # print the current TOTP code
gitpass add < entries.json  # bulk import; takes one object or an array
```

`GITPASS_DIR` chooses the vault, `GITPASS_PASSPHRASE` skips the prompt.

## Security model

```
your passphrase ──scrypt──> identity.age ──X25519──> entries/*.age
```

`identity.age` is committed to the vault repo, so setting up a new device needs
only a clone and your passphrase — nothing to transfer out of band. The tradeoff
is that your git host holds the key file next to the data, which makes **your
passphrase the only barrier**. `gitpass init` therefore enforces a strength
floor and offers a generated six-word diceware phrase (~77 bits).

If you would rather the repo not contain the key, move `identity.age` out of the
vault directory and keep it locally; nothing else in the design depends on it
being there.

scrypt runs at `logN=17` (~128MB) rather than age's default 18 (~256MB), because
the same code has to derive keys inside an Android app. The factor is recorded
in the age header, so raising it later needs no migration.

**Known leaks:** the number of entries and their approximate sizes. Names,
usernames and tags are all inside the ciphertext.

### Recovery without gitpass

Entry files are ordinary age files. If this tool ever breaks or you abandon it,
your data comes back with the stock `age` binary:

```sh
age -d identity.age > identity.txt          # your passphrase
age -d -i identity.txt entries/3f9a2b.age | jq .
```

That escape hatch is the reason for using age rather than a bespoke construction.

## Sync

The remote URL is read from `.git/config` — there is no second config file. The
transport follows the URL scheme:

- `git@host:…` or `ssh://…` — ssh-agent first, then `~/.ssh/id_ed25519`
- `https://…` — an access token, stored encrypted to your vault key at
  `~/.local/share/gitpass/creds.age` (`gitpass token` sets it)
- anything else — treated as a local path, so a USB drive or another checkout
  works as a remote with no auth at all

`gitpass sync` reports what it did: `up-to-date`, `pulled`, `pushed`, or
`merged (2 local entries kept)`.

## Development

```sh
just            # list recipes
just test       # the suite, including real two-device divergence tests
just check      # gofmt, vet, tidy
just demo       # throwaway vault in .demo, then open the TUI on it
just aar        # build the Android library (needs Android SDK + NDK + JDK)
```

Layout:

```
internal/vault/   entry model, age crypto, git commits
internal/sync/    fetch, fast-forward, union-rebase, push
mobile/           gomobile facade — JSON in, JSON out
cmd/gitpass/      TUI and CLI
```

## Android

The core is deliberately gomobile-clean: every value crosses the JNI boundary as
a JSON string, because gomobile cannot bind maps or slices of structs. `just
aar` produces `gitpass.aar` for a Compose app to consume, and CI builds it on
every push so the facade cannot silently break.

The app itself is not written yet. Planned: unlock, search, detail with live
TOTP, edit, sync. Autofill comes after that.

## Not implemented

Attachments, password history, breach checks, browser extension, field-level
merge, tombstone garbage collection, size padding.

## License

MIT.

`internal/vault/wordlist.txt` is the [EFF long wordlist](https://www.eff.org/dice),
by the Electronic Frontier Foundation, used under CC BY 3.0 US.
