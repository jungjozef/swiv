# SWIV disk tools

Reverse-engineering and extraction tools for the Amiga SWIV ADF (Sales Curve,
`SWIV.adf`, with a Skid Row cracktro). Python 3, using a project-local venv:

```sh
python3 -m venv .venv
.venv/bin/pip install capstone unicorn
```

## Extract the game files

```sh
.venv/bin/python tools/extract.py SWIV.adf assets
```

Writes all 128 files to `assets/` (git-ignored). Uses only `unpack.py`, no
emulator and no third-party packages.

## Disk format

| ADF offset | What |
|---|---|
| `$0000` | Boot block. Loads the cracktro (`$D5400`) and the original kernel (`$D9400`, `$2C00` bytes). |
| `$1600` | `u16 N` (= 128), then `N x u32` packed size per file. |
| `$1602+4N` | The files back to back. Each = `u32 unpacked length` + LZ stream. |

File 0 is the catalog `AMDLS0.CAT` (names, newline separated, index = file no.).
File 1 is `AMPROG.OBJ`, the game program (loaded at `$EFC0`, entry `bra.w $FC32`).

### LZ stream (kernel code `$B1C-$D3C`)

* 1KB sliding window, MSB-first bit codes **interleaved** with literal bytes in
  one byte stream. Window position and bit state reset per file.
* Alternates literal run, match, literal run, ...
* `code()`: `0` -> 0; `1 xx` -> xx+1 (xx != 11); `1 11 yy` -> 4+yy (yy != 11);
  `1 11 11` + 11 raw bits.
* Literal run: `code()` bytes copied verbatim. Match: 10 raw bits offset,
  `code()+2` length (0 if > 63).

## Emulation tools (exploratory)

| File | Purpose |
|---|---|
| `amiemu.py` | 68000 harness on Unicorn: RAM, fake ExecBase, custom/CIA regs, VBlank IRQ, floppy drive + MFM track DMA from the ADF, 68000-style `rte`/exception frames. |
| `kernel.py` | Boots the Sales Curve kernel under `amiemu`. Usage: `kernel.py SWIV.adf [max_insns] [trace_len]` |
| `stage2.py` | Runs the cracktro decruncher and dumps memory. |
| `hwtrace.py` | Logs distinct hardware register accesses. |

**Superseded:** the Unicorn harness above is kept for history only. Unicorn's
68k core loses lazily-computed condition codes whenever emulation is stopped
and restarted (even reading SR corrupts them), which made the unpacker appear
to be mis-emulated. The `machine68k` (Musashi) harness below does not have
this problem and runs the kernel and game at native speed.

## Musashi harness (use this one)

```sh
.venv/bin/pip install machine68k numpy
.venv/bin/python tools/trace_boot.py SWIV.adf 4000   # log every file the game loads
.venv/bin/python tools/pam.py assets/TOWN.PAM 60      # decode a level script
```

| File | Purpose |
|---|---|
| `amiga68k.py` | Musashi-based harness: 16MB RAM, custom/CIA register pages, fake ExecBase with A-line trap stubs, floppy drive serving MFM tracks from the ADF, 50 Hz VBlank + copper IRQ, fire-button input, per-address watch hooks, `boot_kernel()`. |
| `mfm.py` | AmigaDOS MFM track encoder used by the drive. |
| `trace_boot.py` | Boots kernel + game, taps fire, logs FindFile/Read calls by frame. |
| `pam.py` | Decoder for `.PAM` level scripts and the in-program level table. |
| `render.py` | Renders a frame from emulated custom-chip state (copper, bitplanes, palette per line, sprites). Needs numpy. |
| `render_frames.py` | Boots the game and saves PNGs of chosen frames. |
| `level_map.py` | Composes a whole level (ground, runway, buildings, trees) from `.PAM` + `.LIN` offline; ~96% pixel-exact against the game. Usage: `level_map.py assets LEVEL out.png all [0xSCROLL HEIGHT]`. |

## Enemy tracing

| File | Purpose |
|---|---|
| `trace_enemies.py` | Hooks `$15292` (task step), `$177FC` (random) and `$1932A` (death) in the running game and writes every task's state block per step. |
| `make_fixture.py` | Compacts a recording into `testdata/town_trace.json.gz` for `TestTownMovementMatchesOriginal`. |
| `enemy_types.py` | Lists the enemy types of a level with counts and routine entries. |
| `lift.py`, `lift_all.sh` | Lifts routines of `AMPROG.OBJ` to Go (`internal/actor/lifted.go`): `lift.py PROG ADDR[:name]... --local LO-HI -o out.go`. See docs/amprog-notes.md. |
| `dr` / `disrange.py` | Disassembles an address range of `AMPROG.OBJ`, understanding the inline-data calls (`$15C48` animation scripts, `$15D36`). |

## Disassembler

```sh
.venv/bin/python tools/disasm.py assets/AMPROG.OBJ 0xefc0 reverse/amprog.s
```

Recursive descent from the entry point plus a speculative pass (call targets,
task entry `lea`s, code after `rts`). Labels branch/call/data targets, marks
strings, names kernel services (`jsr -$5f2(a6)` -> `k_Read`) and custom-chip
registers. Findings are collected in `docs/amprog-notes.md`.

`amiga68k.Amiga` has `fire0/fire1` (CIA fire bits) and `joy0/joy1` (JOYxDAT
words, build them with `Amiga.joy_word(dx, dy)`) for driving the game.
