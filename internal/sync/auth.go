package sync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"

	"github.com/RizkyChandra/gitpass/internal/vault"
)

// CredsDir overrides where the encrypted git token is kept. Left empty on
// desktop; set by the mobile binding, where there is no home directory.
var CredsDir string

func credsPath() (string, error) {
	if CredsDir != "" {
		return filepath.Join(CredsDir, "creds.age"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "gitpass", "creds.age"), nil
}

// SetToken stores an HTTPS access token, encrypted to the vault's own key so a
// stolen home directory or backup does not leak it.
func SetToken(v *vault.Vault, token string) error {
	path, err := credsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	sealed, err := v.Seal([]byte(token))
	if err != nil {
		return err
	}
	return os.WriteFile(path, sealed, 0o600)
}

func readToken(v *vault.Vault) (string, error) {
	path, err := credsPath()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	b, err := v.Unseal(raw)
	return string(b), err
}

// authFor picks a transport based on the remote's URL scheme.
func authFor(v *vault.Vault) (transport.AuthMethod, error) {
	remote, err := v.Repo().Remote("origin")
	if err != nil {
		return nil, errors.New("no remote configured: run `gitpass remote <url>`")
	}
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return nil, errors.New("remote origin has no URL")
	}
	url := urls[0]

	var token string
	if isHTTP(url) {
		if token, err = readToken(v); err != nil {
			return nil, fmt.Errorf("no access token stored for %s: run `gitpass token` (%w)", url, err)
		}
	}
	return authForURL(url, token)
}

func isHTTP(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

// isSSH matches ssh://… and the scp-like git@host:path form, but not a bare
// filesystem path.
func isSSH(url string) bool {
	if strings.HasPrefix(url, "ssh://") {
		return true
	}
	host, _, found := strings.Cut(url, ":")
	return found && strings.Contains(host, "@")
}

func authForURL(url, token string) (transport.AuthMethod, error) {
	if isHTTP(url) {
		if token == "" {
			return nil, nil // public repo, or credentials in the URL
		}
		// Every major host accepts the token as the password with any
		// non-empty username.
		return &githttp.BasicAuth{Username: "gitpass", Password: token}, nil
	}
	if !isSSH(url) {
		return nil, nil // local path or file:// — a USB drive or another checkout
	}

	// ssh-agent first: on a desktop that is zero configuration and keeps the
	// key encrypted at rest.
	if auth, err := gitssh.NewSSHAgentAuth("git"); err == nil {
		return auth, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"id_ed25519", "id_rsa"} {
		path := filepath.Join(home, ".ssh", name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		auth, err := gitssh.NewPublicKeysFromFile("git", path, "")
		if err != nil {
			return nil, fmt.Errorf("%s: %w (encrypted keys need ssh-agent)", name, err)
		}
		return auth, nil
	}
	return nil, errors.New("no ssh-agent and no key at ~/.ssh/id_ed25519")
}
