#!/usr/bin/env python3
"""Render the app icons for the web manifest from the brand mark.

There is no SVG rasteriser on the build host and adding one as a dependency for three
files that change roughly never is not worth it — so this draws the mark directly:
the bolt silhouette from public/favicon.svg, white, on the brand-purple rounded square,
with enough padding to survive a maskable crop.

Run when the mark changes:  python3 web/scripts/make-icons.py
Outputs: public/icon-192.png, public/icon-512.png, public/apple-touch-icon.png
"""

import math
import re
import struct
import zlib
from pathlib import Path

# The first path of favicon.svg — the silhouette. Kept here verbatim rather than parsed
# out of the SVG: the file also contains fifteen blurred ellipses behind a mask, none of
# which belong in an app icon, and picking "the first path" out of it programmatically
# would be a fragile way to say something this stable.
PATH = ("M25.946 44.938c-.664.845-2.021.375-2.021-.698V33.937a2.26 2.26 0 0 0-2.262-2.262"
        "H10.287c-.92 0-1.456-1.04-.92-1.788l7.48-10.471c1.07-1.497 0-3.578-1.842-3.578"
        "H1.237c-.92 0-1.456-1.04-.92-1.788L10.013.474c.214-.297.556-.474.92-.474h28.894"
        "c.92 0 1.456 1.04.92 1.788l-7.48 10.471c-1.07 1.498 0 3.579 1.842 3.579h11.377"
        "c.943 0 1.473 1.088.89 1.83L25.947 44.94z")
VIEWBOX = (48.0, 46.0)
BRAND = (0x86, 0x3B, 0xFF)

TOKEN = re.compile(r"[MmLlHhVvCcSsZzAa]|-?\d*\.?\d+(?:e[-+]?\d+)?", re.I)


def arc_to_cubics(x0, y0, rx, ry, phi, large, sweep, x, y):
    """SVG endpoint arc -> cubic segments (F.6.5 of the SVG spec)."""
    if rx == 0 or ry == 0 or (x0 == x and y0 == y):
        return [(x0, y0, x, y, x, y)]
    rx, ry = abs(rx), abs(ry)
    p = math.radians(phi)
    cs, sn = math.cos(p), math.sin(p)
    dx2, dy2 = (x0 - x) / 2.0, (y0 - y) / 2.0
    x1 = cs * dx2 + sn * dy2
    y1 = -sn * dx2 + cs * dy2
    lam = x1 * x1 / (rx * rx) + y1 * y1 / (ry * ry)
    if lam > 1:
        s = math.sqrt(lam)
        rx, ry = rx * s, ry * s
    num = rx * rx * ry * ry - rx * rx * y1 * y1 - ry * ry * x1 * x1
    den = rx * rx * y1 * y1 + ry * ry * x1 * x1
    co = math.sqrt(max(0.0, num / den)) if den else 0.0
    if large == sweep:
        co = -co
    cx1, cy1 = co * rx * y1 / ry, -co * ry * x1 / rx
    cx = cs * cx1 - sn * cy1 + (x0 + x) / 2.0
    cy = sn * cx1 + cs * cy1 + (y0 + y) / 2.0

    def ang(ux, uy, vx, vy):
        d = (math.hypot(ux, uy) * math.hypot(vx, vy))
        if d == 0:
            return 0.0
        c = max(-1.0, min(1.0, (ux * vx + uy * vy) / d))
        a = math.acos(c)
        return -a if ux * vy - uy * vx < 0 else a

    th1 = ang(1, 0, (x1 - cx1) / rx, (y1 - cy1) / ry)
    dth = ang((x1 - cx1) / rx, (y1 - cy1) / ry, (-x1 - cx1) / rx, (-y1 - cy1) / ry)
    if not sweep and dth > 0:
        dth -= 2 * math.pi
    elif sweep and dth < 0:
        dth += 2 * math.pi

    n = max(1, int(math.ceil(abs(dth) / (math.pi / 2))))
    out, step = [], dth / n
    t = 4.0 / 3.0 * math.tan(step / 4.0)
    px, py = x0, y0
    for i in range(n):
        a0, a1 = th1 + i * step, th1 + (i + 1) * step

        def pt(a):
            return (cs * rx * math.cos(a) - sn * ry * math.sin(a) + cx,
                    sn * rx * math.cos(a) + cs * ry * math.sin(a) + cy)

        def dv(a):
            return (-cs * rx * math.sin(a) - sn * ry * math.cos(a),
                    -sn * rx * math.sin(a) + cs * ry * math.cos(a))

        ex, ey = pt(a1)
        d0x, d0y = dv(a0)
        d1x, d1y = dv(a1)
        out.append((px + t * d0x, py + t * d0y, ex - t * d1x, ey - t * d1y, ex, ey))
        px, py = ex, ey
    return out


def flatten(path, steps=24):
    """Path data -> list of closed polygons in user units."""
    toks = TOKEN.findall(path)
    i, cmd = 0, None
    cx = cy = sx = sy = 0.0
    polys, cur = [], []

    def num():
        nonlocal i
        v = float(toks[i])
        i += 1
        return v

    def bez(x1, y1, x2, y2, x, y):
        nonlocal cx, cy
        for s in range(1, steps + 1):
            t = s / steps
            u = 1 - t
            cur.append((u**3 * cx + 3 * u * u * t * x1 + 3 * u * t * t * x2 + t**3 * x,
                        u**3 * cy + 3 * u * u * t * y1 + 3 * u * t * t * y2 + t**3 * y))
        cx, cy = x, y

    while i < len(toks):
        if toks[i] in "MmLlHhVvCcSsZzAa":
            cmd = toks[i]
            i += 1
        rel = cmd.islower()
        c = cmd.upper()
        if c == "M":
            x, y = num(), num()
            if rel:
                x, y = cx + x, cy + y
            if cur:
                polys.append(cur)
            cur = [(x, y)]
            cx, cy = sx, sy = x, y
            cmd = "l" if rel else "L"
        elif c == "L":
            x, y = num(), num()
            if rel:
                x, y = cx + x, cy + y
            cur.append((x, y))
            cx, cy = x, y
        elif c == "H":
            x = num()
            if rel:
                x += cx
            cur.append((x, cy))
            cx = x
        elif c == "V":
            y = num()
            if rel:
                y += cy
            cur.append((cx, y))
            cy = y
        elif c == "C":
            x1, y1, x2, y2, x, y = (num() for _ in range(6))
            if rel:
                x1, y1, x2, y2, x, y = cx + x1, cy + y1, cx + x2, cy + y2, cx + x, cy + y
            bez(x1, y1, x2, y2, x, y)
        elif c == "A":
            rx, ry, rot, la, sw, x, y = (num() for _ in range(7))
            if rel:
                x, y = cx + x, cy + y
            for seg in arc_to_cubics(cx, cy, rx, ry, rot, int(la), int(sw), x, y):
                bez(*seg)
        elif c == "Z":
            if cur:
                cur.append((sx, sy))
                polys.append(cur)
                cur = []
            cx, cy = sx, sy
    if cur:
        polys.append(cur)
    return polys


def coverage(polys, size, scale, ox, oy, ss=4):
    """Supersampled nonzero-winding fill -> per-pixel coverage 0..1."""
    n = size * ss
    cov = [0.0] * (size * size)
    edges = []
    for poly in polys:
        for (x0, y0), (x1, y1) in zip(poly, poly[1:]):
            ax, ay = x0 * scale * ss + ox * ss, y0 * scale * ss + oy * ss
            bx, by = x1 * scale * ss + ox * ss, y1 * scale * ss + oy * ss
            if ay != by:
                edges.append((ax, ay, bx, by))
    for py in range(n):
        yc = py + 0.5
        xs = []
        for ax, ay, bx, by in edges:
            if (ay <= yc < by) or (by <= yc < ay):
                t = (yc - ay) / (by - ay)
                xs.append((ax + t * (bx - ax), 1 if by > ay else -1))
        if not xs:
            continue
        xs.sort()
        wind = 0
        for k in range(len(xs) - 1):
            wind += xs[k][1]
            if wind == 0:
                continue
            x_start, x_end = xs[k][0], xs[k + 1][0]
            for px in range(max(0, int(x_start)), min(n, int(x_end) + 1)):
                l = max(x_start, px)
                r = min(x_end, px + 1)
                if r > l:
                    cov[(py // ss) * size + (px // ss)] += (r - l) / (ss * ss)
    return [min(1.0, c) for c in cov]


def write_png(path, size, pixels):
    raw = b"".join(b"\x00" + bytes(pixels[y * size * 4:(y + 1) * size * 4]) for y in range(size))
    def chunk(tag, data):
        c = tag + data
        return struct.pack(">I", len(data)) + c + struct.pack(">I", zlib.crc32(c) & 0xFFFFFFFF)
    png = (b"\x89PNG\r\n\x1a\n"
           + chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0))
           + chunk(b"IDAT", zlib.compress(raw, 9))
           + chunk(b"IEND", b""))
    Path(path).write_bytes(png)


def render(size, out, pad_ratio=0.30, radius_ratio=0.22):
    inner = size * (1 - 2 * pad_ratio)
    scale = min(inner / VIEWBOX[0], inner / VIEWBOX[1])
    ox = (size - VIEWBOX[0] * scale) / 2
    oy = (size - VIEWBOX[1] * scale) / 2
    glyph = coverage(flatten(PATH), size, scale, ox, oy)

    r = size * radius_ratio
    px = bytearray(size * size * 4)
    for y in range(size):
        for x in range(size):
            # rounded-square mask
            dx = max(r - x - 0.5, x + 0.5 - (size - r), 0.0)
            dy = max(r - y - 0.5, y + 0.5 - (size - r), 0.0)
            d = math.hypot(dx, dy)
            bg = 1.0 if d <= r - 0.5 else (0.0 if d >= r + 0.5 else (r + 0.5 - d))
            g = glyph[y * size + x]
            i = (y * size + x) * 4
            # white glyph over brand background, both premultiplied by the corner mask
            px[i + 0] = int((BRAND[0] * (1 - g) + 255 * g) * bg)
            px[i + 1] = int((BRAND[1] * (1 - g) + 255 * g) * bg)
            px[i + 2] = int((BRAND[2] * (1 - g) + 255 * g) * bg)
            px[i + 3] = int(255 * bg)
    write_png(out, size, px)
    print(f"  {out} ({size}x{size})")


if __name__ == "__main__":
    pub = Path(__file__).resolve().parent.parent / "public"
    render(192, pub / "icon-192.png")
    render(512, pub / "icon-512.png")
    # iOS crops nothing and shows no transparency, so it gets square corners.
    render(180, pub / "apple-touch-icon.png", radius_ratio=0.0)
