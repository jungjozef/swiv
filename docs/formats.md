# SWIV file formats

Reverse-engineered from the Amiga disk image. The disk archive itself is
described in `tools/README.md` (`internal/adf`).

## `.RAW` – full-screen image (`internal/raw`)

40,992 bytes: 4 bitplanes of 320x256 (10,240 bytes each, plane 0 first),
then 16 big-endian `0x0RGB` palette words.

## `.LIN` – sprite / scenery bank (`internal/lin`)

Big endian.

```
u16  count            number of IMAGES in the bank
frame* until EOF      a frame whose flags bit 0 is set continues into the next
                      frame: an image is a chain of frames
```

Frame:

```
u16  size             bytes of pixel data = h * ceil(w/16) * 4 planes * 2
u8   w, h             pixels
i8   hotX, hotY       hotspot (SIGNED; e.g. -6, or 63 for a 128-wide image)
u8   key              colour index that is transparent
u8   flags            bit 0: continued by the next frame
                      bit 3 (0x08): the image is a hardware sprite
                                    (bullets); drawn through the sprite path
                      bit 4 (0x10): marker frame, the game never draws it
                      bits 1-2: blit-mode hooks, not yet understood
i8   cx, cy           secondary offset, meaning unknown
...  pixels           h rows; per row 4 planes, each ceil(w/16) words
                      (plane 0 first); MSB = leftmost pixel
```

Pixel colour index = bit0 from plane 0 ... bit3 from plane 3. Palettes are
not in the file: they come from the level's `.PAM` colour commands.

**Images.** Large pictures are stored as 32-pixel-wide strips. Strip *j* of an
image has hotX = hotX_0 - 32*j, so drawing each strip at `(x - hotX, y - hotY)`
puts the strips side by side (`_HOUSES` images 1..4 give a 128x64 ground tile
with hotX 63, 31, -1, -33). The game's "frame index" in a dictionary word
(`A/2`) counts **images**, not file frames: `_RUNWAY.LIN` has 13 frames but 7
images (`{0,1,2} {3} {4} {5} {6,7} {8,9} {10,11,12}`), exactly the 7 entries the
running game keeps for the bank, and the header count equals the number of
images in all 90 files.

(An earlier reading of this format called the extra frames a "secondary
group"; that was wrong.)

## `.PAM` – level script (`tools/pam.py`)

A stream of big-endian 32-bit records. There is no terminator on disk (the
game's loader appends a zero long after reading); a 0 long or end of file ends
the script. The game walks it
against the level scroll counter, which starts at `$E9C0` and counts down; each
record carries the distance (rows) since the previous record.

```
bit31 = 1   palette command
    bits 16..23   scroll delta
    bits  4..15   RGB444 colour
    bits  0..3    colour index
bit31 = 0   object
    bits  0..3    kind (0 = main, nonzero = other, not yet understood)
    bits  4..11   type: index into the level dictionary (see below)
    bits 12..19   scroll delta
    bits 20..30   position: an 11-bit SIGNED value (the game sign-extends
                  bit 30; bit 31 is the command flag). If it is <= -92 add
                  $800, then subtract $200 while it is >= $1A0 (at most 4
                  times). The result is x (so x may be slightly negative) and
                  the layer is z = 4 - (number of subtractions).
```

The first 16 commands of each file set the level's 16-colour playfield
palette (all with delta 0); later ones are mid-level colour shifts. Verified
against the live game: the commands the running program executes for
`TOWN.PAM` match `tools/pam.py` output exactly.

Position decode (including the 11-bit sign extension: `0x7FF` is x = -1 on layer 4,
not layer 0) verified against the running game (`$126AA`: object
fields `$8 = z<<8 | ordinal`, `$A = dictionary word`, `$C = x`, `$E = y`).

Object formations are visible in the data: e.g. seven consecutive type-3
objects whose position steps by `0x21`.

### Level table and dictionary (inside `AMPROG.OBJ`)

`$1280C`, 6 bytes per level: `u16 file-name index, u16 dictionary offset
(from $1280C), u16 parameter`. Seven levels: town, desert, grass, river, ice,
scifi, final. The file-name index addresses the NUL-terminated name table at
`$EFC4` (126 entries; `town.pam` = 90).

Dictionary (one per level, u16 entries): `u8 frame*2, u8 bank` where `bank`
is again a file-name index (a `.LIN` sprite/scenery bank). An object's type
selects one entry: that is its bank + image number (`A/2`; an odd `A` selects
the bank's second table). Town's dictionary has 78 entries
and its PAM never uses a type above `0x4D`, which cross-checks the layout.
