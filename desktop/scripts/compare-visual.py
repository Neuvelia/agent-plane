"""Compare unmasked P0 screenshots to the immutable PDF raster.

Requires Pillow. Reports reference differences before a local variant is approved.
"""
import json
import sys
from pathlib import Path
from PIL import Image, ImageChops

reference = Path(sys.argv[1])
actual = Path(sys.argv[2])
output = Path(sys.argv[3])
output.mkdir(parents=True, exist_ok=True)
source = {"dashboard": 2, "project": 3, "runner": 9, "settings": 10}
report = {}
for page, number in source.items():
    before = Image.open(reference / f"page-{number:02d}.png").convert("RGB")
    after = Image.open(actual / f"{page}.png").convert("RGB")
    if before.size != (1080, 720) or after.size != before.size:
        raise SystemExit(f"{page}: wrong size: {before.size} / {after.size}")
    a, b = before.load(), after.load()
    hot = Image.new("RGB", before.size)
    pixels = hot.load()
    changed = 0
    for y in range(720):
        for x in range(1080):
            delta = max(abs(a[x, y][channel] - b[x, y][channel]) for channel in range(3))
            if delta > 16:
                changed += 1
                pixels[x, y] = (255, 89, 65)
            else:
                pixels[x, y] = (0, 0, 0)
    side = Image.new("RGB", (2160, 720))
    side.paste(before, (0, 0))
    side.paste(after, (1080, 0))
    side.save(output / f"{page}-side-by-side.png")
    hot.save(output / f"{page}-diff.png")
    report[page] = {"changed_pixels": changed, "total_pixels": 1080 * 720,
                    "changed_percent": round(100 * changed / (1080 * 720), 3)}
(output / "comparison.json").write_text(json.dumps(report, indent=2) + "\n")
print(json.dumps(report, indent=2))
