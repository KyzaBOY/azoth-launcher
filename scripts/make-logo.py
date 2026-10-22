"""Builds the logo textures in internal/client/logo from their PNGs.

    outrologo.png  800x220   end of the tutorial (white artwork, transparent)
    titlelogo.png  1216x652  title screen, "press any button" (artwork + soft glow)

Each PNG must be RGBA at exactly that size. The DDS is written like the game's
own UI textures: a DX10 header with DXGI_FORMAT_BC3_UNORM_SRGB (BC3 = DXT5), one
mip level, plus the small engine header extension ("CRYF") they carry. (The
game's title logo is BC7; BC3 has the same size and the header says which one
it is.)

    python scripts/make-logo.py

Needs Pillow 11.2 or newer (BC3 encoding).
"""
import io
import os
import struct
import sys

from PIL import Image

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LOGO_DIR = os.path.join(ROOT, "internal", "client", "logo")
LOGOS = [("outrologo", 800, 220), ("titlelogo", 1216, 652)]
DXGI_FORMAT_BC3_UNORM_SRGB = 78


def main():
    for name, width, height in LOGOS:
        build(os.path.join(LOGO_DIR, name + ".png"), os.path.join(LOGO_DIR, name + ".dds"), width, height)


def build(SRC, DST, WIDTH, HEIGHT):
    im = Image.open(SRC).convert("RGBA")
    if im.size != (WIDTH, HEIGHT):
        sys.exit(f"{SRC} is {im.size[0]}x{im.size[1]}, expected {WIDTH}x{HEIGHT}")
    buf = io.BytesIO()
    im.save(buf, format="DDS", pixel_format="DXT5")
    blocks = buf.getvalue()[128:]  # Pillow writes a plain 128-byte DXT5 header
    if len(blocks) != WIDTH * HEIGHT:  # BC3 = 1 byte per pixel
        sys.exit(f"unexpected BC3 size {len(blocks)}")

    reserved1 = [0, 0x800, 0, 0, 0, 0, 0] + [0x3F800000] * 4  # engine extension: 4 x 1.0f
    header = b"DDS " + struct.pack(
        "<7I11I",
        124,                    # header size
        0x1007,                 # CAPS | HEIGHT | WIDTH | PIXELFORMAT
        HEIGHT, WIDTH, 0, 0, 0,  # height, width, pitch, depth, mip count
        *reserved1,
    )
    header += struct.pack("<2I4s5I", 32, 0x4, b"DX10", 0, 0, 0, 0, 0)  # pixel format: FOURCC DX10
    header += struct.pack("<4I4s", 0x1000, 0, 1, 12, b"FYRC")           # caps (TEXTURE) + engine marker
    header += struct.pack("<5I", DXGI_FORMAT_BC3_UNORM_SRGB, 3, 0, 1, 0)  # DX10: format, 2D, array size 1
    assert len(header) == 148
    with open(DST, "wb") as f:
        f.write(header + blocks)
    print(f"{DST}: {len(header) + len(blocks)} bytes")


if __name__ == "__main__":
    main()
