# AMPROG.OBJ notes

Work in progress. Generated listing: `python tools/disasm.py assets/AMPROG.OBJ 0xefc0 reverse/amprog.s`
(`reverse/` is git-ignored: it is derived from the original code.)

## Image

* 55,668 bytes, flat binary (no hunks), loaded at `$EFC0`, entry `bra.w $FC32`.
* `$EFC3-$F46x`: NUL-terminated file name table (`jeepheli.lin`, ... `smart.snd`),
  in the order the game refers to files. Pointer to it stored at `-$40e(a6)`.
* `$FC00-$1BBC0`: code, with text blobs and tables interleaved. Roughly 43% is
  reachable by static flow; the rest is entered via task start addresses and
  tables (the disassembler finds ~7,300 instructions with its speculative pass).
* `a6` is a global base register (= kernel base `$17DC` at run time). Program
  variables are `+d16(a6)` (e.g. `$e1e`, `$2ba0`, `$3041`), kernel variables
  are negative offsets.

## Kernel services (jump table below a6, from the Sales Curve "Kernel V1.0")

Names are inferred from the routines the vectors branch to.

| a6 offset | kernel code | role |
|---|---|---|
| `-$5fa` | `$796` | find/open file by name (a0 = name) |
| `-$5f6` | `$7b6` | close file |
| `-$5f2` | `$b14` | read: a0 = dest, d0 = count (LZ-unpacks) |
| `-$5e6` / `-$5e2` | `$6e4` / `$75a` | open by index / seek |
| `-$5de` `-$5da` `-$5d6` | `$dfe` `$de0` `$e0e` | allocate (any / fast / chip), d0 = size, a0 = result |
| `-$5d2` `-$5ca` | `$e48` `$f02` | free |
| `-$5c6` `-$5c2` `-$5be` | `$100e` `$1030` `$1052` | add task, d0 = priority, a0 = entry |
| `-$59e` | `$f76` | yield (cooperative switch) |

The kernel is a small multitasking executive: tasks yield explicitly, and a
50 Hz vblank handler (`$266`) counts frames at `-$44(a6)` and polls joystick/keys.

## Structure of the game

* `$FC32` boot: frees startup memory, sets the file table, runs init
  (`$14710 $13862 $14a88 $12cae $15de4`), starts tasks, waits for `$2d10(a6)`.
* Main loop `l_fd24` calls ~14 per-frame update routines (`$114e0 $11356 $1138c
  $10718 $11ec6 $122a8 $10558 $122b6 $10f1e $111b6 $10cc8 $ff02 $fd62 $11fa8`).
* `sub_14eca` is "end of frame / wait for vblank" (yields until `$a5(a6)` clear).
  `sub_14ee2` waits d0 frames. `sub_14e9a`/`$14ebc` take an inline word argument
  after the `bsr` (the return address is advanced past it).

## Display

* 5 bitplanes (32 colours), 44-byte pitch (352 px scroll buffer) x 256 rows.
  Three playfield descriptors at `$100 $104 $108(a6)` (triple buffering), each
  with a `$dc00`-byte bitmap and a `$1120`-byte side buffer.
* Copper list is built by `$138a2 / $138d0 / $1391e`; BPLCON0 base `$5200`.
* Full-screen `.RAW` load (`$14e60`): reads 40 bytes/row x 256 rows per plane
  straight into the 44-byte-pitch bitmap; plane stride `$3700` in that buffer.
* Hardware sprites 17-19 / 21-23 / 25-27 / 29-31 colours are written from the
  table at `$11b22` by `$11abc` (colour selected by `-$42(a6)`).
* Blitter code at `$13040-$131d8` (BLTCON0/BLTSIZE).

## Text

Strings use inline markup: `_xNNN _yNNN` position, `_cNN` colour, `_aN`
alignment, `_n` newline, `_m` (paragraph?). Screens: Game Over, Game Completed,
statistics, credits (`$111f0`), high scores (`$1232c`), vehicle specs
(`$105b6-$108c6`: SWIV-1 helicopter / SWIV-2 jeep), debug "CHIPMEM/FASTMEM".

## Level start (traced by running the game, `tools/trace_boot.py`)

Observed file order, with the frame (50 Hz) it happens at:

| frame | event |
|---|---|
| 17-22 | `AMDLS0.CAT`, `AMPROG.OBJ` |
| 235-282 | `bigexpl.snd`, `smart.snd` |
| 311-447 | common sprite banks: `jeepheli`, `expl1`, `bullet`, `homing`, `plop` |
| 454 | `cover.raw` (title), then `hs7.txt`, `hs16.txt` (high-score tables) |
| 660 | `town.pam` (level 1 script) |
| 715-977 | level banks: `_runway`, `_houses`, `_deadtre`, `_stop`, `foddera`, `flame`, `_wall`, `medtank` |
| 1200+ | more banks stream in *during* the level: `yellow`, `_railway`, `train`, `proxmine`, `mine`, `token`, `goose`, `_ditch` |

Code path: `$12520` level task loop -> `sub_127c0` (level table at `$1280C`,
loads the `.PAM` whole into a buffer at `$E04(a6)`; level number at `$B8(a6)`,
dictionary pointer at `$BA(a6)`) -> `sub_12556` walks the 32-bit records ->
`sub_12612` handles one record (colour command -> `$139C4`; object -> alloc a
`$16`-byte object, type -> dictionary word at `$A(obj)`, scroll delta applied
to `$E02(a6)`). `.LIN` banks are read frame by frame (10-byte header then pixel
data) via `k_Read`.

## How a frame is built (verified by rendering emulator frames)

* Display: 4 bitplanes (`BPLCON0=$4200`; the copper briefly switches to 5 for
  the status area), 320x256 window (`DIWSTRT $2C81`, `DIWSTOP $2CC1`, DDF
  `$38-$D0`), modulo 4 (44-byte pitch).
* Each playfield bitmap is a **vertical ring buffer of 320 rows**; plane stride
  is `$3700` (44 x 320). Scrolling is done by moving the copper's BPLxPT values.
* Three buffers (`$100/$104/$108(a6)`): `$108` is the clean **background**
  (ground texture, runway, buildings, trees). Every frame the game copies the
  needed blocks from the background into the display buffers (blit `D=A`,
  BLTCON0 `$09F0`) and draws sprites/bullets/enemies on top with cookie-cut
  blits (`$0FCA`, `D = A?B:C`); `$0B0A` is mask-erase.
* The per-level `.PAM` supplies the palette and **all** the scenery objects,
  including the ground texture: in town the grain is `_houses.lin` images
  1..4 (a 128x64 tile, type `04` in the dictionary, three per row at
  x = -1, 127, 255), in desert `_sand.lin` images 3..6 (128x128).
* Objects are kept in a list ordered by layer `z` (the high byte of the
  object's word at `+$8`: ground tiles z=4, then 3, 2, 1) and drawn back to
  front. The object draw code is `sub_12dcc`; the blit setup is `$13018-$13164`.
  Position on screen: `x = obj.x - hotX`, `y = obj.y - scroll - hotY`, where
  `scroll` is the buffer's word at `+$8` (= `$DCA(a6)`, the scroll value of the
  screen top). Verified: no mirroring.
* HUD text is drawn by the CPU (not the blitter).

## Rendering emulator frames

```sh
.venv/bin/python tools/render_frames.py SWIV.adf out_dir 1300 2400 4500
```

`tools/render.py` runs the copper list line by line (bitplane pointers,
modulos, BPLCON0/1/2, display window, colour registers, 8 hardware sprites) and
supports normal and EHB playfields. `tools/amiga68k.py` now emulates the
blitter (A/B/C/D, shifts, masks, minterms, descending, basic fill and line
mode). The game starts by itself under emulation (fire is tapped); gameplay is
reached by about frame 1100.

## HUD and player (verified against the running game)

* **Text.** `sub_148F4` prints a string; `_xNNN _yNNN _cNN _aN _n` are inline
  commands (`_a0` centre, `_a1` left, `_a2` right). The font is a table at
  `$1C344`, 16 bytes per character code: width word + 7 row words, glyph in the
  low `width` bits (MSB leftmost); lower-case shows the upper-case glyphs;
  advance = width + 1. `[` is the heart and `*` the star.
* **Status strip.** `Heli 3[ 2* 0000000` at x=8 (left aligned) and
  `Jeep 0[ 2* 0000000` / `PRESS FIRE` ending at x=312, on screen rows 8-14.
  It is a separate 1-bitplane 352x8 strip (`$E10/$E14(a6)`) that the copper
  shows as a fifth plane (BPLCON0 `$5200`) for those 7 lines, so a lit pixel
  selects colour `16 + playfield index below it`.
* **Colours 16-31** are groups of four: `000/fff/999/animated`, with colour 16
  a bevel gradient `ddf aae ccf ccf ccf aae 88d` over the 7 rows. The animated
  entries (19, 23, 27, 31) take four consecutive words of the table at
  `$11B22`, starting at `counter & 15` (counter at `-$42(a6)`).
* **Player.** Two player records at `$2BA8(a6)` (Heli) and `$2C5C(a6)` (Jeep),
  each with its own task (`l_15FEC`). The helicopter is images 0-4 of
  `JEEPHELI.LIN`: logic runs every second tick and draws image 0 on one step and
  a rotor image (3, 4, 1, 2 in turn) on the next. Start position on screen
  (288, 192). The shadow is the same image 16 px right and 32 rows down in
  "erase" mode (`D = C & ~A`: opaque pixels become colour 0).
* **Object flag byte (`$15(a0)`) selects the blit mode** in `sub_12E30`:
  bit 5 = erase (shadow, minterm `$0B0A`), bit 4 = minterms `$0BFA/$0B0A`,
  bit 6 = draw into the background buffer as well, bit 7 = draw nothing; bits
  0-3 pick pre/post hooks. Some of these come from the `.LIN` frame flags
  (`$1A(a1)`): 0x10 -> bit 7 (marker frames, never drawn), 0x02 -> bit 1,
  0x04 -> bit 3.

## Player movement, weapons and bullets (measured on the running game)

Driving the emulator with `Amiga.joy1` / `fire1` (fire on port 1 starts the
helicopter; its record is at `$2BA8(a6)`):

* **Logic rate.** Player and bullet logic run every second display tick
  (25 Hz); the level scrolls one row per four ticks.
* **Record fields** (offsets in the player record): `+$40` direction bits
  (1 up, 2 down, 4 left, 8 right), `+$46` x, `+$48` y in scroll units,
  `+$60` fire cooldown, `+$62` its reload value (12), `+$64` weapon level
  (starts at 2), `+$68` spread flag, `+$3A` player number.
* **Movement.** Constant 6 pixels per logic step in each held direction (3 per
  tick) with no acceleration, one step of latency; limited to x 4..316 (measured again by holding the joystick left and right in the running original) and
  y 4..252 on screen.
* **Firing.** While fire is held a volley is launched whenever the cooldown
  has run out (12 ticks). The cooldown is decremented by the number of ticks
  per logic step.
* **Bullet pool.** 31 slots of `$30` bytes (`$5A0` allocation per player
  list): `+0` state (0 free, `$FFFF` live, 4..1 hit animation), `+2` vx,
  `+4` vy, `+$E` x, `+$10` y (scroll units), `+$24` image word. Spawn is
  `sub_14FC4(d0=x, d1=y, d2=vx, d3=vy, d4=image word, d5=class)`; the update
  loop (`l_14F2E`) adds velocity once per tick and frees a bullet that leaves
  the playfield. A hit turns it into a short animation of BULLET.LIN images
  7, 6, 5.
* **Patterns** (`$17A60`): table at `$17B46` indexed by heading (8
  directions; the helicopter always faces up = index 6) and weapon level.
  Level 1..5 fires that many bullets with the first record's velocity
  (up 9 px/tick for the helicopter); odd levels start at record 0, even at
  record 5. Offsets (dx,dy) for level 2: (-2,-4), (2,-4); level 3: (0,-8),
  (-4,0), (4,0); level 5 adds (-8,8), (8,8). Level 6+ is an 8-way fan
  (table `$17D96`) using BULLET.LIN images 8..15. Bullet image for a heading
  is `8 + heading index` (helicopter: 14).
* **Jeep** uses the same routine with a rotating heading (`$166(a5)`) and the
  `$68` flag selecting per-bullet velocities (spread).

## Hardware sprites, palette timing and draw order (verified)

* **Bullets are hardware sprites.** Frames with header flag `0x08` take the
  sprite path (`l_12D0E`): the image is copied into one of 8 sprite DMA lists
  in the buffer descriptor. The channel is `counter & 7` where the counter
  starts at `$40` each frame and decrements per sprite object, so channels
  are handed out `0, 7, 6, 5, 4, 3, 2, 1, 0 ...` in drawing order. Objects
  come from the pool from the highest slot to the lowest.
* **Sprite palette** (`sub_11ABC`): per channel pair `g` colours `17+4g,
  18+4g, 19+4g` = white, grey (`999`) and one entry of the animated table at
  `$11B22` (`counter + g`, wrapping at 16). A bullet pixel of value 1/2/3 uses
  those, so bullets look white/yellow/red and flicker. They do **not** use the
  playfield palette.
* **Palette timing.** A colour command's effect begins two rows later than
  its scroll value (the copper wait lands two lines below), i.e. screen row
  with scroll value `L` uses the state after all commands with `S >= L + 2`.
  Lines past 255 are reached with `WAIT 255,$E0` followed by waits for
  `256 + vp`.
* **Draw order.** Objects are kept sorted by `$8(a0) = z<<8 | ordinal`,
  highest first; ground tiles are z=4. The leftmost ground tile of a row has
  position field `0x7FF`, which the game sign-extends to x = -1 and
  z = 4 (see docs/formats.md).
* **Markers.** `.LIN` frames with flag `0x10` (e.g. `_stop.lin` image 0,
  87 objects in town) are invisible markers.
* **HUD text** is the blue bevel gradient of colour 16 everywhere (the
  emulation of the 5th-plane strip gave a colour that depended on the
  scenery underneath, which does not match the real game).

## Enemy tasks (ported in `internal/actor`)

Every enemy is a coroutine of the game's own kernel with a `$222`-byte state
block (a5). The routines are plain 68000 code written against a small helper
layer, which `internal/actor` ports line for line (each Go function names the
address it comes from).

**Kernel.** Tasks sit in a ring sorted by priority (enemies: 100) and switch
with `k_Yield` (`-$59E`). `k_Wait8a` (`-$58A`) compares a per-task copy `$C(a5)`
with a global generation word (`$CA(a6)`); `k_Sig86` (`-$586`) decrements the
copy, i.e. asks the task to end: the task finishes the current step and its
next kill check reports it (`bne` to `$1930C`, which releases the weight and
orphans the children). `$-4C(a6)` is the number of frames elapsed per logic
pass (2 at 25 Hz, more when the game is slow); `$-42(a6)` counts frames.

**State block.** `$140/$144/$148` x, y, z (16.16, y is a scroll coordinate),
`$14C/$150/$154` velocity, `$158/$15C/$160` acceleration, `$164` speed,
`$166` heading (0..255, 64 = down), `$168` hit points, `$16A` score, `$172`
weight, `$114..$122` parameter words (`$114` starts as the script's kind),
`$134` parent, `$138/$13C` child list, `$16C` despawn margin (-64), `$16F`
flags (bit0 no shadow, bit1 hit flash, bit3 follow the parent, bit4 fixed to
the screen), `$170` the constructor's image word, `$176` debris count,
`$17C/$1A6` the two animation channels (`$18E`, `$1B8` = current image word),
`$18D` blit attributes, `$1FA/$1FC/$1FE..` event word, mask and handlers.

**`$15292` (step).** kill check; `d7` times `v += a; p += v` (`d7` = frames
elapsed; if bit3 and a parent exist: copy its x,y,z first and integrate once);
z < 0 clears z, vz, az; `$15440` off-screen test (outside x -64..384,
y -64..320 relative to the scroll calls the `$21A` callback = end the task,
unless `st.b $21A`); animation ticks; **yield**; then (bit4) `y += scroll
delta`, clear the flash bit, `$21E` callback if the parent is gone, event
handlers (hit: `$19322` `hp -= 1`, at 0 `$1932A`), kill check.

**`$19286` constructor** `(d0 image word, d1 flags, d2 wait row, d3 hp,
d4 score, d5 weight)`: sleeps (yield only, no integration) until the task's
screen row reaches `d2` (`$18A88`; compares with `$DCA`, which leads `$DD6`
by a frame), then draws, takes `d5` of the 160-point enemy budget (`$177E2`
ends tasks created over the cap), registers the hit handler. `d1` bit1/bit2
make the task answer the air / ground weapons.

**Helpers.** `$177FC` the only random generator (`s <<= 1`, xor `$1D872B41`
unless it carried and is non-zero, swap halves); `$155B2` velocity from
heading and speed (`sin/cos` table at `$15A42`, amplitude 256); `$155D0` aim
(table at `$1562A`, 32x32 quarter circle, both distances halved until they
fit); `$1557E` turn towards a point by at most `d2` units per call;
`$19262` formation (`d2 - 1` clones, each offset by `d0,d1`, `$114 += d3`;
the clones continue after the call); `$15104/$1510A` child; `$15138` clone;
`$15C48` animation script: words are images (bit15 clear) or commands
(`$8000` stop, `$8800` end the task, `$9000` loop, `$9800` loop start,
`$B000` delay, `$B800/$C000` set/clear attribute bits; default delay 3).
Death clones the task twice: the blast (`$1790A`, EXPL1 images 7..13, delay
4) and, if `$176` is set, the wreck (`$1794C`, an image stamped into the
ground).

**Script loader (`$12562`).** A record is read once the *previous* record's y
is within 256 rows of the top (`$DCA - $100 <= prev`). Records below the top
(`y > $DCA`) are skipped. Kinds 1..15 create a task at the word found in the
type table `$16422`; the record's low 9 position bits are x, its y goes to
`$144`, the kind to `$114`.

**Rank (`$B6`).** Difficulty rank, recomputed every pass by the main loop
(`$10C94`): `((age1 >> 8) + (age2 >> 8) + ((level1 + level2) >> 2)) >> 3`
plus `max($B8 - 1, 0)` (the script being read), capped at 10, where age is
`$6E` (+1 per pass alive, reset at death, +$1770 at an extra life) and level
is the weapon level `$64` (player 2's idle record holds 2). It is 0 at the
start. At rank 0 the popup and proxmine die right after
their constructor, hit points of foddera/yellow/medtank are rank-dependent
(`rank + 1`), formations get one more member at rank >= 2.

| type (town) | routine | behaviour |
|---|---|---|
| foddera `0404` | `$17008` | formation of 4 (5 at rank 2), x = 32 + (r & 255), vx = int16(r >> 16) * 2, ay = 0.03125, steered back into 32..288; every 4th group scatters |
| yellow `0014` | `$1767C` | formation of 6 (y -8 each); down at 2.0, homes in on the player at speed 2 * (|dx| + |dy|) once above it |
| bird `0015` | `$175EE` | formation of 4 (x +32, y -4); vy 1.0..1.5; knocked 6 rows back when hit |
| rotobase `080D` | `$1890E` | ground; rotating animation, 4 shots every 200..327 frames |
| mine `0011` | `$18AD6` | ground; spinning animation, one hit kills |
| train `0010` | `$18B3E` | crosses at 1 px/frame, kind = number of cars chained behind it (48 px apart, they copy the engine's speed) |
| medtank `0003` | `$18E8A` | hull drives in from the side/bottom (0.5 px/frame) for 300 frames, stops for 100, three times; turret child `$18F6A` turns 16 units per 6 frames towards the player |
| camogun `0016` | `$19BD2` | ground; recoils 8 rows every 100 frames |
| mill `0058` | `$16994` | drifts down at 0.5, rotating overlay (`$183A2`) |
| popup `000E`, proxmine `000F` | `$1966E`, `$199A0` | rank >= 1 only |
| flame `0013` | `$19AD0` | ground; flame child `$19B38` |
| goose `0017` | `$1B74A` | the town's end boss, not ported |

**Collision (`$15E24/$15E82`).** Every constructed task has an object block
at `$1E8`: x,y saved before the yield (`$1F0/$1F2`), half sizes `$1F4/$1F6` =
the cx, cy of the picture it shows, class word `$1F8` (= the constructor's
d1), event word `$1FA`. Once per pass every object whose class bit 15 is
clear scans the others: those with |dx| <= its half width and y within
+-half height get its class ORed into their event word, and it collects
theirs. `$15476` runs the handlers of the events that are set and listened
to (bit 0 `$1FE`, 3 `$202`, 4 `$20A`, 1 `$206`, 2 `$20E`, 5 `$212`; mask
`$1FC`; registered by `$154FE`, `$1550A`, `$1550C`, `$15518`, `$15524`,
`$15526`, `$15532`, `$1553E`, removed by `$1554A`...). Classes:

| object | class | meaning |
|---|---|---|
| player bullet (P1) | `$8041` | passive; raises bit 0 (hit) and 6 (P1 gets the score) |
| enemy bullet | `$8006` | passive; raises bits 1, 2 |
| air enemy | `$22` | bits 1, 5; listens to bit 3 (d1 bit 1) |
| ground enemy | `$24` | bits 2, 5; listens to bit 4 (d1 bit 2) |
| helicopter (P1) | `$48` | bits 3, 6; listens to bit 1 with the damage handler `$182C6` |
| jeep (P1/P2) | `$50`/`$90` | listens to bit 2 |

So the helicopter is hurt by air enemies (touching them kills both) and by
enemy bullets, not by ground enemies. Player bullets die and score when they
touch something with a class; one event is one hit point however many bullets
touch at once.

**Player (`$183D0` helicopter, `$16050` control task).** `$182C6`: unless
`$6C` (invulnerability, 200 after a start, counts down by the frame lag)
or `$6A` (shield) is non-zero, the vehicle task ends and `$178BC` spawns
sixteen debris pieces (`$17912`, heading +100 each, speed +$300 each, blast
animation) at its place. The control task waits 100 ticks and starts the
next vehicle at (288, row 192) when `$44` (lives x 4, -16 at the start) is
not zero, adding 4 to it; each 10000 (then every 30000) raw score points
(`$54`) take 4 off `$44` (an extra life). HUD lives = -`$44`/4.

**Draw modes (attribute byte `$15` of the object blocks, measured on frames).**
Pictures whose frame flag is 8 (all of BULLET.LIN: enemy bullets, the
player's) are hardware sprites painted with the sprite palette (colour 1
white, 2 grey, 3 animated). Everything else is blitted into the playfield:
bit 5 (`0B0A`, "A-not-C": the picture's pixels become colour 0) is the shadow
and the mill's rotor overlay (`$1B8` words of JEEPHELI.LIN, whose blur
pictures are checkerboard masks, so they leave a dithered dark patch; attribute
bit 7 of the overlay hides it every other frame); bit 4 (`0BFA`, "A-or-C" on
planes 0 and 3) fills the silhouette with colour 9, white in every level
palette: the hit flash (`$16F` bit 1, one pass after a hit that did not
destroy) and the helicopter's immunity flash (`$6C` bit 3: four passes on,
four off).

**Tokens and shields.** A token (`$18698`, TOKEN.LIN, class `$20`) flies
out of where it was dropped, then drifts down listening to bit 0 (player
bullets: each hit knocks it 8 rows back and cycles its kind 0..3, then it
becomes kind 4 after twelve cycles) and to bits 3/4 (touched by the
helicopter: pickup handler `$18786`). The pickup raises the player's token
count `$66` (max 19) and applies the kind (`$114`): 1 = spread weapon
(`$68` = $FF; a second one of the same kind raises `$64`, the weapon level,
up to 5), 0/5+ = parallel weapon (`$68` = 0, same rule), 2 = volley
interval `$62` -3 (min 8), 3 = `$6C` += 500 ticks of invulnerability and
`$4C` += 500 (x10 points), 4 = level 6, interval 8 and the smart bomb
(`$17812`: `$A9` set for 50 ticks, every task with a `$216` callback takes a
hit each pass, enemy bullets leave). Only the town's boss drops tokens
(`$1B960`: n per player, spread evenly round a circle, called from
`$1B8DE`/`$1B956`). A destroyed mine releases the shield capsule (`$18820`,
10 hit points; touching it sets `$6A` = -1, the helicopter's task answers
with the glow `$188B2` and `$6A` = 500 ticks during which `$6C` is kept at
100; touching it while shielded or shooting it dead is the smart bomb). A
new helicopter caps the weapon level and takes the fire rate by tokens
collected / 5 (table `$16080`: 2/11, 3/10, 4/10, 5/8). Weapon types: with
`$68` != 0 every bullet of a volley uses its own record's velocity (a fan),
else all share the first record's.

**The goose (`$1B74A`, the town's boss).** Five kinds of task: the body
(`0017`, 25 hit points, class `$22`, flies down from y+288 to row 72 then
sweeps sideways with `$14C` steered by `$1B83E`, firing `$18592` plus two
`$174F0` pairs every `($B6 - 12) * 4`-ish frames), two engine pods
(`$1B9AC`/`$1B9B0`, `0C17`) and a tail (`$1B9A2`, `0E17`) that follow it with
their own offsets (`$1BB38`), and a fifth part (`$1BA6C`, `1017`) that
shuttles across the screen. Hits knock it back (`$1B934`: `$14C` negated);
at 0 hit points `$1B946` orphans the parts, drops tokens (`$1B960`: 2, or 3
when its `$118` is 500 or less) and dies with `$1932A`. The pods read their
parent through `$134` after it is gone: in the original that is address 0 and
reads zeroes, in the port `Task.parentTask` hands out a zero task.

**Level chaining.** The loader task (`$12500`) loads `$AC` (the next level
number) into `$B8`, reads that level's script into `$E04` (`$127C0`: file
name index, dictionary offset and `$90` from the table at `$1280C`) and runs
the record loop. When the script is used up it loops: the next script is read
(the log shows `desert.pam` opened at frame 12301, while the town is still on
screen) and its records carry on from `$E02`, where the previous script
ended: there is no level start in between, only a new dictionary (`$BA`) and
the new files. `$1278A` (which sets `$DD2 = $E02 = $E9C0 << 16`) runs once,
at game start. The scroll never stops for a level change (it does stand
still for a while when files are loaded from disk). The first record of the
desert is a palette command 96 rows after the town's end; the rows in between
show the ring buffer's old content (320 rows further down the level).
`level.Chain` loads the scripts end to end and `Level.Draw` falls back to
the row 320 further down for rows no object covers.

**Screen flash.** The smart bomb (`$1781A`) sets `$2B9E` to `$100`; the
display routine (`$118A2`) adds `$2BA0` (-4 in play) to it every frame until
it is 0 (64 frames), and the palette code (`$14D68`, `$14E06`) blends every
colour of the playfield, the status line and the sprites towards white by
level/256, nibble by nibble (`$13A00`: 15 - ((15 - c) * (256 - level) >> 8)).

**Enemy fire.** `$18592`: if `$CE` (bullet budget, 4) >= 0, clone the bullet
(`$185B2`: aimed at the player, `$80` speed accelerating by `$80` 20 times,
every 5 frames) and the muzzle flash (`$175B0`); `$1858A` fires along the
task's heading, `$18582` a slow shot (`$500`). The bullet is a task of
class `$8006` that dies on events 3/4 (the helicopter's bit 3 included), when
it leaves the screen or on `$A9` (smart bomb); it costs one of the budget.
`$174F0` fires the homing pair `$17526`/`$175B0` (the popup's mortar).

**Lifter.** `tools/lift.py` turns a routine range of `AMPROG.OBJ` into Go
(`internal/actor/lifted.go`, regenerated by `tools/lift_all.sh`): one
function per routine, subroutine and continuation, registers as fields of
`regs`, condition codes as `flags`, every instruction as the statements it
performs, with the helper layer (`$15292` step, `$19286` constructor,
waits, `$19262` formation, children/clones, `$15C48` animation with its
inline script, ...) mapped to the port's methods. State-block and global
offsets are named through two tables at the top of the tool; unknown calls
are reported (`t.unported`) and unknown instructions left as `TODO`. The
lifted routines are the game's enemy code (the hand-written first port was
checked against the lifter's output pass for pass, then removed). Everything
in the routine ranges comes with them: the flame's fireball, the fire
routines, the helicopter's death. To lift another type, add its entry (and local range) to a
script like `tools/lift_all.sh` (it lifts every type of the table at `$16422` plus the shared code; `jmp (a3)` of the sound code returns, `jsr (a0)` of the aiming routine and table-driven spawns go through `funcAt`; left as TODO: the train's checksum `$18bce` and `pea $16c4a`).

**Verification.** `tools/trace_enemies.py` records every `$15292` entry (the
whole state block), every `$177FC` call and every death of the running game;
`tools/make_fixture.py` compacts it into `testdata/town_trace.json.gz`. The
test replays it (scroll, frame counter, player position and generator state
as the original saw them; the same enemies shot down after the same steps)
and requires the port's steps to be the original's: exact for the first
~1250 logic passes (~9800 steps: all town types, enemy bullets, blasts,
wrecks), and the helicopter has to be shot down in the same passes as the
original's (both deaths of the stretch).

## Offline level composition (verified)

`tools/level_map.py` rebuilds a level's background from the `.PAM` and `.LIN`
files alone. Compared pixel for pixel with the emulator's clean background
buffer: **97.6% match at frame 1300, 96.3% at 1800, 92.2% at 2400** (town).
The remainder is shadows, ground-grain phase and the non-zero-kind objects.

## Still unknown

* The meaning of the non-zero object kinds (155 of ~1000 records in town; they
  go through `sub_1654a`/`sub_15120` and have a block of per-object state at
  `+$140..$160`, so probably ground enemies), the `.LIN` header `flags` bits
  1-2 and `cx,cy`, and how shadows are produced (the game shows black where
  my offline composite shows ground, e.g. under the bunker).
* How `.LIN` frame groups (primary/secondary) and the `flags`, `cx/cy` header
  fields are used by the draw routines.

**Scroll stop at the bosses.** The scroll task (`$12C6E`) only advances while
the background drawer is ahead (`$DCA + $F0 >= $DD2`); nothing in AMPROG reads
the boss flag `$A3` that the goose sets (`$1B74A`) and clears when it dies
(`$1B902`), so the stop is the port's own: the scroll stands still from the
moment a boss has come down to screen row 64 (`bossStopY`; the bosses
are constructed 32-63 rows above the top edge, the last one at row 0) until
it is destroyed (`World.BossEngaged`; freezing at load would deadlock, the boss
waits for the screen to reach it), with a 3000-pass safety net.

The bosses are the tasks constructed (`$19286`) with 200 hit
points (the last boss `$1B028`, word `0056`, 20000 points, which waits for bit
3 of `$A6` before it constructs) or 2000+ points (the goose, `$1B74A`, word `0017`, weight $64, is
not a boss: the scroll does not wait for it):

| word | routine | level | hp / points |
|---|---|---|---|
| `161c` (+ `121c`, `1c1c` parts) | `$1A7D0` (`$1A914`, `$1A68E`) | desert | 2500 |
| `042c` | `$1AAA8` | river, near the end | 2000 |
| `0631` (+ `0632`) | `$1AAEE` (`$1AF02`) | start of scifi (the ice's end) | 300 hits, 7500 |
| `0c32` | `$1AD96` | scifi | 10000 |
| `0056` | `$1B028` | final | 200 hits, 20000 |

These were found by scanning every `$19286` call for large values; their
routines were lifted with the other types. Whether each stands where the
original stops the scroll is not verified.

**The last boss and the level end.** The last boss (`$1B028`, word `0056`,
hp 200, 20000 points) is placed by `FINAL.PAM` right after the scifi level; it
waits for `$A6` bit 3, the "big boss on stage" flag the scifi bosses raise
(`$1A66E`, cleared by `$1A67A`), then constructs, strobes the screen (`$2B9E`
white, `$2BA2` black, alternating each pass) and takes its hits (`$168`, one
per hit via `$1B0E4`). When they are used up: smart-bomb flash (`$17812`), a
ring of blasts (`$17836`, 12 rounds with the picture shaking by 3 rows,
`$1B17A`), then `$8E = -1`: the fade to black (`$11870`: `$2BA2` += `$10` per
frame to `$100`, then `$8E` is cleared). The boss then sets `$3041` bit 3 (game
complete) and ends.

The main loop (`$FD62`) shows the end screen: "Game Complete" (or "Game Over"
when bit 3 is clear) and the counters `$30C6` bullets fired (`$14FD4`, one per
bullet slot), `$30D2` enemies destroyed (`$1932A`), `$30CE` enemies
constructed (`$192A4`; "escaped" is `$30CE - $30D2`), `$30CA` tokens picked
up (`$18786`) and the percentage of the level (`(E9C0 - $DCE) * 100 / $68C1`,
100 when complete). The port draws that screen once the fade is done
(`LevelScene.drawStatistics`); the high-score entry that follows in the
original is not ported.

**Game over and the high scores.** `$3041` bit 0 (set by `$11800` when both
players are out of lives) starts the same end screen as the game's completion
(`$FD62`, "Game Over" while bit 3 is clear), then `$11FA8` offers each player
whose score beats the table's last entry (`$12000`) the name entry (`$1201E`:
`1up/2up player please enter your name`, typed in place on the row of the new
score, name at x=32, y = `$50 + 16 * row`, uppercase `$20..$5A`, at most 31
characters and `$B0` pixels, Return ends, an empty name keeps the old one),
then "TODAY'S BEST SCORES" (`$1232C`, score column right-aligned at x=`$122`,
values x10). The table starts with the names of an `HS*.TXT` file (the boot
loads `hs7.txt` and `hs16.txt`) and the scores 70000, 60000 ... 10000 (`$123BE`,
in tens of points). The port keeps one table (HS7) for the session; `-unlimited-lives`
keeps the helicopter coming back.

**Title and menu flow.** The main loop (`$FD24`) runs the pages in turn, each
one skipped when a key or fire is pressed (`$117E6`): the identity/copyright
screen (`$114E0`, a protected string in `$30D8`), the credits line and "PRESS
HELP FOR OPTIONS SCREEN. PRESS FIRE TO START GAME." (`$113BE`), the vehicle
pages `$10718` (SWIV-1) and `$10558` (SWIV-2): the blueprint (`HELIBP2/JEEPBP2`)
is loaded and the description typed out by tasks (`$10AA0`, `$10ACA`), then the
coloured picture (`HELIBP1/JEEPBP1`) fades over it (`$1092C`, `$109A4`), the best
scores (`$122A8`), the programmers (`$111F0`) and the options screen (`$10F1E`:
`1up 2up`, `F1-F3`, `F6-F8`, "Press function keys to select controls").
The port (`menu.go`) runs title (cover + `AMTITUNE`), info, SWIV-1, SWIV-2,
scores and programmers pages; the typewriter positions and the cross-fade are
approximations. HELP is `H` or `F10`; F1-F3 / F6-F8 choose keyboard arrows,
WASD or a gamepad (2up has no vehicle yet). After the end screens the game
returns to the title.

**Goose check against the original (frames 4806-7200 of a boot).** The body,
the four parts and the two homing missile pairs were traced in the running
original (`tools/trace_enemies.py` with an id filter) and in the port with the
same fire pattern: the arrival (x 160, y from 288 up to row 72 at -2), the
parts joining (`$114` 4 to 0), the descent at +1 to row 192, the slow climb at
-0.25 with the +4 kick below row 64, the sideways sweeps (x 140-310) and the
missiles (+-3 px sideways first, turning 14 units per 2 frames towards the
player) agree value for value; the scroll never stands still for it. The one
difference found: `$162D2` (used by the goose to aim) with no player alive
aims at x `$A0`, y `$DCA + $C0`, not at the `$16266` fallback.
Not modelled: the sound routines (`$13C18`, `$13E06`, `$144FA`, ...) draw
random numbers when a Paula channel is free (28 draws in 4300 frames), so
the generator drifts from the original's over a long run.

**Parent loss and death ($21E).** After every step (`$15418`) a task with a
non-negative `$21E` and no parent (`$134 = 0`) calls it: `$15104/$1510A`
children start with `$15D74` (ask for the end), the goose's fifth part uses
`$1932A` (die with a blast), a negative word switches it off. The lifter had
turned every store into "off", so the pods and the fifth part outlived the
goose and drifted away one by one. `Task.orphanCb` now carries the three modes.
Also: a task that has run `$1932A` is out of the picture and the collision
lists at once; the goose's routine keeps running for about 200 frames after
its death (the program checksum loop at `$1B910` yields every 256 words), and
its last picture used to stay on the screen all that time.

**Desert base ($1A7D0, word `161c`, 90 hits, 2500 points).** Ground
installation placed with its turrets (`121c`, `1c1c` and their children by
the script). It waits for screen row `$54` (`$18AA8`), then counts itself in
(`$1A66E`: `$8C` += 1, `$A6` bit 3: the "big boss on stage" flag the scroll
stop of the port now keys on, instead of its own row test), fires `$1A866`
volleys (`081c` shots from `$1A8C6`), launches a medium tank (`$18E8A`, kind 3)
every 4th round and dies through `$178(a5)`: the death blast of a task is the
routine at `$178` (default `$1790A`, here `$1A93C`: a ring of blasts, others
`$17836`, `$178AC`), which `Task.Die` runs through `funcAt`. When `$8C` is
down to 1 the smart-bomb flash (`$17812`) goes off.

**Running a single level in the original.** Writing the level number to
`$AC(a6)` every frame (from frame ~5) makes the loader (`$12528`:
`$B8 = $AC`, `$127C0`) read that level's script: `$AC` 0..6 is town, desert,
grass, river, ice, scifi, final (`FindFile river.pam` at frame 660). With the
immunity word `$6C` of the helicopter record kept high, `tools/survey_level.py`
(every task instance: id, first frame, position, hit points, pictures) and
`tools/spawn_graph.py` (which task id clones/children which entry address)
describe an enemy's behaviour in the original without playing it.

**Enemy survey, first findings (river, frames 1400-3800).** `mama` (`0025`,
`$16B6A`, 70 hits) comes down at 0.375 px/step with an overlay child
(`0225`, `$16BA6`) that spawns a baby (`0625`, `$16C04`, 1 hit) every 4 steps;
in the original run neither the mother nor the babies spawned a single bullet.
Not yet identified: the "huge helicopter" that releases small flyers firing
star bullets, and the mine that is meant to explode into shrapnel.

## Sound effects (`$13A60-$14720`, package `internal/sfx`)

The effects are not samples played by a driver but small programs, one per
Paula channel. Interrupt `$13A7C` resumes the four
voices in turn (`$2A22 + n * $10C`: voice 0 = channel 3 left, 1 = channel 2
right, 2 = channel 1 right, 3 = channel 0 left); a voice is a coroutine with
its own stack that waits for the next frame with `$13AA0` (which also counts
the priority at `+4` down). A voice block holds the parameters `d1`/`d2` at
`+0`/`+2` and a 256-byte buffer at `+$C` for the waveform; the programs write
`$A0` (pointer), `$A4` (length in words), `$A6` (period), `$A8` (volume) of
the channel and switch its DMA on through `$96`.

* `$13B4C` / `$13B74` start a sound: the right (or left) pair of voices is
  tried first, then the other; the voice with the lower remaining priority is
  taken when the new priority (`d0 * 4` frames) is greater. `$13BCC` chooses
  by position: `x < $A0` prefers the left channels.
* `$13AA0` yield, `$13A74/76/78/7A` wait 4/3/2/1 frames, `$14696` waits `d0`
  frames, `$146A6` fills the buffer with shift-register noise (`$1D872B41`).
* Sampled: `$13C32` plays `BIGEXPL.SND` (period `d1`) and `$13C92`
  `SMART.SND`, once; everything else is synthesised from 8-16 byte waves, noise
  or the sine table at `$15A42`.
* Jingles (`$145C0` the extra life: six rising notes, `$145D4` a token: four
  notes) are a small task of their own (`$1460E`) that starts one voice
  (`$14632`, a pluck whose timbre morphs) every five frames.

Where the game calls them (the caller's `d0` is the pan position):

| entry | sound | called from |
|---|---|---|
| `$13C18` | blast, one BIGEXPL voice at a random pitch | `$1790A` default death blast |
| `$13BFC` | small blast, two voices | `$17836`, `$178AC` (death rings) |
| `$13BDC` | big blast, four voices | `$178BC` helicopter destroyed |
| `$13C72` | SMART.SND x 4 | `$17812` smart bomb |
| `$13EFE` | whoosh (a rising period, 32 frames) | `$17A64`: every volley of the helicopter (`Shots.launch`) |
| `$14030` | falling blip | `$19312` an enemy hit but not destroyed |
| `$1437E` | enemy shot (noise pop, falling pitch) | `$185AC` bullets |
| `$145D4` | token jingle | `$1878E` token picked up |
| `$145C0` | extra life jingle | `$160E8` |
| `$140F8` | pop-up sweep | popup, proximity mine, flame, plat |
| `$13DEE/$13E06`, `$144FA` | noise bursts, warble | boss/goose hit and death (`$177F4/8`) |
| the rest | blips, sweeps, rumbles | the enemy types that call them |

The port runs the same programs in `sfx.Engine` (one goroutine per voice,
stepped at 50 Hz, rendered to 44.1 kHz stereo; `Task.Sound(entry, x)` is the
hook, the lifter turns every call into the sound code into it). Their random
numbers come from their own generator, not the game's. `$14194` (the
wind-up of the loading screen) and `$13E64/$13F5E/$141FA` are not called
anywhere and are not ported.

Port note: the effect stream's buffer must be a real duration
(`Player.SetBufferSize(40 * time.Millisecond)`; it first got a byte count,
i.e. a few microseconds, which starved the stream and made the effects
crackle, drift and drop out), and the engine's tick follows the game's
`-tps` so that a faster or slower game keeps its sounds in step.

**The sound interrupt is not the vertical blank.** `$13A26` programs CIA-B
timer A (`$BFD000`: control `$E00` one-shot, latch `$0D88` = 3464 ticks of the
709379 Hz E clock, enable mask `$81`, vector `$78`) and `$13AC0` restarts it at
the end of every interrupt: the voices step about 204.8 times a second, 4.1
times the picture's rate. All the waits (`$13AA0` and its multiples), the
priority counts and the sweeps (`period += period >> 3` per step) are in these
steps; the port had them at 50 Hz, which made every synthesised effect
four times too slow, hence too low and too long. The jingle task (`$1460E`)
waits in kernel (game) frames and stays at the game's rate.
