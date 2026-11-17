#!/usr/bin/env python3
"""Runs the menu entry that patch.py inserts, in node, against a stand-in of the Files menu function.

The stand-in has the shape of the real one (1.8.0 and 1.8.1-alpha1 bundles, 16.11.2026) with other
minified names; the real bundles are IceWhale's code and stay out of this repository.

  python3 tools/files-menu/test_patch.py
"""
import json
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import patch  # noqa: E402  (after the sys.path line)

STAND_IN = (
    'const vo=()=>{};'
    'function Q(){const zz=g.currentFolderExtension&&g.currentFolderExtension.lock_root_path;'
    'return[{label:t("actionbar.download"),command:()=>{vo(it.value)}},{separator:!0}].filter(x=>x)}'
)

# main-<hash>.js: the empty-area menu and the toolbar drop-down, both with "New folder" first.
MAIN_STAND_IN = (
    'const XM=()=>{};'
    'function EMPTY(e,n,i){return[!e.isRoot&&!i&&{label:lt("actionbar.newFolder"),command:()=>{var c;'
    'if((c=n.currentMenu)!=null&&c.read_only){Tt(lt("actionbar.readOnly"));return}XM(e)}},'
    '{label:lt("actionbar.selectAll"),command:()=>{}}].filter(c=>c)}'
    'function TOOLBAR(e){return{icon:"x",tooltip:{content:lt("actionbar.newFolder"),placement:"bottom"},'
    'items:[{label:lt("actionbar.newFolder"),command:()=>{var a;if((a=e.currentMenu)!=null&&a.read_only)'
    '{Tt(lt("actionbar.readOnly"));return}XM(e)}},...(e.locked?[]:[])]}}'
)

FAILS = []


def check(cond, what):
    print(("ok   " if cond else "FAIL ") + what)
    if not cond:
        FAILS.append(what)


def run(code, path, folder, lang):
    js = f"""
const opened = [];
globalThis.window = {{ open: (u, t) => opened.push([u, t]) }};
globalThis.localStorage = {{ getItem: (k) => k === 'lang' ? {json.dumps(lang)} : null }};
Object.defineProperty(globalThis, 'navigator', {{ value: {{ language: 'en-US' }}, configurable: true }});
const g = {{ currentFolderExtension: null }};
const t = (k) => k;
const it = {{ value: {{ path: {json.dumps(path)}, isFolder: {json.dumps(folder)} }} }};
{code}
const items = Q();
if (items[0].command && items[0].label !== 'actionbar.download') items[0].command();
console.log(JSON.stringify({{ labels: items.map((x) => x.label || (x.separator ? '---' : '?')), opened }}));
"""
    with tempfile.NamedTemporaryFile("w", suffix=".mjs", delete=False) as f:
        f.write(js)
    out = subprocess.run(["node", f.name], capture_output=True, text=True)
    Path(f.name).unlink()
    if out.returncode != 0:
        return {"error": out.stderr.strip()}
    return json.loads(out.stdout)


def run_main(code, where, current, read_only, lang):
    call = "EMPTY(e, n, false)" if where == "EMPTY" else "TOOLBAR(e).items"
    js = f"""
const opened = [], toasts = [];
globalThis.window = {{ open: (u, t) => opened.push([u, t]) }};
globalThis.localStorage = {{ getItem: (k) => k === 'lang' ? {json.dumps(lang)} : null }};
Object.defineProperty(globalThis, 'navigator', {{ value: {{ language: 'en-US' }}, configurable: true }});
const lt = (k) => k, Tt = (m) => toasts.push(m);
const ro = {{ read_only: {json.dumps(read_only)} }};
const e = {{ isRoot: false, currentPath: {json.dumps(current)}, currentMenu: ro }}, n = {{ currentMenu: ro }};
{code}
const items = {call};
const mine = items.find((x) => x.label !== 'actionbar.newFolder' && x.label !== 'actionbar.selectAll');
if (mine) mine.command();
console.log(JSON.stringify({{ labels: items.map((x) => x.label), opened, toasts }}));
"""
    with tempfile.NamedTemporaryFile("w", suffix=".mjs", delete=False) as f:
        f.write(js)
    out = subprocess.run(["node", f.name], capture_output=True, text=True)
    Path(f.name).unlink()
    if out.returncode != 0:
        return {"error": out.stderr.strip()}
    return json.loads(out.stdout)


def main():
    code = patch.patch(STAND_IN)

    r = run(code, "/media/ZimaOS-HD/Documents/notes.md", False, "de_de")
    check(r.get("labels", [None])[0] == "Mit Notepad öffnen", f"file on ZimaOS-HD: German entry first {r}")
    check(r.get("labels", [])[1:3] == ["---", "actionbar.download"], f"separator, then the original entries {r}")
    check(r.get("opened") == [["/modules/notepad/?open=%2FDATA%2FDocuments%2Fnotes.md", "_blank"]],
          f"opens /DATA path in a new tab {r}")

    r = run(code, "/media/ZimaOS-HD/Ordner mit Ä/a&b.txt", False, "fr_fr")
    check(r.get("labels", [None])[0] == "Ouvrir avec Notepad", f"French label {r}")
    check(r.get("opened") == [["/modules/notepad/?open=%2FDATA%2FOrdner%20mit%20%C3%84%2Fa%26b.txt", "_blank"]],
          f"space, umlaut and & are encoded {r}")

    r = run(code, "/media/ZimaOS-HD/x.txt", False, None)
    check(r.get("labels", [None])[0] == "Open with Notepad", f"no shell language: navigator en-US gives English {r}")

    r = run(code, "/media/ZimaOS-HD/Documents", True, "de_de")
    check(r.get("labels") == ["actionbar.download", "---"], f"folder: menu unchanged {r}")

    r = run(code, "/media/sda1/notes.md", False, "de_de")
    check(r.get("labels") == ["actionbar.download", "---"], f"file on another disk (not /DATA): menu unchanged {r}")

    r = run(code, "/media/ZimaOS-HD-2/notes.md", False, "de_de")
    check(r.get("labels") == ["actionbar.download", "---"], f"a volume whose name only starts with ZimaOS-HD: unchanged {r}")

    main = patch.patch(MAIN_STAND_IN, "main")
    for where in ("EMPTY", "TOOLBAR"):
        r = run_main(main, where, "/media/ZimaOS-HD/Documents", False, "de_de")
        check(r.get("labels", [])[:2] == ["actionbar.newFolder", "Neue Textdatei"], f"{where}: 'Neue Textdatei' after 'New folder' {r}")
        check(r.get("opened") == [["/modules/notepad/?new=%2FDATA%2FDocuments", "_blank"]], f"{where}: opens ?new= with the /DATA folder {r}")
        r = run_main(main, where, "/media/ZimaOS-HD", False, "en_us")
        check(r.get("opened") == [["/modules/notepad/?new=%2FDATA", "_blank"]] and r["labels"][1] == "New text file",
              f"{where}: top of ZimaOS-HD gives /DATA, English label {r}")
        r = run_main(main, where, "/media/ZimaOS-HD/Documents", True, "de_de")
        check(r.get("opened") == [] and r.get("toasts") == ["actionbar.readOnly"], f"{where}: read-only folder: toast, no tab {r}")
        for other in ("/media/sdc1/x", "/media/ZimaOS-HD-2", ""):
            r = run_main(main, where, other, False, "de_de")
            check("Neue Textdatei" not in r.get("labels", ["?"]), f"{where}: no entry in {other!r} {r}")
    try:
        patch.patch(MAIN_STAND_IN.replace('placement:"bottom"', 'placement:"top"'), "main")
        check(False, "refuses: main bundle with only one of its two anchors")
    except ValueError:
        check(True, "refuses: main bundle with only one of its two anchors")

    for bad, why in ((STAND_IN.replace('"actionbar.download"', '"actionbar.save"'), "anchor missing"),
                     (STAND_IN + STAND_IN.replace("function Q", "function R"), "anchor twice"),
                     (code, "already patched")):
        try:
            patch.patch(bad)
            check(False, f"refuses: {why}")
        except ValueError:
            check(True, f"refuses: {why}")

    print(f"\n{len(FAILS)} failure(s)")
    return 1 if FAILS else 0


if __name__ == "__main__":
    sys.exit(main())
