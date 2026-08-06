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
device's stale copy. `gitpass gc` drops tombstones once they are old enough that
every device has certainly seen them (90 days by default).

Entry names live *inside* the encrypted blob, never in filenames or commit
messages, and every entry is padded to a 512-byte boundary before encryption so
its size says nothing about its contents. `git log` on your vault leaks nothing
but timing and a rough entry count.

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
gitpass gc [days]           # drop tombstones older than days (default 90)
gitpass version
```

`GITPASS_DIR` chooses the vault, `GITPASS_PASSPHRASE` skips the prompt.

### Importing

`gitpass add` takes a JSON array, so migrating is a format conversion rather
than a feature. For KeePassXC:

```sh
keepassxc-cli export --format csv db.kdbx > export.csv
./scripts/keepassxc-csv.py export.csv | gitpass add
shred -u export.csv                    # it is plaintext
```

Titles, usernames, passwords, URLs, notes and `otpauth://` TOTP URIs all carry
over; groups become tags. Icons and KeePassXC's own timestamps do not — entries
are stamped with the time of import.

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

**Known leaks:** roughly how many entries you have. Names, usernames, tags and
sizes are all inside the ciphertext — entries are padded to 512-byte blocks, so
a bare login and one with a long secure note produce identical files.

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

**Point it at a genuinely empty repository.** A brand-new repo with no commits
is the expected first-sync target and just works. A repo created with GitHub's
"Add a README" is *not* empty — it has a commit sharing no history with your
vault — and sync refuses it rather than resetting onto a tree that has no
`identity.age` in it, which would push a vault nobody could open.

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

A Compose app in `android/`, sharing the exact same Go core through a gomobile
binding — every value crosses the JNI boundary as a JSON string, because
gomobile cannot bind maps or slices of structs.

```sh
just aar              # build the Go core into android/app/libs/
just apk              # assemble the debug APK and run unit tests
just android-test     # instrumented tests, needs an emulator or device
just android-install
just keystore         # create a signing key (once)
just apk-release      # signed release APK
just aab              # signed bundle for Play
```

It does unlock, search, detail with a live TOTP countdown, add/edit/delete,
sync, and tombstone collection. Copied secrets are marked sensitive so Android
13+ keeps them out of the clipboard preview, and every screen sets `FLAG_SECURE`
so passwords stay out of screenshots and the recents thumbnail.

### Autofill

Enable gitpass under **Settings → Passwords & accounts → Autofill service** and
it will offer logins inside other apps and browsers.

Fields are located by autofill hints where apps provide them, and otherwise by
input type and by the id/hint text — most real screens set no hints at all.
Entries are matched against the web domain or, for a native app, the brand
segment of its package name (`com.github.android` → `github`), scored rather
than filtered so a near miss shows an extra row instead of nothing.

When the vault is locked the service answers with an authentication request, so
Android shows a small unlock prompt and only then fills. Saving a new login from
a signup form works too, as long as the vault is already unlocked.

The `.aar` is a 17MB binary and is not committed; `just aar` builds it and CI
rebuilds it on every push, so the facade cannot silently break.

### Signing a release

```sh
just keystore      # once — generates android/keystore.{jks,properties}
just apk-release   # signed, minified APK
```

**Back up `android/keystore.jks` and `android/keystore.properties` immediately,
somewhere you will still have in five years.** Android identifies an app by its
signing key: lose it and you cannot ship an update that existing installs will
accept, and the only way forward is a new package name. Both files are
gitignored — never commit them.

Release builds run R8, which is why `proguard-rules.pro` keeps the gomobile JNI
classes and the kotlinx.serialization generated serializers. Both are reached
only reflectively, so R8 sees no references and would otherwise strip them, and
the app would compile and then fail the first time it decoded an entry.

To have CI attach a signed APK and bundle to each release, set the repository
variable `ANDROID_SIGNING_ENABLED` to `true` and add four secrets:

```sh
gh secret set ANDROID_KEYSTORE_BASE64   < <(base64 -w0 android/keystore.jks)
gh secret set ANDROID_KEYSTORE_PASSWORD        # from keystore.properties
gh secret set ANDROID_KEY_ALIAS                # gitpass
gh secret set ANDROID_KEY_PASSWORD             # from keystore.properties
gh variable set ANDROID_SIGNING_ENABLED --body true
```

Without them the Android release job is skipped, rather than publishing an
unsigned APK nobody can install.

## Not implemented

Attachments, password history, breach checks, browser extension, field-level
merge, biometric unlock, an idle auto-lock timeout.

## License

MIT.

`internal/vault/wordlist.txt` is the [EFF long wordlist](https://www.eff.org/dice),
by the Electronic Frontier Foundation, used under CC BY 3.0 US.
