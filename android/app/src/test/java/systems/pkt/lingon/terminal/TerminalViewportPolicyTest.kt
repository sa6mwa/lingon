package systems.pkt.lingon.terminal

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import systems.pkt.lingon.DefaultTerminalZoom

class TerminalViewportPolicyTest {
    @Test
    fun `auto follow start row centers cursor and clamps edges`() {
        assertEquals(0, TerminalViewportPolicy.autoFollowStartRow(cursorY = 0, totalRows = 100, visibleRows = 20))
        assertEquals(40, TerminalViewportPolicy.autoFollowStartRow(cursorY = 50, totalRows = 100, visibleRows = 20))
        assertEquals(80, TerminalViewportPolicy.autoFollowStartRow(cursorY = 99, totalRows = 100, visibleRows = 20))
        assertEquals(80, TerminalViewportPolicy.autoFollowStartRow(cursorY = 200, totalRows = 100, visibleRows = 20))
        assertEquals(0, TerminalViewportPolicy.autoFollowStartRow(cursorY = -5, totalRows = 100, visibleRows = 20))
    }

    @Test
    fun `auto follow disabled when not eligible`() {
        assertFalse(
            TerminalViewportPolicy.shouldAutoFollowCursor(
                imeVisible = false,
                fitToViewWidth = true,
                zoomFactor = DefaultTerminalZoom,
                panOffsetCols = 0,
                panOffsetRows = 0,
                totalRows = 100,
                visibleRows = 20,
            ),
        )
        assertFalse(
            TerminalViewportPolicy.shouldAutoFollowCursor(
                imeVisible = true,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panOffsetCols = 0,
                panOffsetRows = 0,
                totalRows = 100,
                visibleRows = 20,
            ),
        )
        assertFalse(
            TerminalViewportPolicy.shouldAutoFollowCursor(
                imeVisible = true,
                fitToViewWidth = true,
                zoomFactor = DefaultTerminalZoom + 0.1f,
                panOffsetCols = 0,
                panOffsetRows = 0,
                totalRows = 100,
                visibleRows = 20,
            ),
        )
        assertFalse(
            TerminalViewportPolicy.shouldAutoFollowCursor(
                imeVisible = true,
                fitToViewWidth = true,
                zoomFactor = DefaultTerminalZoom,
                panOffsetCols = 1,
                panOffsetRows = 0,
                totalRows = 100,
                visibleRows = 20,
            ),
        )
        assertFalse(
            TerminalViewportPolicy.shouldAutoFollowCursor(
                imeVisible = true,
                fitToViewWidth = true,
                zoomFactor = DefaultTerminalZoom,
                panOffsetCols = 0,
                panOffsetRows = 1,
                totalRows = 100,
                visibleRows = 20,
            ),
        )
        assertFalse(
            TerminalViewportPolicy.shouldAutoFollowCursor(
                imeVisible = true,
                fitToViewWidth = true,
                zoomFactor = DefaultTerminalZoom,
                panOffsetCols = 0,
                panOffsetRows = 0,
                totalRows = 20,
                visibleRows = 20,
            ),
        )
    }

    @Test
    fun `auto follow enabled in normal ime constrained mode`() {
        assertTrue(
            TerminalViewportPolicy.shouldAutoFollowCursor(
                imeVisible = true,
                fitToViewWidth = true,
                zoomFactor = DefaultTerminalZoom,
                panOffsetCols = 0,
                panOffsetRows = 0,
                totalRows = 100,
                visibleRows = 20,
            ),
        )
    }
}
