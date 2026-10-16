# Open visual glitches

Status: waiting for the user's screenshots (original vs port) before
anything is changed here.

1. **End of the town level, ground sprites drawn incorrectly** (reported
   2026-10-16). Functionality is fine, only the picture. The user will
   supply side-by-side screenshots of the original and the port; do not
   guess at it before then.

   What was already checked (nothing wrong found, so do not repeat):
   * the proximity-mine row (word `000f`, `$199A0`) at scroll ~`DCDC`: the
     pixels of the blocks and their shadows are identical to a frame
     rendered from the original under the emulator (`tools/render_frames.py`
     style, `-level 0 -top 0xdc60`);
   * pyramid, rail poles with spiked balls, birds and their shadows at
     `DD80..DC40` look the same as in the original frames;
   * the original frames were made with `tools/_orig_frames.py`-like runs
     (immunity written to `$2BA8+$6C` each frame, scroll value used as the
     frame selector); the port's with a `LevelScene` test pilot.
   Differences in game state (player position, rank, enemies alive) make
   such comparisons only approximate.
