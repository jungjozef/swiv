package adf

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

const dirOffset = 0x1600

// Entry describes one packed file on the disk.
type Entry struct {
	Name     string
	Offset   int // ADF offset of the unpacked-length field
	Packed   int // bytes the file occupies on disk
	Unpacked int
}

// Disk is a parsed SWIV disk image.
type Disk struct {
	data    []byte
	Entries []Entry
}

// Open reads and parses an ADF file.
func Open(path string) (*Disk, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse parses an in-memory ADF image.
func Parse(b []byte) (*Disk, error) {
	if len(b) < dirOffset+2 {
		return nil, fmt.Errorf("adf: image too small (%d bytes)", len(b))
	}
	n := int(binary.BigEndian.Uint16(b[dirOffset:]))
	off := dirOffset + 2 + 4*n
	if n == 0 || off > len(b) {
		return nil, fmt.Errorf("adf: bad directory (%d entries)", n)
	}
	d := &Disk{data: b, Entries: make([]Entry, n)}
	for i := range n {
		size := int(binary.BigEndian.Uint32(b[dirOffset+2+4*i:]))
		if off+4 > len(b) {
			return nil, fmt.Errorf("adf: file %d beyond end of image", i)
		}
		d.Entries[i] = Entry{
			Offset:   off,
			Packed:   size,
			Unpacked: int(binary.BigEndian.Uint32(b[off:])),
		}
		off += size
	}
	// File 0 is the catalog: newline separated names, index = file number.
	cat, err := d.Read(0)
	if err != nil {
		return nil, fmt.Errorf("adf: catalog: %w", err)
	}
	for i, name := range strings.Split(string(cat), "\n") {
		if i < n {
			d.Entries[i].Name = name
		}
	}
	return d, nil
}

// Read unpacks file i.
func (d *Disk) Read(i int) ([]byte, error) {
	if i < 0 || i >= len(d.Entries) {
		return nil, fmt.Errorf("adf: no file %d", i)
	}
	e := d.Entries[i]
	return Unpack(d.data, e.Offset+4, e.Unpacked)
}

// ReadName unpacks the file with the given catalog name.
func (d *Disk) ReadName(name string) ([]byte, error) {
	for i, e := range d.Entries {
		if strings.EqualFold(e.Name, name) {
			return d.Read(i)
		}
	}
	return nil, fmt.Errorf("adf: %q not found", name)
}
