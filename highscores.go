package main

import (
	"strings"

	"github.com/jungjozef/swiv/internal/adf"
)

// The table of "TODAY'S BEST SCORES" ($1232C): seven entries, kept sorted,
// filled at the start with the names of an HS*.TXT file and the scores
// $11170, $EA60, ... $2710 (in tens of points, like the score counter).
const (
	tableSize  = 7
	tableNames = "HS7.TXT" // the table the title loads first (trace of the boot)
)

var defaultScores = [tableSize]int{70000, 60000, 50000, 40000, 30000, 20000, 10000}

type scoreEntry struct {
	Name  string
	Score int
}

// HighScores is the table, ordered best first.
type HighScores struct {
	Entries [tableSize]scoreEntry
}

// NewHighScores reads the default names from the disk's text file (one name
// per line, "." ends the list).
func NewHighScores(d *adf.Disk) *HighScores {
	h := &HighScores{}
	var names []string
	if d != nil {
		if b, err := d.ReadName(tableNames); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				l = strings.TrimSpace(l)
				if l == "." {
					break
				}
				if l != "" {
					names = append(names, strings.ToUpper(l))
				}
			}
		}
	}
	for i := range h.Entries {
		h.Entries[i].Score = defaultScores[i]
		if i < len(names) {
			h.Entries[i].Name = names[i]
		}
	}
	return h
}

// Rank is the row a score would take (0 = best) or -1 when it does not beat
// the last entry ($12000: the new score must be above the lowest one).
func (h *HighScores) Rank(score int) int {
	if score <= h.Entries[tableSize-1].Score {
		return -1
	}
	for i, e := range h.Entries {
		if score > e.Score {
			return i
		}
	}
	return -1
}

// Insert puts a score at its row and drops the last entry, returning the row.
func (h *HighScores) Insert(name string, score int) int {
	r := h.Rank(score)
	if r < 0 {
		return -1
	}
	copy(h.Entries[r+1:], h.Entries[r:tableSize-1])
	h.Entries[r] = scoreEntry{name, score}
	return r
}
