package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/RizkyChandra/gitpass/internal/vault"
)

const pass = "correct-horse-battery-staple"

var base = time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

// newRemote creates a bare repo to act as the git host.
func newRemote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	_, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		Bare:        true,
		InitOptions: git.InitOptions{DefaultBranch: plumbing.ReferenceName("refs/heads/main")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// newVault creates a vault wired to remote and pushes its initial state.
func newVault(t *testing.T, remote string) *vault.Vault {
	t.Helper()
	v, err := vault.Init(t.TempDir(), pass)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetRemote(v, remote); err != nil {
		t.Fatal(err)
	}
	return v
}

func clone(t *testing.T, remote string) *vault.Vault {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clone")
	if err := Clone(dir, remote, ""); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// putAt writes an entry with an exact timestamp, so the tests can order edits
// without sleeping through the one-second timestamp granularity.
func putAt(t *testing.T, v *vault.Vault, e vault.Entry, at time.Time) {
	t.Helper()
	e.UpdatedAt = at
	if err := v.WriteRaw(e); err != nil {
		t.Fatal(err)
	}
	if err := v.CommitAll("test"); err != nil {
		t.Fatal(err)
	}
}

func mustSync(t *testing.T, v *vault.Vault) Result {
	t.Helper()
	r, err := Sync(v)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func get(t *testing.T, v *vault.Vault, id string) vault.Entry {
	t.Helper()
	e, err := v.Get(id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return e
}

func TestPushAndClone(t *testing.T) {
	remote := newRemote(t)
	a := newVault(t, remote)
	e, err := a.Put(vault.Entry{Name: "github.com", Password: "hunter2"})
	if err != nil {
		t.Fatal(err)
	}
	if r := mustSync(t, a); r.Action != "pushed" {
		t.Fatalf("first sync: %s", r)
	}

	b := clone(t, remote)
	if got := get(t, b, e.ID); got.Password != "hunter2" {
		t.Fatalf("clone lost the password: %+v", got)
	}
}

func TestFastForward(t *testing.T) {
	remote := newRemote(t)
	a := newVault(t, remote)
	mustSync(t, a)
	b := clone(t, remote)

	e, _ := a.Put(vault.Entry{Name: "added-later", Password: "s3cret"})
	mustSync(t, a)

	if r := mustSync(t, b); r.Action != "pulled" {
		t.Fatalf("expected a fast-forward, got %s", r)
	}
	if got := get(t, b, e.ID); got.Password != "s3cret" {
		t.Fatalf("fast-forward lost data: %+v", got)
	}
}

// The case the whole file-per-entry design exists for: two devices editing
// different entries offline must both survive, with no git conflict.
func TestDivergentEditsBothSurvive(t *testing.T) {
	remote := newRemote(t)
	a := newVault(t, remote)
	putAt(t, a, vault.Entry{ID: "aaa", Name: "shared", Password: "orig"}, base)
	mustSync(t, a)
	b := clone(t, remote)

	putAt(t, a, vault.Entry{ID: "onlyA", Name: "a-site", Password: "pa"}, base.Add(time.Minute))
	putAt(t, b, vault.Entry{ID: "onlyB", Name: "b-site", Password: "pb"}, base.Add(time.Minute))

	mustSync(t, a) // pushes onlyA
	if r := mustSync(t, b); r.Action != "merged" {
		t.Fatalf("expected a merge, got %s", r)
	}

	for _, v := range []*vault.Vault{b, a} {
		if v == a {
			mustSync(t, a) // a pulls b's merge
		}
		if got := get(t, v, "onlyA"); got.Password != "pa" {
			t.Fatalf("lost A's entry: %+v", got)
		}
		if got := get(t, v, "onlyB"); got.Password != "pb" {
			t.Fatalf("lost B's entry: %+v", got)
		}
	}

	live, err := b.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 3 {
		t.Fatalf("expected 3 entries after merge, got %d", len(live))
	}
}

// Same entry edited on both devices: the newer timestamp wins, and the outcome
// must not depend on which device syncs first.
func TestSameEntryNewerWins(t *testing.T) {
	for _, tc := range []struct {
		name         string
		aAt, bAt     time.Time
		aPass, bPass string
		want         string
	}{
		{"local newer", base.Add(10 * time.Second), base.Add(5 * time.Second), "newA", "oldB", "newA"},
		{"remote newer", base.Add(5 * time.Second), base.Add(10 * time.Second), "oldA", "newB", "newB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remote := newRemote(t)
			a := newVault(t, remote)
			putAt(t, a, vault.Entry{ID: "dup", Name: "shared", Password: "orig"}, base)
			mustSync(t, a)
			b := clone(t, remote)

			putAt(t, a, vault.Entry{ID: "dup", Name: "shared", Password: tc.aPass}, tc.aAt)
			putAt(t, b, vault.Entry{ID: "dup", Name: "shared", Password: tc.bPass}, tc.bAt)

			mustSync(t, a)
			mustSync(t, b)
			mustSync(t, a) // converge

			for name, v := range map[string]*vault.Vault{"a": a, "b": b} {
				if got := get(t, v, "dup"); got.Password != tc.want {
					t.Fatalf("%s: got %q, want %q", name, got.Password, tc.want)
				}
			}
		})
	}
}

// A delete must propagate and must not be resurrected by the other device's
// older copy of the same entry.
func TestDeletePropagates(t *testing.T) {
	remote := newRemote(t)
	a := newVault(t, remote)
	putAt(t, a, vault.Entry{ID: "doomed", Name: "old-account", Password: "p"}, base)
	mustSync(t, a)
	b := clone(t, remote)

	putAt(t, a, vault.Entry{ID: "doomed", Name: "old-account", Deleted: true}, base.Add(time.Minute))
	mustSync(t, a)
	mustSync(t, b)

	live, err := b.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 0 {
		t.Fatalf("deleted entry resurrected on the other device: %+v", live)
	}
	if got := get(t, b, "doomed"); !got.Deleted {
		t.Fatalf("tombstone did not propagate: %+v", got)
	}
}

func TestUpToDate(t *testing.T) {
	remote := newRemote(t)
	a := newVault(t, remote)
	mustSync(t, a)
	if r := mustSync(t, a); r.Action != "up-to-date" {
		t.Fatalf("expected up-to-date, got %s", r)
	}
}

// A brand-new repository on a git host has no refs at all. This is the normal
// first-sync path and must just push.
func TestFirstSyncIntoAnEmptyRemote(t *testing.T) {
	remote := newRemote(t) // bare, no commits — what `gh repo create` gives you
	a := newVault(t, remote)
	e, _ := a.Put(vault.Entry{Name: "github.com", Password: "hunter2"})

	if r := mustSync(t, a); r.Action != "pushed" {
		t.Fatalf("first sync into an empty remote: got %s, want pushed", r)
	}
	b := clone(t, remote)
	if got := get(t, b, e.ID); got.Password != "hunter2" {
		t.Fatalf("clone of the freshly seeded remote lost data: %+v", got)
	}
}

// A repo created with GitHub's "Add a README" is *not* empty: it has a commit,
// and it shares no history with the vault. Rebasing onto it would reset the
// worktree to a tree containing no identity.age, and since unionRebase replays
// only entries the key would be dropped and an unopenable vault pushed.
func TestRefusesRemoteWithUnrelatedHistory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "seeded")
	repo, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		Bare:        true,
		InitOptions: git.InitOptions{DefaultBranch: plumbing.ReferenceName("refs/heads/main")},
	})
	if err != nil {
		t.Fatal(err)
	}
	seedCommit(t, repo)

	v := newVault(t, dir)
	e, _ := v.Put(vault.Entry{Name: "github.com", Password: "hunter2"})

	_, err = Sync(v)
	if err == nil {
		t.Fatal("synced onto an unrelated history instead of refusing")
	}
	if !strings.Contains(err.Error(), "unrelated history") {
		t.Fatalf("unhelpful error: %v", err)
	}

	// The vault must be untouched: still openable, entry still readable.
	if _, statErr := os.Stat(filepath.Join(v.Dir, "identity.age")); statErr != nil {
		t.Fatalf("the refused sync destroyed the vault key: %v", statErr)
	}
	if got := get(t, v, e.ID); got.Password != "hunter2" {
		t.Fatalf("the refused sync damaged an entry: %+v", got)
	}
}

func ghSignature(when time.Time) object.Signature {
	return object.Signature{Name: "GitHub", Email: "noreply@github.com", When: when}
}

// seedCommit writes one commit straight into a bare repo, the way a host does
// when it initialises a repository with a README.
func seedCommit(t *testing.T, repo *git.Repository) {
	t.Helper()
	blob := repo.Storer.NewEncodedObject()
	blob.SetType(plumbing.BlobObject)
	w, err := blob.Writer()
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("# my vault\n"))
	w.Close()
	blobHash, err := repo.Storer.SetEncodedObject(blob)
	if err != nil {
		t.Fatal(err)
	}

	tree := &object.Tree{Entries: []object.TreeEntry{
		{Name: "README.md", Mode: filemode.Regular, Hash: blobHash},
	}}
	treeObj := repo.Storer.NewEncodedObject()
	if err := tree.Encode(treeObj); err != nil {
		t.Fatal(err)
	}
	treeHash, err := repo.Storer.SetEncodedObject(treeObj)
	if err != nil {
		t.Fatal(err)
	}

	when := base
	commit := &object.Commit{
		Author: ghSignature(when), Committer: ghSignature(when),
		Message: "Initial commit", TreeHash: treeHash,
	}
	commitObj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(commitObj); err != nil {
		t.Fatal(err)
	}
	commitHash, err := repo.Storer.SetEncodedObject(commitObj)
	if err != nil {
		t.Fatal(err)
	}
	ref := plumbing.NewHashReference(plumbing.ReferenceName("refs/heads/main"), commitHash)
	if err := repo.Storer.SetReference(ref); err != nil {
		t.Fatal(err)
	}
}
