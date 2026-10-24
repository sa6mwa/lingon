package systems.pkt.lingon.terminal

internal data class TerminalCell(val row: Int, val col: Int) : Comparable<TerminalCell> {
    override fun compareTo(other: TerminalCell): Int = compareValuesBy(this, other, TerminalCell::row, TerminalCell::col)
}

internal data class TerminalSelection(val anchor: TerminalCell, val focus: TerminalCell) {
    val start: TerminalCell get() = minOf(anchor, focus)
    val end: TerminalCell get() = maxOf(anchor, focus)

    fun contains(row: Int, col: Int): Boolean = TerminalCell(row, col) in start..end

    fun extractText(snapshot: TerminalSnapshot): String {
        if (start.row < 0 || end.row >= snapshot.rows || start.col < 0 || end.col >= snapshot.cols) return ""
        return (start.row..end.row).joinToString("\n") { row ->
            val firstCol = if (row == start.row) start.col else 0
            val lastCol = if (row == end.row) end.col else snapshot.cols - 1
            buildString {
                for (col in firstCol..lastCol) {
                    val index = row * snapshot.cols + col
                    val grapheme = snapshot.graphemes?.getOrNull(index).orEmpty()
                    val rune = snapshot.runes.getOrElse(index) { 0 }
                    if (snapshot.modes.getOrElse(index) { 0 } and MODE_HIDDEN != 0) {
                        append(' ')
                    } else if (grapheme.isNotEmpty()) {
                        append(grapheme)
                    } else if (rune != 0) {
                        appendCodePoint(rune)
                    } else if (!snapshot.isWideContinuation(row, col)) {
                        append(' ')
                    }
                }
            }.trimEnd(' ')
        }
    }
}
