package vault

import (
	"crypto/rand"
	_ "embed"
	"fmt"
	"math/big"
	"strings"
	"unicode"
)

// identity.age lives inside the vault repo, so whoever holds the repo holds the
// key file too and the passphrase is the only remaining barrier. That makes a
// strength floor mandatory rather than a nicety.
//
//go:embed wordlist.txt
var wordlistData string

var wordlist = strings.Fields(wordlistData)

const minPassphraseLen = 12

// CheckPassphrase rejects passphrases too weak to be the sole protection on a
// vault whose key file is published to a git host.
//
// ponytail: a length-and-variety floor, not a real entropy estimate. Swap in
// zxcvbn if users start picking "aaaaaaaaaaaa".
func CheckPassphrase(p string) error {
	if len([]rune(p)) < minPassphraseLen {
		return fmt.Errorf("passphrase must be at least %d characters (it is the only thing protecting the key stored in the repo)", minPassphraseLen)
	}
	distinct := map[rune]bool{}
	for _, r := range p {
		distinct[unicode.ToLower(r)] = true
	}
	if len(distinct) < 5 {
		return fmt.Errorf("passphrase is too repetitive")
	}
	return nil
}

const passwordAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789!@#$%^&*-_=+"

// RandomPassword generates a password for a single site. Unlike the master
// passphrase this is never typed by hand, so it is dense rather than memorable.
// The alphabet omits l/I/1 and O/0 for the times you do have to read it aloud.
func RandomPassword(n int) (string, error) {
	b := make([]byte, n)
	max := big.NewInt(int64(len(passwordAlphabet)))
	for i := range b {
		k, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = passwordAlphabet[k.Int64()]
	}
	return string(b), nil
}

// Diceware returns a random n-word passphrase from the EFF long wordlist.
// Six words is ~77 bits.
func Diceware(n int) (string, error) {
	words := make([]string, n)
	max := big.NewInt(int64(len(wordlist)))
	for i := range words {
		k, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		words[i] = wordlist[k.Int64()]
	}
	return strings.Join(words, "-"), nil
}
