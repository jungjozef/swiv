package actor

// next is $177FC: the game's only random number generator. The state lives at
// $2BA4(a6); the value returned in d0 is the state after the swap.
func (w *World) next() uint32 {
	if len(w.SeedQueue) > 0 { // tests: replay the original's recorded states
		w.Seed, w.SeedQueue = w.SeedQueue[0], w.SeedQueue[1:]
	}
	d := w.Seed
	carry := d>>31 != 0
	d <<= 1
	if carry || d == 0 { // "bhi" falls through only when no carry and non-zero
		d ^= 0x1D872B41
	}
	d = d<<16 | d>>16
	w.Seed = d
	return d
}
