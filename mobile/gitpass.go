// Package gitpass is the gomobile binding for the vault.
//
// gomobile can only bind a narrow set of types — no maps, no slices of
// structs, no variadics — so every compound value crosses the boundary as a
// JSON string. Kotlin decodes it with kotlinx.serialization. This keeps the
// Go API stable no matter how the Entry struct grows.
//
// Build with:
//
//	gomobile bind -target=android -o gitpass.aar ./mobile
package gitpass

import (
	"encoding/json"
	"time"

	"github.com/RizkyChandra/gitpass/internal/sync"
	"github.com/RizkyChandra/gitpass/internal/vault"
)

// Vault is an unlocked vault. Hold one for the lifetime of the unlocked
// session and drop it on lock.
type Vault struct {
	v *vault.Vault
}

// SetCredsDir tells the library where to keep the encrypted git token. Android
// has no home directory, so the app must pass its private files dir here
// before any sync.
func SetCredsDir(dir string) { sync.CredsDir = dir }

// Init creates a new vault at dir.
func Init(dir, passphrase string) (*Vault, error) {
	v, err := vault.Init(dir, passphrase)
	if err != nil {
		return nil, err
	}
	return &Vault{v}, nil
}

// Open unlocks an existing vault. A wrong passphrase is reported as an error.
func Open(dir, passphrase string) (*Vault, error) {
	v, err := vault.Open(dir, passphrase)
	if err != nil {
		return nil, err
	}
	return &Vault{v}, nil
}

// Clone fetches a vault from a git remote. Call Open afterwards to unlock it.
func Clone(dir, url, token string) error { return sync.Clone(dir, url, token) }

// List returns the live entries as a JSON array, sorted by name.
func (v *Vault) List() (string, error) {
	entries, err := v.v.List()
	if err != nil {
		return "", err
	}
	return encode(entries)
}

// Get returns one entry as a JSON object.
func (v *Vault) Get(id string) (string, error) {
	e, err := v.v.Get(id)
	if err != nil {
		return "", err
	}
	return encode(e)
}

// Put saves an entry given as JSON and returns the saved entry, which carries
// the assigned id and timestamp. An empty id creates a new entry.
func (v *Vault) Put(entryJSON string) (string, error) {
	var e vault.Entry
	if err := json.Unmarshal([]byte(entryJSON), &e); err != nil {
		return "", err
	}
	saved, err := v.v.Put(e)
	if err != nil {
		return "", err
	}
	return encode(saved)
}

// Delete writes a tombstone for the entry.
func (v *Vault) Delete(id string) error { return v.v.Delete(id) }

// TOTP returns {"code":"123456","seconds_left":17} for the entry.
func (v *Vault) TOTP(id string) (string, error) {
	e, err := v.v.Get(id)
	if err != nil {
		return "", err
	}
	code, left, err := e.Code(time.Now())
	if err != nil {
		return "", err
	}
	return encode(struct {
		Code        string `json:"code"`
		SecondsLeft int    `json:"seconds_left"`
	}{code, left})
}

// Sync replicates through the remote and returns a short description of what
// happened, e.g. "merged (2 local entries kept)".
func (v *Vault) Sync() (string, error) {
	res, err := sync.Sync(v.v)
	if err != nil {
		return "", err
	}
	return res.String(), nil
}

// SetRemote points the vault at a git remote.
func (v *Vault) SetRemote(url string) error { return sync.SetRemote(v.v, url) }

// SetToken stores an HTTPS access token, encrypted to the vault key.
func (v *Vault) SetToken(token string) error { return sync.SetToken(v.v, token) }

// GeneratePassword returns a random password of n characters.
func GeneratePassword(n int) (string, error) { return vault.RandomPassword(n) }

// GeneratePassphrase returns a random diceware phrase, for the unlock screen
// to suggest when creating a vault.
func GeneratePassphrase(words int) (string, error) { return vault.Diceware(words) }

// CheckPassphrase reports whether a passphrase is strong enough to be the sole
// protection on the key file stored in the repo.
func CheckPassphrase(passphrase string) error { return vault.CheckPassphrase(passphrase) }

func encode(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
