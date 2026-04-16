package systems.pkt.lingon.terminal

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import systems.pkt.lingon.DefaultTerminalZoom

class TerminalViewportPolicyTest {
    @Test
    fun `effective render scale starts at fitted size and grows smoothly`() {
        assertEquals(
            0.6f,
            TerminalViewportPolicy.effectiveRenderScale(
                zoomFactor = DefaultTerminalZoom,
                fitScale = 0.6f,
            ),
            0.001f,
        )
        assertEquals(
            0.75f,
            TerminalViewportPolicy.effectiveRenderScale(
                zoomFactor = DefaultTerminalZoom + 0.25f,
                fitScale = 0.6f,
            ),
            0.001f,
        )
        assertEquals(
            1.0f,
            TerminalViewportPolicy.effectiveRenderScale(
                zoomFactor = DefaultTerminalZoom,
                fitScale = 1.0f,
            ),
            0.001f,
        )
        assertEquals(
            1.38f,
            TerminalViewportPolicy.effectiveRenderScale(
                zoomFactor = DefaultTerminalZoom + 0.15f,
                fitScale = 1.2f,
            ),
            0.001f,
        )
    }

    @Test
    fun `auto follow start row fills viewport before scrolling`() {
        assertEquals(0, TerminalViewportPolicy.autoFollowStartRow(cursorY = 0, totalRows = 100, visibleRows = 20))
        assertEquals(0, TerminalViewportPolicy.autoFollowStartRow(cursorY = 9, totalRows = 100, visibleRows = 20))
        assertEquals(0, TerminalViewportPolicy.autoFollowStartRow(cursorY = 19, totalRows = 100, visibleRows = 20))
        assertEquals(1, TerminalViewportPolicy.autoFollowStartRow(cursorY = 20, totalRows = 100, visibleRows = 20))
        assertEquals(31, TerminalViewportPolicy.autoFollowStartRow(cursorY = 50, totalRows = 100, visibleRows = 20))
        assertEquals(80, TerminalViewportPolicy.autoFollowStartRow(cursorY = 99, totalRows = 100, visibleRows = 20))
        assertEquals(80, TerminalViewportPolicy.autoFollowStartRow(cursorY = 200, totalRows = 100, visibleRows = 20))
        assertEquals(0, TerminalViewportPolicy.autoFollowStartRow(cursorY = -5, totalRows = 100, visibleRows = 20))
    }

    @Test
    fun `auto follow bottom anchor matches full bottom when cursor at prompt row`() {
        assertEquals(80, TerminalViewportPolicy.autoFollowStartRow(cursorY = 99, totalRows = 100, visibleRows = 20))
    }

    @Test
    fun `zoomed cursor follow pans horizontally to keep typing visible`() {
        assertEquals(
            0f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                zoomFactor = DefaultTerminalZoom + 0.5f,
                panActive = false,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 0f,
                scaledCellWidthPx = 10f,
                viewportWidthPx = 100,
                totalCols = 30,
                cursorX = 0,
            ),
            0.001f,
        )
        assertEquals(
            110f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                zoomFactor = DefaultTerminalZoom + 0.5f,
                panActive = false,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 0f,
                scaledCellWidthPx = 10f,
                viewportWidthPx = 100,
                totalCols = 30,
                cursorX = 19,
            ),
            0.001f,
        )
        assertEquals(
            50f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                zoomFactor = DefaultTerminalZoom + 0.5f,
                panActive = false,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 100f,
                scaledCellWidthPx = 10f,
                viewportWidthPx = 100,
                totalCols = 30,
                cursorX = 6,
            ),
            0.001f,
        )
    }

    @Test
    fun `horizontal cursor follow is disabled outside live zoomed typing mode`() {
        assertEquals(
            42f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                zoomFactor = DefaultTerminalZoom,
                panActive = false,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 42f,
                scaledCellWidthPx = 10f,
                viewportWidthPx = 100,
                totalCols = 30,
                cursorX = 25,
            ),
            0.001f,
        )
        assertEquals(
            42f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                zoomFactor = DefaultTerminalZoom + 0.5f,
                panActive = true,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 42f,
                scaledCellWidthPx = 10f,
                viewportWidthPx = 100,
                totalCols = 30,
                cursorX = 25,
            ),
            0.001f,
        )
        assertEquals(
            42f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                zoomFactor = DefaultTerminalZoom + 0.5f,
                panActive = false,
                scrollbackOffsetRows = 3,
                cameraOffsetXPx = 42f,
                scaledCellWidthPx = 10f,
                viewportWidthPx = 100,
                totalCols = 30,
                cursorX = 25,
            ),
            0.001f,
        )
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

    @Test
    fun `live reentry rows consume pan rows while preserving viewport continuity`() {
        assertEquals(
            0,
            TerminalViewportPolicy.scrollbackRowsToExitForLiveReentry(
                scrollbackOffsetRows = 4,
                cameraOffsetYPx = 0f,
                scaledCellHeightPx = 10f,
            ),
        )
        assertEquals(
            3,
            TerminalViewportPolicy.scrollbackRowsToExitForLiveReentry(
                scrollbackOffsetRows = 4,
                cameraOffsetYPx = 39.9f,
                scaledCellHeightPx = 10f,
            ),
        )
        assertEquals(
            4,
            TerminalViewportPolicy.scrollbackRowsToExitForLiveReentry(
                scrollbackOffsetRows = 4,
                cameraOffsetYPx = 40f,
                scaledCellHeightPx = 10f,
            ),
        )
        assertEquals(
            4,
            TerminalViewportPolicy.scrollbackRowsToExitForLiveReentry(
                scrollbackOffsetRows = 4,
                cameraOffsetYPx = 92f,
                scaledCellHeightPx = 10f,
            ),
        )
    }

    @Test
    fun `height change preserves viewport bottom anchor`() {
        assertEquals(
            300f,
            TerminalViewportPolicy.preserveBottomAnchorOnHeightChange(
                cameraOffsetYPx = 0f,
                previousViewportHeightPx = 1000,
                nextViewportHeightPx = 700,
                totalRows = 100,
                scaledCellHeightPx = 20f,
            ),
            0.001f,
        )
        assertEquals(
            0f,
            TerminalViewportPolicy.preserveBottomAnchorOnHeightChange(
                cameraOffsetYPx = 50f,
                previousViewportHeightPx = 700,
                nextViewportHeightPx = 1000,
                totalRows = 100,
                scaledCellHeightPx = 20f,
            ),
            0.001f,
        )
        assertEquals(
            900f,
            TerminalViewportPolicy.preserveBottomAnchorOnHeightChange(
                cameraOffsetYPx = 1200f,
                previousViewportHeightPx = 700,
                nextViewportHeightPx = 1000,
                totalRows = 100,
                scaledCellHeightPx = 20f,
            ),
            0.001f,
        )
    }
}
