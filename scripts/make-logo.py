"""Builds internal/client/logo/outrologo.dds from internal/client/logo/outrologo.png.

The game draws this image at the end of the tutorial, in place of its own title
logo. The PNG must be 800x220 RGBA (white artwork on a transparent background).
The DDS is written in the same format as the game's original texture: BC3 (DXT5)
in a DX10 header with DXGI_FORMAT_BC3_UNORM_SRGB, one mip level, plus the small
engine header extension ("CRYF") the game's textures carry.

    python scripts/make-logo.py

Needs Pillow 11.2 or newer (BC3 encoding).
"""
import io
import os
import struct
import sys

from PIL import Image

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC = os.path.join(ROOT, "internal", "client", "logo", "outrologo.png")
DST = os.path.join(ROOT, "internal", "client", "logo", "outrologo.dds")
WIDTH, HEIGHT = 800, 220
DXGI_FORMAT_BC3_UNORM_SRGB = 78


def main():
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
