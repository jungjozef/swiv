"""Native port of the Sales Curve 'Kernel V1.0' LZ unpacker (kernel code $B1C-$D3C).

Stream layout (bytes, MSB-first bit codes interleaved with literal bytes):
  token loop alternates  LITERAL RUN  ->  MATCH  ->  LITERAL RUN ...
  literal run : code() = n, then n raw bytes taken from the byte stream
  match       : 10 raw bits = offset, code()+2 = length (0 if > $3F)
  code()      : 0            -> 0
                1 xx         -> xx+1            (xx != 11)
                1 11 yy      -> 4+yy            (yy != 11)  [= 1<<2|yy]
                1 11 11 + 11 raw bits -> value
  window      : 1024 bytes, written position advances with every output byte
"""


class Unpacker:
    def __init__(self, data, pos=0):
        self.d = data
        self.p = pos
        self.cnt = 0     # d5: bits left - 1
        self.cur = 0     # d6: current byte
        self.win = bytearray(1024)
        self.wp = 0

    def bit(self):
        self.cnt -= 1
        if self.cnt < 0:
            self.cur = self.d[self.p]
            self.p += 1
            self.cnt = 7
        b = (self.cur >> 7) & 1
        self.cur = (self.cur << 1) & 0xFF
        return b

    def bits(self, n):
        v = 0
        for _ in range(n):
            v = (v << 1) | self.bit()
        return v

    def code(self):
        if not self.bit():
            return 0
        v = self.bits(2) + 1
        if v != 4:
            return v
        v = 4 + self.bits(2) - 0  # moveq #1 ; two bits => 4..7
        if v != 7:
            return v
        return self.bits(11)

    def unpack(self, n_out):
        out = bytearray()
        mode = 0
        remain = 0           # -$45c(a6): remaining in current run
        offset = 0
        try:
            self._loop(out, n_out, mode, remain, offset)
        except IndexError:   # ran off the end of the input: return what we have
            pass
        return bytes(out)

    def _loop(self, out, n_out, mode, remain, offset):
        while len(out) < n_out:
            left = n_out - len(out)
            if mode == 0:
                if remain == 0:
                    remain = self.code()
                n = remain
                if n > left:
                    n = left
                else:
                    mode = 1
                chunk = self.d[self.p:self.p + n]
                self.p += n
                out += chunk
                for b in chunk:
                    self.win[self.wp] = b
                    self.wp = (self.wp + 1) & 0x3FF
            else:
                if remain == 0:
                    offset = self.bits(10)
                    ln = self.code() + 2
                    remain = ln if ln <= 0x3F else 0
                n = remain
                if n > left:
                    n = left
                else:
                    mode = 0
                src = (self.wp - offset) & 0x3FF
                for _ in range(n):
                    b = self.win[src]
                    out.append(b)
                    self.win[self.wp] = b
                    src = (src + 1) & 0x3FF
                    self.wp = (self.wp + 1) & 0x3FF
            remain -= n


if __name__ == '__main__':
    import sys
    adf = open(sys.argv[1] if len(sys.argv) > 1 else 'SWIV.adf', 'rb').read()
    u = Unpacker(adf, 0x1600)
    out = u.unpack(int(sys.argv[2]) if len(sys.argv) > 2 else 0x400)
    print(out[:0x100].hex())
    print(bytes(c if 32 <= c < 127 else 46 for c in out[:0x200]).decode())
