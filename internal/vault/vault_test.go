package vault

import (
	"testing"
	"time"
)

const testPass = "correct-horse-battery-staple"

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	v, err := Init(dir, testPass)
	if err != nil {
		t.Fatal(err)
	}
	e, err := v.Put(Entry{Name: "github.com", Username: "alice", Password: "hunter2"})
	if err != nil {
		t.Fatal(err)
	}
	if e.ID == "" || e.UpdatedAt.IsZero() {
		t.Fatalf("Put did not assign id/timestamp: %+v", e)
	}

	// Reopening must decrypt with nothing but the passphrase.
	v2, err := Open(dir, testPass)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v2.Get(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "hunter2" || got.Username != "alice" || got.Name != "github.com" {
		t.Fatalf("round trip lost data: %+v", got)
	}

	if _, err := Open(dir, "wrong-passphrase-entirely"); err == nil {
		t.Fatal("opened vault with the wrong passphrase")
	}
}

func TestDeleteLeavesTombstone(t *testing.T) {
	dir := t.TempDir()
	v, _ := Init(dir, testPass)
	e, _ := v.Put(Entry{Name: "gone", Password: "secret"})

	if err := v.Delete(e.ID); err != nil {
		t.Fatal(err)
	}
	live, err := v.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 0 {
		t.Fatalf("deleted entry still listed: %+v", live)
	}
	all, _ := v.All()
	if len(all) != 1 || !all[0].Deleted {
		t.Fatalf("expected one tombstone, got %+v", all)
	}
	// The tombstone must not still carry the secret it was hiding.
	if all[0].Password != "" {
		t.Fatal("tombstone retained the password")
	}
}

func TestTOTPMatchesRFC6238(t *testing.T) {
	// RFC 6238 test vector: seed "12345678901234567890" at T=59s.
	e := Entry{TOTP: "otpauth://totp/x?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"}
	code, left, err := e.Code(time.Unix(59, 0))
	if err != nil {
		t.Fatal(err)
	}
	if code != "287082" {
		t.Fatalf("got %s, want 287082", code)
	}
	if left != 1 {
		t.Fatalf("got %ds left, want 1", left)
	}
	// A bare secret, as copied off a site that shows only the key, must work too.
	bare := Entry{TOTP: "gezd gnbv gy3t qojq gezd gnbv gy3t qojq"}
	if c, _, err := bare.Code(time.Unix(59, 0)); err != nil || c != "287082" {
		t.Fatalf("bare secret: got %q %v", c, err)
	}
}

func TestPassphraseFloor(t *testing.T) {
	if err := CheckPassphrase("short"); err == nil {
		t.Fatal("accepted a 5-character passphrase")
	}
	if err := CheckPassphrase("aaaaaaaaaaaaaaaa"); err == nil {
		t.Fatal("accepted a repetitive passphrase")
	}
	if err := CheckPassphrase(testPass); err != nil {
		t.Fatal(err)
	}
	p, err := Diceware(6)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckPassphrase(p); err != nil {
		t.Fatalf("generated passphrase rejected by our own check: %q", p)
	}
}
