package systems.pkt.lingon.terminal

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TerminalLinksTest {
    @Test
    fun findHttpsLinksRecognizesHttpsOnly() {
        val links = TerminalLinks.findHttpsLinks("see http://plain.test and https://secure.test/path")

        assertEquals(listOf("https://secure.test/path"), links.map { it.url })
    }

    @Test
    fun findHttpsLinksStopsAtWhitespaceAndTrimsTerminalPunctuation() {
        val links = TerminalLinks.findHttpsLinks("open (https://example.test/path?q=1), then go")

        assertEquals(1, links.size)
        assertEquals("https://example.test/path?q=1", links.single().url)
    }

    @Test
    fun findHttpsLinksKeepsBalancedClosingPunctuation() {
        val links = TerminalLinks.findHttpsLinks("open https://example.test/a_(b)")

        assertEquals("https://example.test/a_(b)", links.single().url)
    }

    @Test
    fun findHttpsLinksMapsSnapshotCells() {
        val snapshot = snapshot(
            cols = 32,
            rows = listOf("open https://example.test now"),
        )

        val links = TerminalLinks.findHttpsLinks(snapshot)

        assertEquals(1, links.size)
        val link = links.single()
        assertEquals("https://example.test", link.url)
        assertEquals(0, link.startRow)
        assertEquals(5, link.startCol)
        assertEquals(0, link.endRow)
        assertEquals(25, link.endColExclusive)
        assertTrue(link.contains(row = 0, col = 5))
        assertTrue(link.contains(row = 0, col = 24))
    }

    @Test
    fun findHttpsLinksMapsSoftWrappedSnapshotCells() {
        val snapshot = snapshot(
            cols = 10,
            rows = listOf(
                "xx https:/",
                "/example.t",
                "est done",
            ),
        )

        val link = TerminalLinks.findHttpsLinks(snapshot).single()

        assertEquals("https://example.test", link.url)
        assertEquals(0, link.startRow)
        assertEquals(3, link.startCol)
        assertEquals(2, link.endRow)
        assertEquals(3, link.endColExclusive)
        assertTrue(link.contains(row = 1, col = 0))
        assertTrue(link.contains(row = 2, col = 2))
    }

    private fun snapshot(cols: Int, rows: List<String>): TerminalSnapshot {
        val runes = IntArray(cols * rows.size) { ' '.code }
        rows.forEachIndexed { row, text ->
            text.forEachIndexed { col, char ->
                if (col < cols) {
                    runes[row * cols + col] = char.code
                }
            }
        }
        return TerminalSnapshot(
            cols = cols,
            rows = rows.size,
            runes = runes,
            modes = IntArray(cols * rows.size),
            fg = IntArray(cols * rows.size),
            bg = IntArray(cols * rows.size),
            graphemes = null,
            cursorX = 0,
            cursorY = 0,
            cursorVisible = false,
            mode = 0,
            title = "",
        )
    }
}
