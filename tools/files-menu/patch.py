#!/usr/bin/env python3
"""Add Notepad entries to the menus of the ZimaOS Files app (icewhale_files).

The Files app has no extension point for its menus: the entries are array literals in its bundles
(measured on 1.8.0 and 1.8.1-alpha1, 16.11.2026). This tool inserts entries into them:

  assets/Files-<hash>.js  "Open with Notepad" at the top of the file menu (right click and the
                          three-dot button share one function), for a file, not a folder, on
                          ZimaOS-HD; opens /modules/notepad/?open=/DATA/<path>.
  assets/main-<hash>.js   "New text file" after "New folder" in the menu of the empty area and in
                          the drop-down of the new-folder button, inside a folder of ZimaOS-HD;
                          opens /modules/notepad/?new=/DATA/<folder>, where Notepad asks for the name.

ZimaOS-HD is /DATA (same device and inode, measured on both hosts). Every link opens a new tab.

  python3 tools/files-menu/patch.py <Files-*.js|main-*.js> <out-dir>

Writes <out-dir>/<same name>. Refuses (exit 1) when an anchor is not found exactly once or the
file is already patched: a new Files build needs a look, not a guess.
"""
import re
import sys
from pathlib import Path

MARK = "/modules/notepad/?"
HD = r"/^\/media\/ZimaOS-HD"   # JS regex source, closed by the caller

LABELS = {
    "open": '{de:"Mit Notepad öffnen",fr:"Ouvrir avec Notepad",es:"Abrir con Notepad",zh:"用 Notepad 打开"}',
    "new": '{de:"Neue Textdatei",fr:"Nouveau fichier texte",es:"Nuevo archivo de texto",zh:"新建文本文件"}',
}
ENGLISH = {"open": "Open with Notepad", "new": "New text file"}


def label(kind):
    return ('(()=>{let l="";try{l=localStorage.getItem("lang")||""}catch(e){}'
            f'l=(l||navigator.language||"en").slice(0,2).toLowerCase();return {LABELS[kind]}[l]||"{ENGLISH[kind]}"}})()')


def to_data(expr):
    return f'encodeURIComponent({expr}.replace({HD}/,"/DATA"))'


# --- Files-<hash>.js: file menu --------------------------------------------------------------
# function U(){const oe=f.currentFolderExtension&&f.currentFolderExtension.lock_root_path;return[{label:a("actionbar.download"),command:()=>{vo(u.value)}}
# The minified names differ per build (1.8.0: U/oe/f/a/u, 1.8.1-alpha1: q/re/f/s/u).
FILE_MENU = re.compile(
    r'function \w+\(\)\{const \w+=(?P<st>\w+)\.currentFolderExtension&&(?P=st)\.currentFolderExtension\.lock_root_path;'
    r'return\[(?=\{label:\w+\("actionbar\.download"\),command:\(\)=>\{\w+\((?P<item>\w+)\.value\)\}\})'
)


def open_entry(m):
    v = f"{m['item']}.value"
    cond = f'!{v}.isFolder&&{HD}\\//.test({v}.path)'
    cmd = f'()=>{{window.open("{MARK}open="+{to_data(f"{v}.path")},"_blank")}}'
    return f'...({cond}?[{{label:{label("open")},command:{cmd}}},{{separator:!0}}]:[]),'


# --- main-<hash>.js: "New folder" in the empty-area menu and in the toolbar drop-down ----------
# command:()=>{var c;if((c=n.currentMenu)!=null&&c.read_only){Tt(lt("actionbar.readOnly"));return}XM(e)}},
READ_ONLY = (r'command:\(\)=>\{var (?P<v>\w+);if\(\((?P=v)=(?P<menu>\w+)\.currentMenu\)!=null&&(?P=v)\.read_only\)'
             r'\{(?P<toast>\w+)\((?P<t>\w+)\("actionbar\.readOnly"\)\);return\}\w+\((?P<store>\w+)\)\}\},')
# return[!e.isRoot&&!i&&{label:lt("actionbar.newFolder"),<READ_ONLY>   (1.8.0: e/i/lt/n/Tt, 1.8.1-alpha1: e/i/Xe/t/fn)
EMPTY_MENU = re.compile(r'return\[!\w+\.isRoot&&!(?P<smb>\w+)&&\{label:\w+\("actionbar\.newFolder"\),' + READ_ONLY)
# tooltip:{content:lt("actionbar.newFolder"),placement:"bottom"},items:[{label:lt("actionbar.newFolder"),<READ_ONLY>
TOOLBAR = re.compile(r'tooltip:\{content:(?P<t0>\w+)\("actionbar\.newFolder"\),placement:"bottom"\},'
                     r'items:\[\{label:(?P=t0)\("actionbar\.newFolder"\),' + READ_ONLY)


def new_entry(m):
    st, menu = m["store"], m["menu"]
    # The folder itself (ZimaOS-HD) or one below it; read-only folders get the same toast as "New folder".
    cond = f'{HD}(\\/|$)/.test({st}.currentPath||"")'
    cmd = (f'()=>{{if({menu}.currentMenu&&{menu}.currentMenu.read_only){{{m["toast"]}({m["t"]}("actionbar.readOnly"));return}}'
           f'window.open("{MARK}new="+{to_data(f"{st}.currentPath")},"_blank")}}')
    return f'...({cond}?[{{label:{label("new")},command:{cmd}}}]:[]),'


PATCHES = {
    "Files": [("file menu", FILE_MENU, open_entry)],
    "main": [("empty-area menu", EMPTY_MENU, new_entry), ("toolbar new-folder drop-down", TOOLBAR, new_entry)],
}


def kind_of(name):
    m = re.fullmatch(r"(Files|main)-[\w-]+\.js", name)
    return m.group(1) if m else None


def patch(text, kind="Files"):
    if MARK in text:
        raise ValueError("already patched")
    edits = []
    for what, anchor, build in PATCHES[kind]:
        found = list(anchor.finditer(text))
        if len(found) != 1:
            raise ValueError(f"{what}: anchor found {len(found)} times, expected 1")
        edits.append((found[0].end(), build(found[0])))
    for pos, ins in sorted(edits, reverse=True):
        text = text[:pos] + ins + text[pos:]
    return text


def main(argv):
    if len(argv) != 3:
        print("usage: patch.py <Files-*.js|main-*.js> <out-dir>", file=sys.stderr)
        return 2
    src, out = Path(argv[1]), Path(argv[2])
    kind = kind_of(src.name)
    if not kind:
        print(f"refused: {src.name} is neither Files-<hash>.js nor main-<hash>.js", file=sys.stderr)
        return 1
    try:
        new = patch(src.read_text(encoding="utf-8"), kind)
    except ValueError as e:
        print(f"refused: {src}: {e}", file=sys.stderr)
        return 1
    out.mkdir(parents=True, exist_ok=True)
    (out / src.name).write_text(new, encoding="utf-8")
    print(f"patched {src.name}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
