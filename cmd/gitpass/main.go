// Command gitpass is a git-backed password manager.
//
//	gitpass                 open the TUI
//	gitpass init            create a vault
//	gitpass clone <url>     fetch an existing vault
//	gitpass remote <url>    point this vault at a git remote
//	gitpass token           store an HTTPS access token
//	gitpass sync            replicate through the remote
//	gitpass get <name>      print one entry's password (for scripts)
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/RizkyChandra/gitpass/internal/sync"
	"github.com/RizkyChandra/gitpass/internal/vault"
)

// version is set by the linker at release time.
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gitpass:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}
	switch cmd {
	case "":
		return runTUI()
	case "init":
		return cmdInit()
	case "clone":
		return cmdClone(args)
	case "remote":
		return cmdRemote(args)
	case "token":
		return cmdToken()
	case "sync":
		return cmdSync()
	case "get":
		return cmdGet(args)
	case "totp":
		return cmdTOTP(args)
	case "add":
		return cmdAdd()
	case "gc":
		return cmdGC(args)
	case "version", "-v", "-V", "-version", "--version":
		fmt.Println(versionString())
		return nil
	case "help", "-h", "-help", "--help":
		fmt.Print(usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// versionString reports the release version when the linker set one, and
// otherwise falls back to the module version recorded in the binary. Without
// the fallback every `go install ...@v1.0.0` build would claim to be "dev".
func versionString() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

const usage = `gitpass — a git-backed password manager

  gitpass                open the TUI
  gitpass init           create a vault
  gitpass clone <url>    fetch an existing vault
  gitpass remote <url>   point this vault at a git remote
  gitpass token          store an HTTPS access token
  gitpass sync           replicate through the remote
  gitpass add            add entries from JSON on stdin
  gitpass get <name>     print one entry's password
  gitpass totp <name>    print the current TOTP code
  gitpass gc [days]      drop tombstones older than days (default 90)
  gitpass version        print the version

The vault lives in $GITPASS_DIR (default ~/.local/share/gitpass/vault).
`

// vaultDir is where the repo lives. Overridable for a second vault or testing.
func vaultDir() (string, error) {
	if d := os.Getenv("GITPASS_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "gitpass", "vault"), nil
}

// prompt reads a passphrase without echoing it. It falls back to a plain read
// when stdin is not a terminal, so `echo pass | gitpass get x` works.
func prompt(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		var s string
		_, err := fmt.Scanln(&s)
		return s, err
	}
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// open unlocks the vault, prompting for the passphrase.
func open() (*vault.Vault, error) {
	dir, err := vaultDir()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, "identity.age")); err != nil {
		return nil, fmt.Errorf("no vault at %s — run `gitpass init` or `gitpass clone <url>`", dir)
	}
	if p := os.Getenv("GITPASS_PASSPHRASE"); p != "" {
		return vault.Open(dir, p)
	}
	p, err := prompt("Passphrase: ")
	if err != nil {
		return nil, err
	}
	return vault.Open(dir, p)
}

func cmdInit() error {
	dir, err := vaultDir()
	if err != nil {
		return err
	}
	suggestion, err := vault.Diceware(6)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, `Creating a vault at %s.

The key file is stored inside the repo, so your passphrase is the only thing
protecting it once the repo is pushed. Make it a strong one — here is a
generated suggestion worth about 77 bits:

    %s

`, dir, suggestion)

	p, err := prompt("Passphrase: ")
	if err != nil {
		return err
	}
	if err := vault.CheckPassphrase(p); err != nil {
		return err
	}
	again, err := prompt("Confirm: ")
	if err != nil {
		return err
	}
	if p != again {
		return errors.New("passphrases did not match")
	}
	if _, err := vault.Init(dir, p); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\nVault created. Next: `gitpass remote <url>` then `gitpass sync`.\n")
	return nil
}

func cmdClone(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: gitpass clone <url>")
	}
	dir, err := vaultDir()
	if err != nil {
		return err
	}
	url := args[0]
	var token string
	if strings.HasPrefix(url, "http") {
		if token, err = prompt("Access token (blank if public): "); err != nil {
			return err
		}
	}
	if err := sync.Clone(dir, url, token); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Cloned to %s.\n", dir)
	if token == "" {
		return nil
	}
	// Persist the token now that there is a vault key to encrypt it with.
	v, err := open()
	if err != nil {
		return err
	}
	return sync.SetToken(v, token)
}

func cmdRemote(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: gitpass remote <url>")
	}
	v, err := open()
	if err != nil {
		return err
	}
	return sync.SetRemote(v, args[0])
}

func cmdToken() error {
	v, err := open()
	if err != nil {
		return err
	}
	t, err := prompt("Access token: ")
	if err != nil {
		return err
	}
	return sync.SetToken(v, t)
}

func cmdSync() error {
	v, err := open()
	if err != nil {
		return err
	}
	r, err := sync.Sync(v)
	if err != nil {
		return err
	}
	fmt.Println(r)
	return nil
}

// cmdAdd reads one entry, or an array of them, as JSON on stdin. Handy for
// importing an export from another manager without inventing a flag per field.
func cmdAdd() error {
	v, err := open()
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := json.NewDecoder(os.Stdin).Decode(&raw); err != nil {
		return fmt.Errorf("reading JSON from stdin: %w", err)
	}
	var entries []vault.Entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		var one vault.Entry
		if err := json.Unmarshal(raw, &one); err != nil {
			return err
		}
		entries = []vault.Entry{one}
	}
	for _, e := range entries {
		e.ID = "" // always a fresh entry; editing goes through the TUI
		if _, err := v.Put(e); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "added %d %s\n", len(entries), map[bool]string{true: "entry", false: "entries"}[len(entries) == 1])
	return nil
}

// cmdGC drops old tombstones. The age limit exists so that a device which has
// been offline for a while cannot resurrect entries deleted elsewhere.
func cmdGC(args []string) error {
	age := vault.DefaultGCAge
	if len(args) == 1 {
		days, err := strconv.Atoi(args[0])
		if err != nil || days <= 0 {
			return fmt.Errorf("gc takes a positive number of days, got %q", args[0])
		}
		age = time.Duration(days) * 24 * time.Hour
	}
	v, err := open()
	if err != nil {
		return err
	}
	n, err := v.GC(age)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "dropped %d tombstone(s); run `gitpass sync` to publish\n", n)
	return nil
}

func cmdGet(args []string) error {
	e, err := find(args, "get")
	if err != nil {
		return err
	}
	fmt.Println(e.Password)
	return nil
}

func cmdTOTP(args []string) error {
	e, err := find(args, "totp")
	if err != nil {
		return err
	}
	code, _, err := e.Code(time.Now())
	if err != nil {
		return err
	}
	fmt.Println(code)
	return nil
}

// find locates the first live entry whose name contains the argument.
func find(args []string, cmd string) (vault.Entry, error) {
	var zero vault.Entry
	if len(args) != 1 {
		return zero, fmt.Errorf("usage: gitpass %s <name>", cmd)
	}
	v, err := open()
	if err != nil {
		return zero, err
	}
	entries, err := v.List()
	if err != nil {
		return zero, err
	}
	needle := strings.ToLower(args[0])
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name), needle) {
			return e, nil
		}
	}
	return zero, fmt.Errorf("no entry matching %q", args[0])
}
