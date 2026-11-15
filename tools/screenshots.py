#!/usr/bin/env python3
"""Screenshots for the README (Playwright, real Chromium, English UI).

Starts its own notepad-dev on a temporary /DATA with made-up demo files (no real data), opens a few
of them in tabs and writes docs/screenshots/{dark,light,phone}.png. The version label shows VERSION.

  ~/.venvs/playwright/bin/python3 tools/screenshots.py

Needs build/notepad-dev and the editor bundle (sh check.sh builds both) and ../lintux-modkit.
"""
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

HERE = Path(__file__).resolve().parent.parent
OUT = HERE / "docs" / "screenshots"
MODKIT = Path(os.environ.get("LINTUX_MODKIT", HERE.parent / "lintux-modkit"))

DEMO = {
    "AppData/nextcloud/docker-compose.yml": """services:
  app:
    image: nextcloud:31-apache
    restart: unless-stopped
    ports:
      - "8080:80"
    environment:
      - MYSQL_HOST=db
      - MYSQL_DATABASE=nextcloud
      - TZ=Europe/Berlin
    volumes:
      - /DATA/AppData/nextcloud/html:/var/www/html
    depends_on:
      - db

  db:
    image: mariadb:11.4
    restart: unless-stopped
    command: --transaction-isolation=READ-COMMITTED
    volumes:
      - /DATA/AppData/nextcloud/db:/var/lib/mysql
""",
    "AppData/nginx/site.conf": """server {
    listen 80;
    server_name files.example.invalid;

    # forward everything to the app container
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        client_max_body_size 512m;
    }
}
""",
    "Documents/backup.sh": """#!/bin/sh
# Nightly copy of the photo library to the second disk.
set -eu
SRC=/DATA/Gallery
DST=/media/Backup/Gallery
mkdir -p "$DST"
rsync -a --delete "$SRC/" "$DST/"
echo "backup done: $(date -Iseconds)"
""",
    "Documents/notes.md": "# Notes\n\n- renew the certificate in March\n- *check* the UPS battery\n",
    "Media/README.txt": "Movies and music.\n",
}


def free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    p = s.getsockname()[1]
    s.close()
    return p


def main():
    version = (HERE / "VERSION").read_text().strip()
    tmp = Path(tempfile.mkdtemp(prefix="notepad-shots-"))
    data, web = tmp / "DATA", tmp / "web"
    for rel, text in DEMO.items():
        f = data / rel
        f.parent.mkdir(parents=True, exist_ok=True)
        f.write_text(text)
    shutil.copytree(HERE / "files/usr/share/casaos/www/modules/notepad", web)
    for name in ("common.js", "styles.css"):
        shutil.copy(MODKIT / "web" / name, web / name)
    index = web / "index.html"
    index.write_text(index.read_text().replace("@VERSION@", version))
    port = free_port()
    srv = subprocess.Popen([str(HERE / "build" / "notepad-dev"), "-root", str(data), "-web", str(web),
                            "-addr", f"127.0.0.1:{port}"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    base = f"http://127.0.0.1:{port}/modules/notepad/"
    OUT.mkdir(parents=True, exist_ok=True)
    try:
        for _ in range(50):
            try:
                socket.create_connection(("127.0.0.1", port), timeout=0.2).close()
                break
            except OSError:
                time.sleep(0.1)
        with sync_playwright() as p:
            b = p.chromium.launch()
            for theme in ("dark", "light"):
                ctx = b.new_context(viewport={"width": 1280, "height": 760}, locale="en-US")
                ctx.add_init_script("try { localStorage.setItem('lang', 'en_US'); } catch (e) {}")
                page = ctx.new_page()
                page.goto(base)
                expect(page.locator("#newFileBtn")).to_have_text("New file")
                page.locator("#theme-select").select_option(theme)
                for folder, files in (("AppData", None), ("nginx", ["site.conf"]), ("nextcloud", ["docker-compose.yml"]),
                                      ("Documents", ["backup.sh"])):
                    page.locator(".np-node", has_text=folder).first.click()
                    for name in files or []:
                        page.locator(".np-node", has_text=name).first.click()
                        expect(page.locator(".np-tab.active")).to_contain_text(name)
                active = "docker-compose.yml" if theme == "dark" else "backup.sh"
                page.locator(".np-tab", has_text=active).click()
                expect(page.locator(".np-tab.active")).to_contain_text(active)
                page.wait_for_timeout(500)  # styles.css fades colours over 0.15 s
                page.screenshot(path=str(OUT / f"{theme}.png"))
                ctx.close()
            ctx = b.new_context(viewport={"width": 390, "height": 760}, locale="en-US", is_mobile=True, has_touch=True)
            ctx.add_init_script("try { localStorage.setItem('lang', 'en_US'); } catch (e) {}")
            page = ctx.new_page()
            page.goto(base)
            page.locator("#treeToggle").tap()
            page.locator(".np-node", has_text="Documents").first.tap()
            page.locator(".np-node", has_text="backup.sh").first.tap()
            expect(page.locator(".np-tab.active")).to_contain_text("backup.sh")
            page.wait_for_timeout(500)
            page.screenshot(path=str(OUT / "phone.png"))
            ctx.close()
            b.close()
    finally:
        srv.terminate()
        srv.wait(timeout=5)
        shutil.rmtree(tmp, ignore_errors=True)
    for f in sorted(OUT.glob("*.png")):
        print(f"{f.relative_to(HERE)} {f.stat().st_size} bytes")
    return 0


if __name__ == "__main__":
    sys.exit(main())
