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
    fun singleCodePointWideGlyphsUseHostContinuationEncoding() {
        val runes = intArrayOf('日'.code, 0, '本'.code, 0, ' '.code, 0x1f642, 0, ' '.code, 'o'.code, 'k'.code)
        for (graphemes in listOf(null, Array(runes.size) { "" })) {
            val snapshot = TerminalSnapshot(runes.size, 1, runes, IntArray(runes.size), IntArray(runes.size), IntArray(runes.size), graphemes, 0, 0, false, 0, "")
            val selection = TerminalSelection(TerminalCell(0, 0), TerminalCell(0, 9))
            assertEquals("日本 🙂 ok", selection.extractText(snapshot))
            assertEquals("日本 🙂 ok", TerminalSelection(selection.focus, selection.anchor).extractText(snapshot))
            assertEquals("日", TerminalSelection(TerminalCell(0, 0), TerminalCell(0, 0)).extractText(snapshot))
        }
    }

    @Test
    fun continuationRecognitionPreservesSpacesGraphemesAndRowBoundaries() {
        val runes = intArrayOf('e'.code, ' '.code, 0x1f469, 0, 'x'.code, 0, 'A'.code, 0, 'B'.code, ' '.code)
        val graphemes = arrayOf("e\u0301", "", "👩‍💻", "", "", "", "", "", "", "")
        val snapshot = TerminalSnapshot(5, 2, runes, IntArray(runes.size), IntArray(runes.size), IntArray(runes.size), graphemes, 0, 0, false, 0, "")
        snapshot.fg[7] = 1 // A zero cell with different attributes is not a continuation.
        assertEquals("e\u0301 👩‍💻x\n A B", TerminalSelection(TerminalCell(0, 0), TerminalCell(1, 4)).extractText(snapshot))
        snapshot.modes[2] = MODE_HIDDEN
        snapshot.modes[3] = MODE_HIDDEN
        assertEquals("e\u0301   x", TerminalSelection(TerminalCell(0, 0), TerminalCell(0, 4)).extractText(snapshot))
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
