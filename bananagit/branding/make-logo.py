#!/usr/bin/env python3
"""Regenerate BananaGit's logo and favicon files from the suite's pixel-art banana.

    python3 bananagit/branding/make-logo.py

Reads bananagit/branding/banana.png (BananaWiki's 32x32 favicon) and writes the
files Gitea serves and the two sources `make generate-images` starts from.
The SVG holds one rectangle per run of equal pixels, so it stays sharp at any size.
Needs Pillow.
"""
from pathlib import Path

from PIL import Image

ROOT = Path(__file__).resolve().parents[2]
source = Image.open(Path(__file__).with_name("banana.png")).convert("RGBA")
width, height = source.size
pixels = source.load()

rects = []
for y in range(height):
    x = 0
    while x < width:
        r, g, b, a = pixels[x, y]
        start = x
        while x < width and pixels[x, y] == (r, g, b, a):
            x += 1
        if a:
            opacity = "" if a == 255 else f' fill-opacity="{a / 255:.2f}"'
            rects.append(f'<rect x="{start}" y="{y}" width="{x - start}" height="1" fill="#{r:02x}{g:02x}{b:02x}"{opacity}/>')
svg = (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {width} {height}" shape-rendering="crispEdges">'
       + "".join(rects) + "</svg>\n")

for name in ("assets/logo.svg", "assets/favicon.svg", "public/assets/img/logo.svg", "public/assets/img/favicon.svg"):
    (ROOT / name).write_text(svg)


def png(name: str, size: int, background: bool = False) -> None:
    image = source.resize((size, size), Image.NEAREST)
    if background:
        flat = Image.new("RGBA", image.size, "white")
        flat.alpha_composite(image)
        image = flat.convert("RGB")
    image.save(ROOT / "public/assets/img" / name, optimize=True)


png("logo.png", 512)
png("favicon.png", 180)
png("avatar_default.png", 200)
png("apple-touch-icon.png", 180, background=True)
print(f"{len(rects)} rectangles; logo and favicon files written")
