package terminal

// ScrollbackRow captures one terminal row for scrollback history.
type ScrollbackRow struct {
	Cols  int
	Cells []Cell
}
