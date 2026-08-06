#!/usr/bin/env python3
"""Convert a KeePassXC CSV export into JSON for `gitpass add`.

    keepassxc-cli export --format csv db.kdbx > export.csv
    ./scripts/keepassxc-csv.py export.csv | gitpass add
    shred -u export.csv          # it is plaintext

Everything here is one-shot glue, which is why it is a script and not a
subcommand: gitpass already reads a JSON array on stdin.

A real CSV parser is not optional. KeePassXC quotes fields containing newlines
(backup codes in Notes routinely span lines) and doubles embedded quotes, both
of which line-oriented tools silently mangle.

Not carried over, because gitpass has nowhere to put them: Icon, Created, and
Last Modified. Entries are stamped with the time of import.
"""

import csv
import json
import sys


def convert(rows):
    entries = []
    for row in rows:
        name = (row.get("Title") or "").strip()
        password = row.get("Password") or ""
        # A row with neither a name nor a password carries nothing worth keeping.
        if not name and not password:
            continue

        entry = {
            "name": name or "untitled",
            # KeePassXC has one Username field whether it holds a login or an
            # email; gitpass falls back to email only when username is empty,
            # so putting it in username keeps autofill working either way.
            "username": (row.get("Username") or "").strip(),
            "password": password,
            "url": (row.get("URL") or "").strip(),
            "notes": row.get("Notes") or "",
            # Already an otpauth:// URI, which is exactly what gitpass stores.
            "totp": (row.get("TOTP") or "").strip(),
        }

        # Groups become tags. "Root" is KeePassXC's default and means nothing.
        group = (row.get("Group") or "").strip()
        if group and group != "Root":
            entry["tags"] = [g for g in group.split("/") if g and g != "Root"]

        entries.append({k: v for k, v in entry.items() if v})
    return entries


def main():
    if len(sys.argv) != 2:
        sys.exit(f"usage: {sys.argv[0]} <keepassxc-export.csv>")
    with open(sys.argv[1], newline="", encoding="utf-8") as fh:
        entries = convert(csv.DictReader(fh))

    json.dump(entries, sys.stdout, indent=2, ensure_ascii=False)
    sys.stdout.write("\n")

    totp = sum(1 for e in entries if "totp" in e)
    notes = sum(1 for e in entries if "notes" in e)
    blank = sum(1 for e in entries if "password" not in e)
    print(
        f"{len(entries)} entries — {totp} with TOTP, {notes} with notes, "
        f"{blank} with no password",
        file=sys.stderr,
    )


if __name__ == "__main__":
    main()
