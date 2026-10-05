#!/usr/bin/env python3
# Regenerates the PNG icons in web/ from the mark (same geometry as
# web/icon.svg). Run from rfog/: python3 tools/icons.py web /tmp/preview.png
# Needs Pillow. The SVG is the source of truth; keep the two in step.
# Renders the RFoG mark (same geometry as web/icon.svg) to PNGs.
import math, sys
from PIL import Image, ImageDraw, ImageFilter
out = sys.argv[1]
S = 1024  # draw big, scale down
k = S / 64
def P(x, y): return (x * k, y * k)
def mark(glow=True):
    im = Image.new('RGBA', (S, S), (0, 0, 0, 0))
    d = ImageDraw.Draw(im)
    d.rounded_rectangle([0, 0, S - 1, S - 1], radius=14 * k, fill=(11, 12, 14, 255), outline=(30, 33, 37, 255), width=int(2 * k))
    cyan = (79, 211, 232)
    layer = Image.new('RGBA', (S, S), (0, 0, 0, 0))
    g = ImageDraw.Draw(layer)
    def line(x1, y1, x2, y2, w, a):
        g.line([P(x1, y1), P(x2, y2)], fill=cyan + (a,), width=int(w * k))
        r = w * k / 2
        for (x, y) in ((x1, y1), (x2, y2)):
            cx, cy = P(x, y); g.ellipse([cx - r, cy - r, cx + r, cy + r], fill=cyan + (a,))
    def arc(r, w, a):
        cx, cy = P(32, 38)
        R = r * k
        g.arc([cx - R, cy - R, cx + R, cy + R], start=-152, end=-28, fill=cyan + (a,), width=int(w * k))
        for ang in (-152, -28):
            x = cx + (R - w * k / 2) * math.cos(math.radians(ang)); y = cy + (R - w * k / 2) * math.sin(math.radians(ang))
            rr = w * k / 2; g.ellipse([x - rr, y - rr, x + rr, y + rr], fill=cyan + (a,))
    def arc2(r, w, a, up):
        cx, cy = P(32, 34 if up else 38)
        R = r * k
        st, en = (-180, 0) if up else (0, 180)
        g.arc([cx - R, cy - R, cx + R, cy + R], start=st, end=en, fill=cyan + (a,), width=int(w * k))
        for ang in (st, en):
            x = cx + (R - w * k / 2) * math.cos(math.radians(ang)); y = cy + (R - w * k / 2) * math.sin(math.radians(ang))
            rr = w * k / 2; g.ellipse([x - rr, y - rr, x + rr, y + rr], fill=cyan + (a,))
    arc2(10, 3.5, 255, True)
    arc2(19, 3.5, 140, True)
    arc2(10, 3, 56, False)
    arc2(19, 3, 31, False)
    line(7, 36, 57, 36, 2.5, 128)
    cx, cy = P(32, 36)
    r = 5 * k; g.ellipse([cx - r, cy - r, cx + r, cy + r], fill=cyan + (255,))
    r = 2 * k; g.ellipse([cx - r, cy - r, cx + r, cy + r], fill=(232, 251, 255, 255))
    if glow:
        halo = layer.filter(ImageFilter.GaussianBlur(radius=2.2 * k))
        im = Image.alpha_composite(im, halo)
    im = Image.alpha_composite(im, layer)
    return im
big = mark()
for size, name in [(512, 'icon-512.png'), (192, 'icon-192.png'), (180, 'apple-touch-icon.png'), (32, 'favicon-32.png')]:
    big.resize((size, size), Image.LANCZOS).save(out + '/' + name, optimize=True)
# a preview sheet: the sizes on dark and light, for a look
sheet = Image.new('RGB', (760, 300), (240, 240, 240))
sheet.paste((11, 12, 14), [380, 0, 760, 300])
for i, s in enumerate([128, 64, 32, 16]):
    ic = big.resize((s, s), Image.LANCZOS)
    x = 20 + sum([128, 64, 32, 16][:i]) + i * 20
    sheet.paste(ic, (x, 80), ic); sheet.paste(ic, (380 + x, 80), ic)
sheet.save(sys.argv[2])
