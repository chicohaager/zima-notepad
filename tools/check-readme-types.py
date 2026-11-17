#!/usr/bin/env python3
"""Fail when the README table "Which files open" and the code disagree: every
extension and file name of web/src/editor.js (BY_EXT, BY_NAME), every syntax
label (LANGUAGES) and every encoding of internal/files/encoding.go must appear
in that section, and the section must name nothing the code lacks. Prints the
offending names, not just counts."""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def section():
    s = (ROOT / "README.md").read_text(encoding="utf-8")
    m = re.search(r"^## Which files open\n(.*?)^## ", s, re.S | re.M)
    if not m:
        sys.exit("README: section '## Which files open' missing")
    return m.group(1)


def main():
    js = (ROOT / "web/src/editor.js").read_text(encoding="utf-8")
    go = (ROOT / "internal/files/encoding.go").read_text(encoding="utf-8")
    sec = section()
    low = sec.lower()
    labels = re.findall(r"^  \w+: \['([^']+)'", js, re.M)
    exts, names = [], []
    for name, out in (("BY_EXT", exts), ("BY_NAME", names)):
        body = re.search(r"const " + name + r" = \{(.*?)\n\};", js, re.S).group(1)
        out += re.findall(r"'?([\w.+-]+)'?: '\w+'", body)
    encs = re.search(r"var Encodings = \[\]string\{(.*?)\}", go, re.S).group(1)
    encs = re.findall(r'"([^"]+)"', encs)
    missing = []
    missing += [f"syntax {l}" for l in labels if l != "Plain text" and f"| {l} |" not in sec]
    missing += [f"extension .{e}" for e in exts if f"`.{e}`" not in sec]
    missing += [f"file name {n}" for n in names if f"`{n}`" not in low]
    # How the README table spells each encoding id; an id the code gains without a
    # spelling here counts as missing, so a new encoding cannot slip past the table.
    spell = {"utf-8": "UTF-8", "utf-8-bom": "UTF-8 with BOM", "utf-16le": "UTF-16 LE", "utf-16be": "UTF-16 LE / BE",
             "windows-1252": "Windows-1252", "windows-1250": "Windows-1250", "windows-1251": "Windows-1251",
             "iso-8859-2": "ISO-8859-2", "iso-8859-5": "ISO-8859-5", "iso-8859-15": "ISO-8859-15", "koi8-r": "KOI8-R",
             "gb18030": "GB18030", "gbk": "GBK", "big5": "Big5", "shift_jis": "Shift_JIS", "euc-jp": "EUC-JP",
             "euc-kr": "EUC-KR"}
    enc_rows = "\n".join(l for l in sec.splitlines() if l.startswith("| ") and "✓" in l)
    missing += [f"encoding {e}" for e in encs if e not in spell or spell[e] not in enc_rows]
    rows = re.findall(r"^\| ([^|]+?) \| [^|]*`\.", sec, re.M)
    extra = [r for r in rows if r not in labels]
    shown = set(re.findall(r"`\.([\w+-]+)`", sec)) - {"txt", "log", "csv"}
    extra += [f".{e}" for e in sorted(shown - set(exts))]
    print(f"README types: {len(labels) - 1} syntaxes, {len(exts)} extensions, {len(names)} file names, {len(encs)} encodings checked")
    if missing or extra:
        for x in missing:
            print(f"  missing in README: {x}")
        for x in extra:
            print(f"  in README but not in the code: {x}")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
