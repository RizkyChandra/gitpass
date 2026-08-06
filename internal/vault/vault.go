// Package vault stores password entries as one age-encrypted JSON file per
// entry inside a git repository.
//
// The on-disk layout is deliberately boring so that a stock age binary is
// enough to recover everything if gitpass ever breaks:
//
//	identity.age          age -p encrypted X25519 private key
//	entries/<id>.age      age file, plaintext is one Entry as JSON
//
// One file per entry means two devices editing different entries never
// produce a git conflict, which matters because go-git cannot do three-way
// merges. See internal/sync.
package vault

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// scryptWorkFactor is pinned rather than left to age's default of 18. logN=17
// needs ~128MB to derive the key; 18 needs ~256MB, which risks OOM for the Go
// runtime inside an Android app. The factor is recorded in the age header, so
// raising it later needs no migration of existing vaults.
const scryptWorkFactor = 17

// padBlock rounds every entry's plaintext up to a multiple of this many bytes
// before encryption, so that ciphertext size no longer betrays how much is in
// an entry. Without it, a file's length distinguishes a bare username from one
// carrying a long secure note, and that is visible to anyone holding the repo.
//
// Padding is trailing whitespace on the JSON, which encoding/json and jq both
// ignore, so the `age -d | jq` recovery path keeps working untouched.
//
// ponytail: fixed 512-byte buckets, not a length-hiding scheme. A 4KB note
// still lands in a bigger bucket than a 100-byte login; only the fine detail
// is hidden. Switch to exponential buckets if that distinction matters.
const padBlock = 512

// Entry is one credential. It is the unit of both encryption and conflict
// resolution: the whole struct is encrypted into a single file, and sync
// resolves collisions by comparing UpdatedAt between two copies.
type Entry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Username  string    `json:"username,omitempty"`
	Email     string    `json:"email,omitempty"`
	Password  string    `json:"password,omitempty"`
	TOTP      string    `json:"totp,omitempty"` // otpauth:// URI, or a bare base32 secret
	URL       string    `json:"url,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`

	// Deleted marks a tombstone. Deleting writes the entry back with this set
	// instead of removing the file, which turns a delete into an ordinary
	// update and lets sync resolve it with the same timestamp rule as any
	// other edit — no resurrect-on-merge special case.
	// ponytail: tombstones are never collected, add a gc subcommand if the
	// repo gets fat.
	Deleted bool `json:"deleted,omitempty"`
}

// UnmarshalJSON accepts an entry whose updated_at is empty or absent, treating
// it as the zero time.
//
// Callers that build an entry from scratch — the Android app, an importer
// piping JSON into `gitpass add` — have no timestamp to supply, and the plain
// time.Time decoder rejects "" outright. Put overwrites the field anyway, so
// demanding it here only breaks honest callers. A malformed timestamp is still
// an error; this tolerates absence, not nonsense.
func (e *Entry) UnmarshalJSON(b []byte) error {
	type entry Entry // avoids recursing into this method
	aux := struct {
		UpdatedAt string `json:"updated_at"`
		*entry
	}{entry: (*entry)(e)}

	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	if aux.UpdatedAt == "" {
		e.UpdatedAt = time.Time{}
		return nil
	}
	t, err := time.Parse(time.RFC3339, aux.UpdatedAt)
	if err != nil {
		return fmt.Errorf("updated_at: %w", err)
	}
	e.UpdatedAt = t
	return nil
}

// Code returns the current TOTP code and how many seconds it remains valid.
func (e Entry) Code(t time.Time) (code string, secondsLeft int, err error) {
	if e.TOTP == "" {
		return "", 0, errors.New("entry has no TOTP configured")
	}
	uri := e.TOTP
	if !strings.HasPrefix(uri, "otpauth://") {
		// Tolerate a bare secret pasted from a site that shows only the key.
		uri = "otpauth://totp/gitpass?secret=" + strings.ToUpper(strings.ReplaceAll(uri, " ", ""))
	}
	key, err := otp.NewKeyFromURL(uri)
	if err != nil {
		return "", 0, fmt.Errorf("parse totp: %w", err)
	}
	period := key.Period()
	if period == 0 {
		period = 30
	}
	code, err = totp.GenerateCodeCustom(key.Secret(), t, totp.ValidateOpts{
		Period:    uint(period),
		Digits:    key.Digits(),
		Algorithm: key.Algorithm(),
	})
	if err != nil {
		return "", 0, err
	}
	return code, int(period) - int(uint64(t.Unix())%period), nil
}

// Vault is an unlocked vault backed by a git repository.
type Vault struct {
	Dir  string
	id   *age.X25519Identity
	repo *git.Repository
}

func entryPath(dir, id string) string { return filepath.Join(dir, "entries", id+".age") }

// Init creates a new vault: a git repo holding a freshly generated identity
// encrypted under passphrase. It fails if dir already contains a vault.
func Init(dir, passphrase string) (*Vault, error) {
	if err := CheckPassphrase(passphrase); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, "identity.age")); err == nil {
		return nil, fmt.Errorf("%s already contains a vault", dir)
	}
	if err := os.MkdirAll(filepath.Join(dir, "entries"), 0o700); err != nil {
		return nil, err
	}
	repo, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.ReferenceName("refs/heads/main")},
	})
	if err != nil {
		if !errors.Is(err, git.ErrRepositoryAlreadyExists) {
			return nil, err
		}
		if repo, err = git.PlainOpen(dir); err != nil {
			return nil, err
		}
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, err
	}
	if err := writeIdentity(dir, id, passphrase); err != nil {
		return nil, err
	}
	v := &Vault{Dir: dir, id: id, repo: repo}
	if err := v.commit("identity.age", "init vault"); err != nil {
		return nil, err
	}
	return v, nil
}

// Open unlocks an existing vault.
func Open(dir, passphrase string) (*Vault, error) {
	id, err := readIdentity(dir, passphrase)
	if err != nil {
		return nil, err
	}
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("open repo: %w", err)
	}
	return &Vault{Dir: dir, id: id, repo: repo}, nil
}

func writeIdentity(dir string, id *age.X25519Identity, passphrase string) error {
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return err
	}
	r.SetWorkFactor(scryptWorkFactor)
	f, err := os.OpenFile(filepath.Join(dir, "identity.age"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := age.Encrypt(f, r)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, id.String()); err != nil {
		return err
	}
	return w.Close()
}

func readIdentity(dir, passphrase string) (*age.X25519Identity, error) {
	f, err := os.Open(filepath.Join(dir, "identity.age"))
	if err != nil {
		return nil, fmt.Errorf("no vault at %s: %w", dir, err)
	}
	defer f.Close()
	si, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, err
	}
	r, err := age.Decrypt(f, si)
	if err != nil {
		return nil, errors.New("wrong passphrase")
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return age.ParseX25519Identity(string(b))
}

// All returns every entry including tombstones, sorted by name. Sync needs the
// tombstones; user-facing code wants List.
func (v *Vault) All() ([]Entry, error) {
	paths, err := filepath.Glob(filepath.Join(v.Dir, "entries", "*.age"))
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(paths))
	for _, p := range paths {
		e, err := v.readEntry(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, nil
}

// List returns the live entries, sorted by name.
func (v *Vault) List() ([]Entry, error) {
	all, err := v.All()
	if err != nil {
		return nil, err
	}
	live := all[:0]
	for _, e := range all {
		if !e.Deleted {
			live = append(live, e)
		}
	}
	return live, nil
}

// Get returns one entry by id, tombstones included.
func (v *Vault) Get(id string) (Entry, error) {
	return v.readEntry(entryPath(v.Dir, id))
}

func (v *Vault) readEntry(path string) (Entry, error) {
	var e Entry
	raw, err := os.ReadFile(path)
	if err != nil {
		return e, err
	}
	b, err := v.Unseal(raw)
	if err != nil {
		return e, err
	}
	return e, json.Unmarshal(b, &e)
}

// Seal encrypts arbitrary bytes to the vault's key. Used for entries and for
// the git credentials file.
func (v *Vault) Seal(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, v.id.Recipient())
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Unseal reverses Seal.
func (v *Vault) Unseal(b []byte) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(b), v.id)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return io.ReadAll(r)
}

// Put writes an entry, assigning an id and timestamp if absent, and commits it.
func (v *Vault) Put(e Entry) (Entry, error) {
	if strings.TrimSpace(e.Name) == "" {
		return e, errors.New("entry needs a name")
	}
	if e.ID == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return e, err
		}
		e.ID = hex.EncodeToString(b)
	}
	e.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if err := v.WriteRaw(e); err != nil {
		return e, err
	}
	// The commit message must not name the entry: git log is plaintext and
	// would otherwise leak the list of sites. The id is random.
	return e, v.commit(filepath.Join("entries", e.ID+".age"), "update "+e.ID)
}

// WriteRaw encrypts an entry to disk verbatim: it preserves UpdatedAt and does
// not commit. Sync uses it to replay many entries onto a reset worktree before
// making a single commit; Put is the normal path for user edits.
func (v *Vault) WriteRaw(e Entry) error {
	if err := os.MkdirAll(filepath.Join(v.Dir, "entries"), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	sealed, err := v.Seal(pad(b))
	if err != nil {
		return err
	}
	return os.WriteFile(entryPath(v.Dir, e.ID), sealed, 0o600)
}

// pad appends spaces until the payload fills a whole number of padBlock-sized
// blocks, hiding the true length of an entry.
func pad(b []byte) []byte {
	n := ((len(b) / padBlock) + 1) * padBlock
	return append(b, bytes.Repeat([]byte{' '}, n-len(b))...)
}

// Delete writes a tombstone rather than removing the file, so that the delete
// propagates through sync like any other edit.
func (v *Vault) Delete(id string) error {
	e, err := v.Get(id)
	if err != nil {
		return err
	}
	e.Deleted = true
	e.Password, e.TOTP, e.Notes, e.Username, e.Email = "", "", "", "", ""
	_, err = v.Put(e)
	return err
}

var signature = func() *object.Signature {
	return &object.Signature{Name: "gitpass", Email: "gitpass@localhost", When: time.Now()}
}

// commit stages one path and commits it. A no-op commit is not an error.
func (v *Vault) commit(path, msg string) error {
	wt, err := v.repo.Worktree()
	if err != nil {
		return err
	}
	if _, err := wt.Add(path); err != nil {
		return err
	}
	return finish(wt, msg)
}

// CommitAll stages every change in the worktree and commits once.
func (v *Vault) CommitAll(msg string) error {
	wt, err := v.repo.Worktree()
	if err != nil {
		return err
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return err
	}
	return finish(wt, msg)
}

func finish(wt *git.Worktree, msg string) error {
	_, err := wt.Commit(msg, &git.CommitOptions{Author: signature()})
	if errors.Is(err, git.ErrEmptyCommit) {
		return nil
	}
	return err
}

// DefaultGCAge is how long a tombstone must sit before GC will drop it.
//
// This is not arbitrary caution. Sync decides conflicts by comparing an entry
// against its counterpart; once the tombstone is gone there is nothing left to
// beat, so a device that last synced before the delete would push its stale
// copy back and resurrect the entry. The window therefore has to exceed the
// longest realistic gap between syncs on any device you own.
const DefaultGCAge = 90 * 24 * time.Hour

// GC permanently removes tombstones older than olderThan and returns how many
// it dropped. Everything it deletes remains in git history, so a mistake is
// recoverable with git alone.
func (v *Vault) GC(olderThan time.Duration) (int, error) {
	if olderThan <= 0 {
		return 0, errors.New("refusing to collect tombstones with no age limit: deleted entries would come back from any device that has not synced since")
	}
	all, err := v.All()
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().UTC().Add(-olderThan)
	dropped := 0
	for _, e := range all {
		if !e.Deleted || e.UpdatedAt.After(cutoff) {
			continue
		}
		if err := os.Remove(entryPath(v.Dir, e.ID)); err != nil {
			return dropped, err
		}
		dropped++
	}
	if dropped == 0 {
		return 0, nil
	}
	return dropped, v.CommitAll(fmt.Sprintf("gc: drop %d tombstone%s", dropped, map[bool]string{true: "", false: "s"}[dropped == 1]))
}

// Repo exposes the underlying repository to internal/sync.
func (v *Vault) Repo() *git.Repository { return v.repo }

// Recipient is the public key entries are encrypted to.
func (v *Vault) Recipient() *age.X25519Recipient { return v.id.Recipient() }
