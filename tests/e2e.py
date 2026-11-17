#!/usr/bin/env python3
"""Browser test of the Notepad tile (Playwright, real Chromium).

Starts its own notepad-dev on a copy of testdata/sample, so nothing outside a
temporary folder changes, and walks the flows of the spec (F1–F7): tree, open,
edit, save with encoding and line ending kept, conflict, new file, find and
replace, unsaved warning, language from the ZimaOS shell, light/dark, phone.

  ~/.venvs/playwright/bin/python3 tests/e2e.py [--shots DIR]

Needs build/notepad-dev and build/dev-web (sh check.sh builds both).
"""
import argparse
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from urllib.parse import quote

from playwright.sync_api import expect, sync_playwright

HERE = Path(__file__).resolve().parent.parent
FAILS = []


def check(cond, what):
    print(("ok   " if cond else "FAIL ") + what)
    if not cond:
        FAILS.append(what)


def free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    p = s.getsockname()[1]
    s.close()
    return p


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--shots", default="")
    a = ap.parse_args()
    shots = Path(a.shots) if a.shots else None
    if shots:
        shots.mkdir(parents=True, exist_ok=True)
    tmp = Path(tempfile.mkdtemp(prefix="notepad-e2e-"))
    data = tmp / "DATA"
    shutil.copytree(HERE / "testdata" / "sample", data)
    ini = data / "Documents" / "windows-ansi.ini"
    os.chmod(ini, 0o640)
    ro = data / "Documents" / "ro.txt"
    ro.write_text("protected\n")
    os.chmod(ro, 0o444)
    # A real /DATA has dozens of folders (the test host: more than 40 at the top level): the tree must scroll
    # inside its card, not stretch the page (found on the test host, 14.11.2026).
    for i in range(80):
        (data / f"folder-{i:02d}").mkdir()
    # Hidden and system entries as on the test host's /DATA (.casaos, .media, .swapfile, lost+found; 16.11.2026):
    # hidden by default, a checkbox shows them (forum request, 16.11.2026).
    (data / ".casaos").mkdir()
    (data / ".swapfile").write_text("x")
    (data / "lost+found").mkdir()
    (data / "Documents" / ".hidden-note").write_text("secret\n")
    (data / "OnlyDots").mkdir()
    (data / "OnlyDots" / ".env").write_text("A=1\n")
    # Files in encodings detection cannot tell from Windows-1252 (Rückmeldung 17.11.2026: GBK showed mojibake, silently).
    # Bytes as iconv -t GBK writes them: 中文测试 / an invalid GBK pair.
    (data / "Charsets").mkdir()
    (data / "Charsets" / "zh-gbk.txt").write_bytes(b"\xd6\xd0\xce\xc4\xb2\xe2\xca\xd4\n")
    (data / "Charsets" / "broken-gbk.txt").write_bytes(b"a\xfe\xfeb\n")
    (data / "Charsets" / "CMakeLists.txt").write_text("project(demo)\n")
    port = free_port()
    srv = subprocess.Popen([str(HERE / "build" / "notepad-dev"), "-root", str(data), "-web", str(HERE / "build" / "dev-web"),
                            "-addr", f"127.0.0.1:{port}"], stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    base = f"http://127.0.0.1:{port}/modules/notepad/"
    try:
        for _ in range(50):
            try:
                socket.create_connection(("127.0.0.1", port), timeout=0.2).close()
                break
            except OSError:
                time.sleep(0.1)
        with sync_playwright() as p:
            b = p.chromium.launch()
            run(b, base, data, ini, shots)
            b.close()
    finally:
        srv.terminate()
        srv.wait(timeout=5)
        shutil.rmtree(tmp, ignore_errors=True)
    print(f"\n{len(FAILS)} failure(s)")
    return 1 if FAILS else 0


def tree_click(page, name):
    page.locator(".np-node", has_text=name).first.click()


def run(b, base, data, ini, shots):
    errors = []
    ctx = b.new_context(viewport={"width": 1280, "height": 800}, locale="de-DE")
    # The ZimaOS shell keeps its language in localStorage.lang; the tile shares the origin.
    ctx.add_init_script("try { if (!localStorage.getItem('lang')) localStorage.setItem('lang', 'de_DE'); } catch (e) {}")
    page = ctx.new_page()
    page.on("pageerror", lambda e: errors.append(str(e)))
    # Failed requests are counted from the responses (with their URL); 409/422 of /write are provoked below.
    page.on("console", lambda m: errors.append(m.text) if m.type == "error" and not m.text.startswith("Failed to load resource") else None)
    bad = []
    writes = []
    page.on("request", lambda r: writes.append(r.url) if "/notepad/api/write" in r.url else None)
    page.on("response", lambda r: bad.append(f"{r.status} {r.url.split('?')[0]}") if r.status >= 400 else None)
    page.goto(base)
    expect(page.locator("#newFileBtn")).to_have_text("Neue Datei")
    check(page.locator("html").get_attribute("lang") == "de", "language follows the ZimaOS shell (de_DE -> de)")

    # layout: the page itself does not scroll on the desktop, the tree scrolls inside its card
    layout = page.evaluate("""() => ({page: document.documentElement.scrollHeight, view: innerHeight,
        tree: document.querySelector('#treeBody').scrollHeight, treeBox: document.querySelector('#treeBody').clientHeight})""")
    check(layout["page"] <= layout["view"] and layout["tree"] > layout["treeBox"], f"desktop layout: page fits, tree scrolls {layout}")

    # F1 tree
    expect(page.locator(".np-node", has_text="Documents")).to_be_visible()
    tree_click(page, "Documents")
    expect(page.locator(".np-node", has_text="windows-ansi.ini")).to_be_visible()

    # H hidden and system entries: off by default, the checkbox shows them, the choice is kept
    def node_shown(name):
        loc = page.locator(".np-node", has_text=name)
        return loc.count() > 0 and loc.first.is_visible()
    check(not page.locator("#hiddenToggle").is_checked(), "H checkbox 'show hidden' is off by default")
    for name in (".casaos", ".swapfile", "lost+found", ".hidden-note"):
        check(not node_shown(name), f"H {name} is hidden by default")
    check(node_shown("AppData") and node_shown("notes.md"), "H ordinary entries stay visible")
    tree_click(page, "OnlyDots")
    only = page.locator(".np-only-hidden", has_text="Nur versteckte Einträge (1)")
    expect(only).to_be_visible(timeout=5000)
    check(only.count() == 1 and only.is_visible(), "H a folder with only hidden entries says so instead of looking empty")
    page.locator("#hiddenToggle").check()
    for name in (".casaos", ".swapfile", "lost+found", ".hidden-note"):
        check(node_shown(name), f"H {name} shown after ticking the checkbox")
    check(node_shown("windows-ansi.ini"), "H the opened folder stays open when the checkbox changes")
    check(node_shown(".env") and not only.is_visible(), "H shown: the hidden entry instead of the note")
    page.reload()
    expect(page.locator(".np-node", has_text=".casaos")).to_be_visible()
    check(page.locator("#hiddenToggle").is_checked(), "H the choice survives a reload")
    page.locator("#hiddenToggle").uncheck()
    check(not node_shown(".casaos"), "H unticking hides them again")
    # creating a dot name with hidden entries off turns the display on, the new entry is not lost from sight
    page.locator("#newFolderBtn").click()
    page.locator("#dlgInput").fill(".config")
    page.locator("#dlgActions button", has_text="Anlegen").click()
    expect(page.locator(".np-node", has_text=".config")).to_be_visible()
    check(page.locator("#hiddenToggle").is_checked() and (data / ".config").is_dir(), "H new .config created and shown")
    page.locator("#hiddenToggle").uncheck()
    tree_click(page, "Documents")
    expect(page.locator(".np-node", has_text="windows-ansi.ini")).to_be_visible()

    # F2 open with syntax and encoding detected
    tree_click(page, "windows-ansi.ini")
    expect(page.locator(".np-tab.active")).to_contain_text("windows-ansi.ini")
    expect(page.locator(".np-editor:not([hidden]) .cm-content")).to_contain_text("Grüße")
    check(page.locator("#encPick").input_value() == "windows-1252", "F2 encoding windows-1252 detected")
    check(page.locator("#eolPick").input_value() == "crlf", "F2 line ending CRLF detected")
    check(page.locator("#langPick").input_value() == "ini", "F2 syntax INI chosen from the extension")
    # Rückmeldung 17.11.2026: a German ANSI file must not warn (it did for every Windows-1252 file before)
    check(not page.locator("#barGuess").is_visible() and "Windows-1252 gelesen" not in page.locator("#status").inner_text(),
          "F2 a German Windows-1252 file opens without the guess warning")
    tree_click(page, "windows-ansi.ini")
    check(page.locator(".np-tab").count() == 1, "F2 second click switches, no second tab")

    # F3 edit and save: bytes keep CRLF and windows-1252, mode stays 0640
    page.locator(".np-editor:not([hidden]) .cm-content").click()
    page.keyboard.press("Control+End")
    page.keyboard.type("x=ä")
    expect(page.locator("#barState")).to_have_text("Geändert")
    expect(page.locator(".np-tab.active .np-dirty")).to_be_visible()
    page.keyboard.press("Control+s")
    expect(page.locator("#status")).to_contain_text("gespeichert")
    raw = ini.read_bytes()
    check(raw == b"[general]\r\nname=Gr\xfc\xdfe\r\nx=\xe4", f"F3 bytes on disk keep windows-1252 and CRLF: {raw!r}")
    check(oct(ini.stat().st_mode & 0o777) == "0o640", "F3 mode 0640 kept")
    expect(page.locator("#barState")).to_have_text("Gespeichert")

    # F3b a character windows-1252 cannot store: error, file unchanged
    page.keyboard.type("✓")
    page.locator("#saveBtn").click()
    expect(page.locator("#status")).to_contain_text("UTF-8")
    check(ini.read_bytes() == raw, "F3b unencodable character refused, file unchanged")
    page.locator("#encPick").select_option("utf-8")
    expect(page.locator("#dlgTitle")).to_have_text("Kodierung UTF-8")
    page.locator("#dlgActions button", has_text="Beim Speichern umwandeln").click()
    page.locator("#saveBtn").click()
    expect(page.locator("#status")).to_contain_text("gespeichert")
    check(ini.read_bytes() == "[general]\r\nname=Grüße\r\nx=ä✓".encode(), "F3b saved as UTF-8 after switching")

    # F4 conflict: changed on disk, then saved in the tile
    ini.write_bytes(b"changed outside\r\n")
    page.locator(".np-editor:not([hidden]) .cm-content").click()
    page.keyboard.type("!")
    page.locator("#saveBtn").click()
    expect(page.locator("#dlg")).to_be_visible()
    expect(page.locator("#dlgTitle")).to_have_text("Datei auf der Platte geändert")
    box = page.locator("#dlg").bounding_box()
    check(abs(box["x"] + box["width"] / 2 - 640) < 4 and box["y"] > 40, f"F4 dialog centred: {box}")
    if shots:
        page.screenshot(path=str(shots / "conflict-de.png"))
    page.locator("#dlgActions button", has_text="Von Platte neu laden").click()
    expect(page.locator(".np-editor:not([hidden]) .cm-content")).to_contain_text("changed outside")
    check(ini.read_bytes() == b"changed outside\r\n", "F4 conflict: reload keeps the outside change")

    # F5 new file in the current folder, then type and save
    page.locator("#newFileBtn").click()
    page.locator("#dlgInput").fill("neu.conf")
    page.locator("#dlgActions button", has_text="Anlegen").click()
    expect(page.locator(".np-tab.active")).to_contain_text("neu.conf")
    new = data / "Documents" / "neu.conf"
    check(new.exists() and oct(new.stat().st_mode & 0o777) == "0o644", "F5 new file created with 0644")
    page.locator(".np-editor:not([hidden]) .cm-content").click()
    page.keyboard.type("key = value\n")
    before = len(writes)
    page.keyboard.press("Control+s")
    expect(page.locator("#status")).to_contain_text("neu.conf gespeichert")
    page.wait_for_timeout(500)
    check(len(writes) - before == 1, f"Ctrl+S sends exactly one write ({len(writes) - before})")
    check(new.read_text() == "key = value\n", "F5 new file saved")

    # F6 find and replace
    page.locator(".np-editor:not([hidden]) .cm-content").click()
    page.keyboard.press("Control+f")
    if shots:
        page.screenshot(path=str(shots / "search-de.png"))
    page.locator(".np-editor:not([hidden]) .cm-search input[name=search]").fill("value")
    page.locator(".np-editor:not([hidden]) .cm-search input[name=replace]").fill("wert")
    page.locator(".np-editor:not([hidden]) .cm-search button[name=replaceAll]").click()
    page.keyboard.press("Escape")
    expect(page.locator(".np-editor:not([hidden]) .cm-content")).to_contain_text("key = wert")

    # F7 unsaved changes: closing the tab asks
    page.locator(".np-tab.active .np-x").click()
    expect(page.locator("#dlg")).to_be_visible()
    expect(page.locator("#dlgTitle")).to_have_text("Ungespeicherte Änderungen")
    page.locator("#dlgActions button", has_text="Abbrechen").click()
    check(page.locator(".np-tab", has_text="neu.conf").count() == 1, "F7 cancel keeps the dirty tab")

    # Review 14.11.2026, each case failed before its fix.
    # R1 Enter in the name field creates (the form's implicit submit took Cancel)
    page.locator("#newFileBtn").click()
    page.locator("#dlgInput").fill("enter.txt")
    page.keyboard.press("Enter")
    expect(page.locator(".np-tab.active")).to_contain_text("enter.txt")
    enter = data / "Documents" / "enter.txt"
    check(enter.exists(), "R1 Enter in the new-file dialog creates the file")

    # R2 typing while a save is on its way keeps the tab modified
    held = []
    page.route("**/notepad/api/write*", lambda route: held.append(route))
    page.locator(".np-editor:not([hidden]) .cm-content").click()
    page.keyboard.type("one")
    page.locator("#saveBtn").click()
    for _ in range(50):
        if held:
            break
        page.wait_for_timeout(50)
    page.locator(".np-editor:not([hidden]) .cm-content").click()
    page.keyboard.press("Control+End")
    page.keyboard.type("TWO")
    held[0].continue_()
    expect(page.locator("#status")).to_contain_text("enter.txt gespeichert")
    page.unroute("**/notepad/api/write*")
    check(enter.read_text() == "one" and page.locator("#barState").inner_text() == "Geändert"
          and page.locator(".np-tab.active .np-dirty").is_visible(), f"R2 text typed during a save stays modified (disk {enter.read_text()!r})")
    page.keyboard.press("Control+s")
    expect(page.locator("#barState")).to_have_text("Gespeichert")

    # R3 a line-ending change stays a modification after type + undo
    page.locator("#eolPick").select_option("crlf")
    expect(page.locator("#barState")).to_have_text("Geändert")
    page.locator(".np-editor:not([hidden]) .cm-content").click()
    page.wait_for_timeout(600)  # CodeMirror groups edits within 500 ms into one undo step
    page.keyboard.type("z")
    page.keyboard.press("Control+z")
    check(page.locator("#barState").inner_text() == "Geändert", "R3 EOL change survives type + undo")
    page.locator("#eolPick").select_option("lf")
    expect(page.locator("#barState")).to_have_text("Gespeichert")

    # R4 a file deleted outside the editor can be created again on save
    enter.unlink()
    page.locator(".np-editor:not([hidden]) .cm-content").click()
    page.keyboard.type("!")
    page.locator("#saveBtn").click()
    expect(page.locator("#dlgTitle")).to_have_text("Datei existiert nicht mehr")
    page.locator("#dlgActions button", has_text="Anlegen und speichern").click()
    for _ in range(50):  # the status still shows the previous save; wait for the file itself
        if enter.exists() and enter.read_text():
            break
        page.wait_for_timeout(100)
    check(enter.exists() and enter.read_text() == "oneTWO!", f"R4 deleted file recreated with the tab's text: {enter.exists() and enter.read_text()!r}")

    # R5 a double click opens one tab
    page.locator(".np-node", has_text="notes.md").dblclick()
    expect(page.locator(".np-tab.active")).to_contain_text("notes.md")
    page.wait_for_timeout(300)
    check(page.locator(".np-tab", has_text="notes.md").count() == 1, "R5 double click opens one tab")

    # R6 a write-protected file opens read-only
    tree_click(page, "ro.txt")
    expect(page.locator(".np-tab.active")).to_contain_text("ro.txt")
    page.locator(".np-editor:not([hidden]) .cm-content").click()
    page.keyboard.type("typed")
    ro_state = {"pill": page.locator("#barRo").is_visible(), "save disabled": page.locator("#saveBtn").is_disabled(),
                "text": page.locator(".np-editor:not([hidden]) .cm-content").inner_text()}
    check(ro_state["pill"] and ro_state["save disabled"] and "typed" not in ro_state["text"],
          f"R6 write-protected file: typing changes nothing, save disabled {ro_state}")

    # R7 tabs work with the keyboard
    page.locator(".np-tab.active").focus()
    page.keyboard.press("ArrowLeft")
    expect(page.locator(".np-tab.active")).to_contain_text("notes.md")
    check(page.locator(".np-tab.active").get_attribute("tabindex") == "0", "R7 arrow keys move between tabs")

    # E reopen with encoding: a GBK file is guessed as Windows-1252, "Neu öffnen" as GBK shows the text,
    # a save keeps GBK bytes, invalid bytes open read-only, a page reload keeps the chosen encoding
    zh = data / "Charsets" / "zh-gbk.txt"
    tree_click(page, "Charsets")
    tree_click(page, "zh-gbk.txt")
    expect(page.locator(".np-tab.active")).to_contain_text("zh-gbk.txt")
    expect(page.locator("#status")).to_contain_text("Windows-1252 gelesen")
    check(page.locator("#barGuess").is_visible() and page.locator("#encPick").input_value() == "windows-1252",
          "E1 GBK file opens as guessed Windows-1252")
    page.locator("#encPick").select_option("gbk")
    expect(page.locator("#dlgTitle")).to_have_text("Kodierung GBK (简体中文)")
    page.locator("#dlgActions button", has_text="Abbrechen").click()
    expect(page.locator("#encPick")).to_have_value("windows-1252")
    check(page.locator("#encPick").input_value() == "windows-1252", "E2 cancel puts the picker back")
    page.locator("#encPick").select_option("gbk")
    page.locator("#dlgActions button", has_text="Neu öffnen").click()
    expect(page.locator(".np-editor:not([hidden]) .cm-content")).to_have_text("中文测试")
    check(not page.locator("#barGuess").is_visible() and page.locator("#encPick").input_value() == "gbk"
          and page.locator("#barState").inner_text() == "Gespeichert", "E3 reopened as GBK: text right, not guessed, not modified")
    page.locator(".np-editor:not([hidden]) .cm-content").click()
    page.keyboard.press("Control+End")
    page.keyboard.type("中")
    page.keyboard.press("Control+s")
    expect(page.locator("#status")).to_contain_text("gespeichert")
    check(zh.read_bytes() == b"\xd6\xd0\xce\xc4\xb2\xe2\xca\xd4\n\xd6\xd0", f"E4 saved in GBK: {zh.read_bytes()!r}")
    tree_click(page, "broken-gbk.txt")
    expect(page.locator(".np-tab.active")).to_contain_text("broken-gbk.txt")
    page.locator("#encPick").select_option("gbk")
    page.locator("#dlgActions button", has_text="Neu öffnen").click()
    expect(page.locator("#barLossy")).to_be_visible()
    check(page.locator("#saveBtn").is_disabled() and not page.locator("#barRo").is_visible(),
          "E5 invalid GBK bytes: view only, save disabled, not called write-protected")
    expect(page.locator("#status")).to_contain_text("ungültig")
    tree_click(page, "CMakeLists.txt")
    expect(page.locator(".np-tab.active")).to_contain_text("CMakeLists.txt")
    check(page.locator("#langPick").input_value() == "cmake", "E6 CMakeLists.txt gets CMake syntax")
    page.reload()
    expect(page.locator(".np-tab", has_text="zh-gbk.txt")).to_have_count(1)
    page.locator(".np-tab", has_text="zh-gbk.txt").click()
    expect(page.locator(".np-editor:not([hidden]) .cm-content")).to_contain_text("中文测试")
    check(page.locator("#encPick").input_value() == "gbk", "E7 a page reload reopens the tab in the chosen encoding")
    page.locator(".np-tab", has_text="notes.md").click()

    # language switch in the tile
    page.locator("#langSelect").select_option("fr")
    expect(page.locator("#newFileBtn")).to_have_text("Nouveau fichier")
    # the status bar follows at once, not only after the next cursor move (15.11.2026: "9 lines" stayed English)
    expect(page.locator("#barLines")).to_contain_text("lignes")
    check(not page.locator("#barPos").inner_text().startswith("Z "), "language switch re-renders the cursor position")
    page.locator("#langSelect").select_option("de")

    # light and dark
    for theme in ("light", "dark"):
        page.locator("#theme-select").select_option(theme)
        page.wait_for_timeout(400)  # styles.css fades button colours over 0.15 s
        bg = page.evaluate("getComputedStyle(document.querySelector('.np-editor:not([hidden]) .cm-editor')).backgroundColor")
        check(bg == ("rgb(255, 255, 255)" if theme == "light" else "rgb(22, 27, 34)"), f"theme {theme}: editor background {bg}")
        if shots:
            page.screenshot(path=str(shots / f"desktop-{theme}.png"))
    check(not errors, f"no JS errors: {errors[:3]}")
    # styles.css (lintux-modkit) uses the ZimaOS wallpaper, which only the ZimaOS web server has.
    unexpected = [x for x in bad if not (x.endswith("/notepad/api/write") and x[:3] in ("404", "409", "422"))
                  and x != f"404 {base.split('/modules')[0]}/images/wallpaper/zimaos01.jpg"]
    check(not unexpected, f"no failed requests besides the provoked 404/409/422: {unexpected}")
    ctx.close()

    # a session of a ZimaOS sub-account: the API answers 403 auth_forbidden (lintux-modkit auth)
    ctx = b.new_context(locale="de-DE")
    page = ctx.new_page()
    page.route("**/notepad/api/info", lambda route: route.fulfill(
        status=403, content_type="application/json",
        body='{"error":"only the ZimaOS administrator may use this module","code":"auth_forbidden"}'))
    page.goto(base)
    expect(page.locator("#status")).to_have_class("err")
    status = page.locator("#status").inner_text()
    check("Administrator" in status and "fehlgeschlagen" not in status, f"403 auth_forbidden shown as admin notice: {status!r}")
    ctx.close()

    # L deep link from the ZimaOS Files menu ("Open with Notepad"): ?open=<path> opens that file as the active tab
    ctx = b.new_context(viewport={"width": 1280, "height": 800}, locale="de-DE")
    page = ctx.new_page()
    link_errors = []
    page.on("pageerror", lambda e: link_errors.append(str(e)))
    target = data / "Documents" / "notes.md"
    page.goto(base + "index.html?open=" + quote(str(target), safe=""))
    expect(page.locator(".np-tab.active")).to_contain_text("notes.md")
    check(page.locator(".np-tab.active").get_attribute("title") == str(target), "L ?open= opens the linked file as the active tab")
    check("open=" not in page.url, f"L the parameter is removed from the address afterwards: {page.url}")
    page.reload()
    expect(page.locator(".np-tab", has_text="notes.md")).to_have_count(1)
    check(page.locator(".np-tab", has_text="notes.md").count() == 1, "L a reload restores the tab once, no duplicate")
    # a link outside the root is refused visibly, no tab
    page.goto(base + "index.html?open=" + quote("/etc/passwd", safe=""))
    expect(page.locator("#status")).to_have_class("err")
    check(page.locator(".np-tab", has_text="passwd").count() == 0, "L a link outside the root opens nothing and shows an error")
    # N "New text file" from the Files menu: ?new=<folder> asks for the name in that folder, creates and opens it
    folder = data / "Downloads"
    folder.mkdir(exist_ok=True)
    page.goto(base + "index.html?new=" + quote(str(folder), safe=""))
    expect(page.locator("#dlg")).to_be_visible()
    check(str(folder) in page.locator("#dlgTitle").inner_text(), f"N the dialog names the linked folder: {page.locator('#dlgTitle').inner_text()!r}")
    page.locator("#dlgInput").fill("aus-files.txt")
    page.keyboard.press("Enter")
    expect(page.locator(".np-tab.active")).to_contain_text("aus-files.txt")
    check((folder / "aus-files.txt").is_file(), "N the file is created in the linked folder")
    check("new=" not in page.url, f"N the parameter is removed from the address afterwards: {page.url}")
    page.reload()
    page.wait_for_timeout(800)
    check(not page.locator("#dlg").is_visible(), "N a reload does not ask again")
    # cancelling creates nothing
    page.goto(base + "index.html?new=" + quote(str(folder), safe=""))
    expect(page.locator("#dlg")).to_be_visible()
    page.locator("#dlgActions button", has_text="Abbrechen").click()
    check(sorted(x.name for x in folder.iterdir()) == ["aus-files.txt"], "N cancel creates nothing")
    # a folder outside the root: no dialog, a visible error
    page.goto(base + "index.html?new=" + quote("/etc", safe=""))
    expect(page.locator("#status")).to_have_class("err")
    check(not page.locator("#dlg").is_visible(), "N a folder outside the root opens no dialog and shows an error")
    check(not link_errors, f"L/N no JS errors: {link_errors[:3]}")
    ctx.close()

    # phone: tree behind the Files button
    ctx = b.new_context(viewport={"width": 390, "height": 844}, locale="en-US", is_mobile=True, has_touch=True)
    page = ctx.new_page()
    page.goto(base)
    expect(page.locator("#treeToggle")).to_be_visible()
    check(not page.locator("#tree").is_visible(), "phone: tree hidden until Files is tapped")
    page.locator("#treeToggle").tap()
    tree_click(page, "Documents")
    page.locator(".np-node", has_text="notes.md").tap()
    expect(page.locator(".np-tab.active")).to_contain_text("notes.md")
    check(page.locator("#langPick").input_value() == "markdown", "phone: opened notes.md as Markdown")
    width = page.evaluate("document.documentElement.scrollWidth")
    check(width <= 390, f"phone: no horizontal page scroll ({width}px)")
    if shots:
        page.screenshot(path=str(shots / "phone.png"))
    ctx.close()


if __name__ == "__main__":
    sys.exit(main())
