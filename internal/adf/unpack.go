// Package adf reads the Sales Curve packed file archive stored on the SWIV
// Amiga disk image.
//
// The LZ scheme is a 1KB-window LZSS whose MSB-first bit codes are interleaved
// with literal bytes in a single byte stream (ported from the original kernel).
package adf

import "errors"

// ErrTruncated is returned when the packed stream ends before the expected
// number of bytes has been produced.
var ErrTruncated = errors.New("adf: packed stream truncated")

const windowSize = 1024

type bitReader struct {
	src []byte
	pos int
	cnt int // bits left in cur, minus one (mirrors the original's d5)
	cur byte
	eof bool
}

func (r *bitReader) bit() int {
	r.cnt--
	if r.cnt < 0 {
		if r.pos >= len(r.src) {
			r.eof = true
			return 0
		}
		r.cur = r.src[r.pos]
		r.pos++
		r.cnt = 7
	}
	b := int(r.cur >> 7)
	r.cur <<= 1
	return b
}

func (r *bitReader) bits(n int) int {
	v := 0
	for range n {
		v = v<<1 | r.bit()
	}
	return v
}

// code reads the variable-length number used for run and match lengths.
func (r *bitReader) code() int {
	if r.bit() == 0 {
		return 0
	}
	v := r.bits(2) + 1
	if v != 4 {
		return v
	}
	v = 4 + r.bits(2)
	if v != 7 {
		return v
	}
	return r.bits(11)
}

// Unpack decodes n bytes of packed data starting at src[pos].
func Unpack(src []byte, pos, n int) ([]byte, error) {
	r := &bitReader{src: src, pos: pos}
	out := make([]byte, 0, n)
	var win [windowSize]byte
	wp := 0
	literal := true
	remain, offset := 0, 0

	for len(out) < n {
		left := n - len(out)
		if literal {
			if remain == 0 {
				remain = r.code()
			}
			c := min(remain, left)
			if remain <= left {
				literal = false
			}
			if r.pos+c > len(src) {
				return out, ErrTruncated
			}
			for _, b := range src[r.pos : r.pos+c] {
				out = append(out, b)
				win[wp] = b
				wp = (wp + 1) & (windowSize - 1)
			}
			r.pos += c
			remain -= c
		} else {
			if remain == 0 {
				offset = r.bits(10)
				if ln := r.code() + 2; ln <= 0x3F {
					remain = ln
				}
			}
			c := min(remain, left)
			if remain <= left {
				literal = true
			}
			s := (wp - offset) & (windowSize - 1)
			for range c {
				b := win[s]
				out = append(out, b)
				win[wp] = b
				s = (s + 1) & (windowSize - 1)
				wp = (wp + 1) & (windowSize - 1)
			}
			remain -= c
		}
		if r.eof {
			return out, ErrTruncated
		}
	}
	return out, nil
}
