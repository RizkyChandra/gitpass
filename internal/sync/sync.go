// Package sync replicates a vault through a git remote.
//
// go-git implements only fast-forward merges — there is no three-way merge and
// no conflict resolution. Rather than work around that, this package never asks
// git to merge anything. Because each entry is its own file and carries its own
// timestamp, a divergence is resolved by replaying local entries on top of the
// remote state and keeping the newer copy of each. Git only ever sees a linear
// history of complete snapshots.
package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/RizkyChandra/gitpass/internal/vault"
)

const branch = "main"

var (
	branchRef = plumbing.NewBranchReferenceName(branch)
	remoteRef = plumbing.NewRemoteReferenceName("origin", branch)
	pushSpec  = config.RefSpec(fmt.Sprintf("+%s:%s", branchRef, branchRef))
)

// Result describes what a Sync did, for display in the TUI.
type Result struct {
	Action string // "up-to-date", "pulled", "pushed", "merged"
	Kept   int    // entries whose local copy won during a merge
}

func (r Result) String() string {
	if r.Kept > 0 {
		return fmt.Sprintf("%s (%d local %s kept)", r.Action, r.Kept, plural(r.Kept, "entry", "entries"))
	}
	return r.Action
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Sync brings the vault and its remote into agreement, in both directions.
func Sync(v *vault.Vault) (Result, error) {
	repo := v.Repo()
	auth, err := authFor(v)
	if err != nil {
		return Result{}, err
	}

	// A push can lose a race with another device; refetch and redo. Three
	// attempts is plenty for a personal vault with a handful of devices.
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		res, err := syncOnce(v, repo, auth)
		if err == nil {
			return res, nil
		}
		if !isRaceLost(err) {
			return res, err
		}
		last = err
	}
	return Result{}, fmt.Errorf("remote kept moving under us: %w", last)
}

func isRaceLost(err error) bool {
	return errors.Is(err, git.ErrNonFastForwardUpdate) ||
		strings.Contains(err.Error(), "non-fast-forward")
}

func syncOnce(v *vault.Vault, repo *git.Repository, auth transport.AuthMethod) (Result, error) {
	err := repo.Fetch(&git.FetchOptions{RemoteName: "origin", Auth: auth, Tags: git.NoTags})
	switch {
	case err == nil, errors.Is(err, git.NoErrAlreadyUpToDate):
	case errors.Is(err, transport.ErrEmptyRemoteRepository):
		// A newly created repo on the host has no refs at all yet.
		return Result{Action: "pushed"}, push(repo, auth)
	default:
		return Result{}, fmt.Errorf("fetch: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return Result{}, fmt.Errorf("no local commits: %w", err)
	}

	remote, err := repo.Reference(remoteRef, true)
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		// Remote branch does not exist yet: this is the first push.
		return Result{Action: "pushed"}, push(repo, auth)
	}
	if err != nil {
		return Result{}, err
	}

	if head.Hash() == remote.Hash() {
		return Result{Action: "up-to-date"}, nil
	}

	localCommit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return Result{}, err
	}
	remoteCommit, err := repo.CommitObject(remote.Hash())
	if err != nil {
		return Result{}, err
	}

	if behind, err := localCommit.IsAncestor(remoteCommit); err != nil {
		return Result{}, err
	} else if behind {
		return Result{Action: "pulled"}, reset(repo, remote.Hash())
	}

	if ahead, err := remoteCommit.IsAncestor(localCommit); err != nil {
		return Result{}, err
	} else if ahead {
		return Result{Action: "pushed"}, push(repo, auth)
	}

	// Two vaults always share their init commit. No merge base means the
	// remote is somebody else's repository — most often a GitHub repo created
	// with "Add a README". Rebasing onto it would reset the worktree to a tree
	// with no identity.age in it, and since only entries are replayed the key
	// file would be dropped and an undecryptable vault pushed in its place.
	bases, err := localCommit.MergeBase(remoteCommit)
	if err != nil {
		return Result{}, err
	}
	if len(bases) == 0 {
		return Result{}, fmt.Errorf(
			"remote has an unrelated history and is not this vault — point at an "+
				"empty repository, or run `gitpass clone %s` on a fresh machine if it "+
				"already holds a vault", remoteURL(repo))
	}

	kept, err := unionRebase(v, repo, remote.Hash())
	if err != nil {
		return Result{}, err
	}
	return Result{Action: "merged", Kept: kept}, push(repo, auth)
}

// unionRebase discards the local commits, takes the remote state wholesale,
// then writes back every local entry that the remote lacks or that is newer
// locally. Deletes need no special case: a tombstone is just an entry whose
// timestamp happens to be newer.
func unionRebase(v *vault.Vault, repo *git.Repository, onto plumbing.Hash) (int, error) {
	local, err := v.All()
	if err != nil {
		return 0, err
	}
	if err := reset(repo, onto); err != nil {
		return 0, err
	}

	// Belt and braces behind the merge-base check above. Losing this file is
	// unrecoverable for anyone who only has the pushed copy, so refuse to
	// commit rather than publish a vault nobody can open.
	if _, err := os.Stat(filepath.Join(v.Dir, "identity.age")); err != nil {
		return 0, fmt.Errorf("refusing to merge: the remote tree has no identity.age (%w)", err)
	}

	kept := 0
	for _, mine := range local {
		theirs, err := v.Get(mine.ID)
		if err != nil && !os.IsNotExist(err) {
			return 0, err
		}
		if err == nil && !wins(mine, theirs) {
			continue
		}
		if err := v.WriteRaw(mine); err != nil {
			return 0, err
		}
		kept++
	}
	if kept == 0 {
		return 0, nil
	}
	return kept, v.CommitAll(fmt.Sprintf("merge: %d local %s", kept, plural(kept, "entry", "entries")))
}

// wins reports whether mine should replace theirs. Ties are broken on the
// encoded bytes so that two devices merging the same pair independently reach
// the same answer — otherwise they would ping-pong forever.
func wins(mine, theirs vault.Entry) bool {
	if !mine.UpdatedAt.Equal(theirs.UpdatedAt) {
		return mine.UpdatedAt.After(theirs.UpdatedAt)
	}
	a, _ := json.Marshal(mine)
	b, _ := json.Marshal(theirs)
	return string(a) > string(b)
}

// remoteURL reports origin's URL for error messages, empty if unset.
func remoteURL(repo *git.Repository) string {
	remote, err := repo.Remote("origin")
	if err != nil || len(remote.Config().URLs) == 0 {
		return "<url>"
	}
	return remote.Config().URLs[0]
}

func reset(repo *git.Repository, to plumbing.Hash) error {
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	return wt.Reset(&git.ResetOptions{Commit: to, Mode: git.HardReset})
}

func push(repo *git.Repository, auth transport.AuthMethod) error {
	err := repo.Push(&git.PushOptions{
		RemoteName: "origin",
		Auth:       auth,
		RefSpecs:   []config.RefSpec{pushSpec},
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

// Clone fetches an existing vault. The token is passed in rather than read from
// storage because the credentials file is encrypted to the vault key, which
// does not exist locally until after the clone.
func Clone(dir, url, token string) error {
	auth, err := authForURL(url, token)
	if err != nil {
		return err
	}
	_, err = git.PlainClone(dir, false, &git.CloneOptions{URL: url, Auth: auth})
	return err
}

// SetRemote points a vault at a git remote, replacing any existing origin.
func SetRemote(v *vault.Vault, url string) error {
	repo := v.Repo()
	if _, err := repo.Remote("origin"); err == nil {
		if err := repo.DeleteRemote("origin"); err != nil {
			return err
		}
	}
	_, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{url}})
	return err
}
