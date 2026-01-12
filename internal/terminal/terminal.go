package terminal

import "fmt"

// Emulator provides access to an authoritative terminal emulator.
type Emulator interface {
	Write(p []byte) error
	Resize(cols, rows int)
	Snapshot() (Snapshot, error)
}

// Cursor represents a cursor position.
type Cursor struct {
	X int
	Y int
}

// Cell represents a terminal cell's content and attributes.
type Cell struct {
	Rune rune
	Mode int16
	FG   uint32
	BG   uint32
}

// Snapshot captures terminal state for resync.
type Snapshot struct {
	Cols          int
	Rows          int
	Cursor        Cursor
	CursorVisible bool
	Mode          uint32
	Title         string
	Cells         []Cell
}

// Cell mode flags used in snapshots.
const (
	ModeBold      int16 = 1 << 0
	ModeFaint     int16 = 1 << 1
	ModeItalic    int16 = 1 << 2
	ModeUnderline int16 = 1 << 3
	ModeBlink     int16 = 1 << 4
	ModeInverse   int16 = 1 << 5
	ModeHidden    int16 = 1 << 6
)

// Color encoding flags for snapshot cells.
const (
	ColorDefault   uint32 = 0
	ColorIndexed   uint32 = 1 << 24
	ColorTrue      uint32 = 2 << 24
	ColorFlagMask  uint32 = 0xff000000
	ColorValueMask uint32 = 0x00ffffff
)

// CellAt returns the cell at (x, y).
func (s Snapshot) CellAt(x, y int) (Cell, error) {
	if x < 0 || y < 0 || x >= s.Cols || y >= s.Rows {
		return Cell{}, fmt.Errorf("cell out of range")
	}
	idx := y*s.Cols + x
	return s.Cells[idx], nil
}
