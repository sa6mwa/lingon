package systems.pkt.lingon.terminal

import org.junit.Assert.assertEquals
import org.junit.Test

class TerminalSelectionTest {
    @Test
    fun copySelectionUsesOrderedCellRangeAndTrimsRowPadding() {
        val snapshot = snapshot("alpha   ", "beta    ", "gamma   ")
        val selection = TerminalSelection(TerminalCell(2, 2), TerminalCell(0, 2))

        assertEquals("pha\nbeta\ngam", selection.extractText(snapshot))
        assertEquals("pha\nbeta\ngam", TerminalSelection(selection.focus, selection.anchor).extractText(snapshot))
    }

    @Test
    fun singleCellAndUnicodeGraphemeAreCopiedOnce() {
        val snapshot = snapshot("abcd")
        snapshot.graphemes!![1] = "é"
        snapshot.runes[1] = 0
        snapshot.runes[2] = 0
        assertEquals("é", TerminalSelection(TerminalCell(0, 1), TerminalCell(0, 1)).extractText(snapshot))
        assertEquals("é", TerminalSelection(TerminalCell(0, 1), TerminalCell(0, 2)).extractText(snapshot))
    }

    @Test
    fun concealedCellsAreNotCopied() {
        val snapshot = snapshot("secret")
        snapshot.modes[1] = MODE_HIDDEN
        assertEquals("s cret", TerminalSelection(TerminalCell(0, 0), TerminalCell(0, 5)).extractText(snapshot))
    }

    private fun snapshot(vararg lines: String): TerminalSnapshot {
        val cols = lines.maxOf { it.length }
        val runes = IntArray(cols * lines.size) { ' '.code }
        lines.forEachIndexed { row, line ->
            line.forEachIndexed { col, char -> runes[row * cols + col] = char.code }
        }
        return TerminalSnapshot(cols, lines.size, runes, IntArray(runes.size), IntArray(runes.size), IntArray(runes.size), Array(runes.size) { "" }, 0, 0, false, 0, "")
    }
}
