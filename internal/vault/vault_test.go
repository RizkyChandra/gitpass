package vault

import (
	"encoding/json"
	"os"
	"strings"
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

func TestPaddingHidesEntrySize(t *testing.T) {
	dir := t.TempDir()
	v, _ := Init(dir, testPass)

	small, _ := v.Put(Entry{Name: "a", Password: "x"})
	big, _ := v.Put(Entry{Name: "b", Password: "y", Notes: strings.Repeat("secret diary ", 20)})

	sizeOf := func(e Entry) int64 {
		fi, err := os.Stat(entryPath(dir, e.ID))
		if err != nil {
			t.Fatal(err)
		}
		return fi.Size()
	}
	if sizeOf(small) != sizeOf(big) {
		t.Fatalf("entry size leaks content length: %d vs %d", sizeOf(small), sizeOf(big))
	}

	// Padding must not disturb the round trip, nor the age+jq recovery path,
	// which relies on the plaintext still being parseable JSON.
	got, err := v.Get(big.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Notes != big.Notes {
		t.Fatal("padding corrupted the entry")
	}
	raw, _ := os.ReadFile(entryPath(dir, big.ID))
	plain, err := v.Unseal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain)%padBlock != 0 {
		t.Fatalf("plaintext is %d bytes, not a multiple of %d", len(plain), padBlock)
	}
	var check map[string]any
	if err := json.Unmarshal(plain, &check); err != nil {
		t.Fatalf("padded plaintext is not valid JSON, breaking `age -d | jq`: %v", err)
	}
}

func TestGCDropsOnlyOldTombstones(t *testing.T) {
	dir := t.TempDir()
	v, _ := Init(dir, testPass)

	live, _ := v.Put(Entry{Name: "keep-me", Password: "p"})
	recent, _ := v.Put(Entry{Name: "just-deleted"})
	old, _ := v.Put(Entry{Name: "long-gone"})

	if err := v.Delete(recent.ID); err != nil {
		t.Fatal(err)
	}
	// Backdate one tombstone past the collection window.
	oldTomb, _ := v.Get(old.ID)
	oldTomb.Deleted = true
	oldTomb.UpdatedAt = time.Now().UTC().Add(-100 * 24 * time.Hour)
	if err := v.WriteRaw(oldTomb); err != nil {
		t.Fatal(err)
	}
	if err := v.CommitAll("backdate"); err != nil {
		t.Fatal(err)
	}

	n, err := v.GC(DefaultGCAge)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("dropped %d tombstones, want 1", n)
	}

	all, _ := v.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 files left, got %d", len(all))
	}
	if _, err := v.Get(old.ID); !os.IsNotExist(err) {
		t.Fatal("the old tombstone survived GC")
	}
	if _, err := v.Get(recent.ID); err != nil {
		t.Fatal("GC dropped a fresh tombstone, which would resurrect the entry elsewhere")
	}
	if _, err := v.Get(live.ID); err != nil {
		t.Fatal("GC touched a live entry")
	}

	// A zero window would collect everything, including deletes other devices
	// have not seen yet.
	if _, err := v.GC(0); err == nil {
		t.Fatal("GC accepted a zero age limit")
	}
}

// Regression: the Android app and JSON importers build entries with no
// timestamp, and the plain time.Time decoder rejects an empty string.
func TestEntryAcceptsMissingTimestamp(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"empty string", `{"name":"x","updated_at":""}`},
		{"absent", `{"name":"x"}`},
		{"null", `{"name":"x","updated_at":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var e Entry
			if err := json.Unmarshal([]byte(tc.body), &e); err != nil {
				t.Fatalf("rejected a legitimate entry: %v", err)
			}
			if e.Name != "x" || !e.UpdatedAt.IsZero() {
				t.Fatalf("decoded wrong: %+v", e)
			}
		})
	}

	// A real timestamp must still survive, and rubbish must still be rejected.
	var e Entry
	if err := json.Unmarshal([]byte(`{"updated_at":"2026-08-06T12:00:00Z"}`), &e); err != nil {
		t.Fatal(err)
	}
	if e.UpdatedAt.UTC().Format(time.RFC3339) != "2026-08-06T12:00:00Z" {
		t.Fatalf("lost the timestamp: %v", e.UpdatedAt)
	}
	if err := json.Unmarshal([]byte(`{"updated_at":"last tuesday"}`), &e); err == nil {
		t.Fatal("accepted a malformed timestamp")
	}

	// Round trip through Put, the path the mobile facade actually takes.
	v, _ := Init(t.TempDir(), testPass)
	var fresh Entry
	if err := json.Unmarshal([]byte(`{"name":"github.com","password":"p","updated_at":""}`), &fresh); err != nil {
		t.Fatal(err)
	}
	saved, err := v.Put(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if saved.UpdatedAt.IsZero() {
		t.Fatal("Put should have stamped the entry")
	}
}
