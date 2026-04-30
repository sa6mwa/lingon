package systems.pkt.lingon.terminal

import org.junit.Assert.assertEquals
import org.junit.Test
import systems.pkt.lingon.protocol.ScrollbackRow

class ScrollbackSnapshotTest {
    @Test
    fun `scrollback snapshot prepends requested history to full live snapshot`() {
        val live = snapshot(
            cols = 3,
            rows = listOf("L01", "L02"),
        )
        val scrollback = listOf(
            row("S01"),
            row("S02"),
            row("S03"),
        )

        val display = buildScrollbackSnapshot(live, scrollback, offset = 2)

        assertEquals(3, display.cols)
        assertEquals(4, display.rows)
        assertEquals("S02", rowText(display, 0))
        assertEquals("S03", rowText(display, 1))
        assertEquals("L01", rowText(display, 2))
        assertEquals("L02", rowText(display, 3))
    }

    private fun snapshot(cols: Int, rows: List<String>): TerminalSnapshot {
        val runes = IntArray(cols * rows.size) { ' '.code }
        rows.forEachIndexed { row, text ->
            text.forEachIndexed { col, ch ->
                if (col < cols) {
                    runes[row * cols + col] = ch.code
                }
            }
        }
        return TerminalSnapshot(
            cols = cols,
            rows = rows.size,
            runes = runes,
            modes = IntArray(runes.size),
            fg = IntArray(runes.size),
            bg = IntArray(runes.size),
            graphemes = null,
            cursorX = 0,
            cursorY = rows.lastIndex,
            cursorVisible = true,
            mode = 0,
            title = "",
        )
    }

    private fun row(text: String): ScrollbackRow {
        val builder = ScrollbackRow.newBuilder()
        text.forEach { ch ->
            builder.addRunes(ch.code)
            builder.addModes(0)
            builder.addFg(0)
            builder.addBg(0)
        }
        return builder.build()
    }

    private fun rowText(snapshot: TerminalSnapshot, row: Int): String {
        val start = row * snapshot.cols
        return buildString {
            for (col in 0 until snapshot.cols) {
                appendCodePoint(snapshot.runes[start + col])
            }
        }
    }
}
