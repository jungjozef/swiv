"""Extract all files from the SWIV (Sales Curve) ADF into a directory.

Disk layout, found by reverse-engineering the Sales Curve 'Kernel V1.0'
loader (open routine at $81A..$964 in the running kernel):

  $1600            u16 N                     number of files
  $1602            N x u32  packed_size[i]   bytes each file occupies on disk
  $1602 + 4N       file 0 ... file N-1 back to back; each file =
                       u32 unpacked_length   (raw)
                       LZ stream             (see unpack.py)

File 0 is the catalog (AMDLS0.CAT): newline separated names, index = file no.
The LZ window contents persist across reads inside the kernel but the packer
treats every file as starting from an empty window, so a fresh window is used.
"""
import os
import struct
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from unpack import Unpacker

BASE = 0x1600


def read_directory(adf):
    n = struct.unpack_from('>H', adf, BASE)[0]
    sizes = struct.unpack_from('>%dI' % n, adf, BASE + 2)
    return n, sizes


def extract_all(adf):
    n, sizes = read_directory(adf)
    off = BASE + 2 + 4 * n
    files = []
    for i in range(n):
        ulen = struct.unpack_from('>I', adf, off)[0]
        data = Unpacker(adf, off + 4).unpack(ulen)
        files.append((off, sizes[i], ulen, data))
        off += sizes[i]
    return files


def catalog_names(files):
    cat = files[0][3].decode('latin-1')
    return [s for s in cat.split('\n')]


if __name__ == '__main__':
    adf_path = sys.argv[1] if len(sys.argv) > 1 else 'SWIV.adf'
    out_dir = sys.argv[2] if len(sys.argv) > 2 else 'assets'
    adf = open(adf_path, 'rb').read()
    files = extract_all(adf)
    names = catalog_names(files)
    os.makedirs(out_dir, exist_ok=True)
    print('%d files, %d catalog names' % (len(files), len(names)))
    for i, (off, psize, ulen, data) in enumerate(files):
        name = names[i] if i < len(names) and names[i] else 'FILE%03d' % i
        short = len(data) != ulen
        print('%3d  disk=%06x  packed=%6d  unpacked=%6d  %-18s%s' %
              (i, off, psize, ulen, name, '  !! SHORT' if short else ''))
        with open(os.path.join(out_dir, name), 'wb') as f:
            f.write(data)
