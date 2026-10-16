package adf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const adfPath = "../../SWIV.adf"

func openDisk(t *testing.T) *Disk {
	t.Helper()
	d, err := Open(adfPath)
	if err != nil {
		t.Skipf("no disk image: %v", err)
	}
	return d
}

func TestDirectory(t *testing.T) {
	d := openDisk(t)
	if len(d.Entries) != 128 {
		t.Fatalf("entries = %d, want 128", len(d.Entries))
	}
	if d.Entries[0].Name != "AMDLS0.CAT" || d.Entries[1].Name != "AMPROG.OBJ" {
		t.Fatalf("catalog names wrong: %q %q", d.Entries[0].Name, d.Entries[1].Name)
	}
}

// Every file must unpack to its declared length.
func TestUnpackAll(t *testing.T) {
	d := openDisk(t)
	for i, e := range d.Entries {
		b, err := d.Read(i)
		if err != nil {
			t.Errorf("%d %s: %v", i, e.Name, err)
			continue
		}
		if len(b) != e.Unpacked {
			t.Errorf("%d %s: got %d bytes, want %d", i, e.Name, len(b), e.Unpacked)
		}
	}
}

// Cross-check against the Python extractor's output when present.
func TestMatchesExtractedAssets(t *testing.T) {
	d := openDisk(t)
	dir := "../../assets"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("no assets/ directory")
	}
	for i, e := range d.Entries {
		want, err := os.ReadFile(filepath.Join(dir, e.Name))
		if err != nil {
			continue
		}
		got, err := d.Read(i)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s differs from Python extractor output", e.Name)
		}
	}
}
