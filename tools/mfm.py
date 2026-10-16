"""Standard AmigaDOS MFM track encoder (shared by the emulator harnesses)."""


def _mfm_longs(vals, prev):
    """MFM-encode 0x55555555-masked data longs; returns (longs, last_data_bit)."""
    out = []
    for d in vals:
        clk = ~(((d >> 1) | (prev << 31)) | (d << 1)) & 0xAAAAAAAA
        out.append((d | clk) & 0xFFFFFFFF)
        prev = d & 1
    return out, prev


def encode_track(trk_data, track_no):
    """Standard AmigaDOS MFM track (11 sectors) as a list of 32-bit longs."""
    out = []
    prev = 0

    def odd_even(longs):
        return [(x >> 1) & 0x55555555 for x in longs] + [x & 0x55555555 for x in longs]

    def emit(vals):
        nonlocal prev
        enc, prev = _mfm_longs(vals, prev)
        out.extend(enc)

    for sec in range(11):
        out.extend([0xAAAAAAAA])                 # gap
        out.append(0x44894489)                   # sync
        prev = 1
        info = (0xFF << 24) | (track_no << 16) | (sec << 8) | (11 - sec)
        hdr = odd_even([info])
        label = odd_even([0, 0, 0, 0])
        emit(hdr)
        emit(label)
        hsum = 0
        for x in hdr + label:
            hsum ^= x
        hsum &= 0x55555555
        emit([0, hsum & 0x55555555])             # header checksum (odd=0 even=x) approx
        data = trk_data[sec * 512:(sec + 1) * 512]
        words = [int.from_bytes(data[i:i + 4], 'big') for i in range(0, 512, 4)]
        dd = odd_even(words)
        dsum = 0
        for x in dd:
            dsum ^= x
        dsum &= 0x55555555
        emit([0, dsum])                           # data checksum
        emit(dd)
    out.extend([0xAAAAAAAA] * 40)
    return out
