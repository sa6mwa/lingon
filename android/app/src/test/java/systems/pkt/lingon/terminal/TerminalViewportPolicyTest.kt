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
    fun `vertical cursor follow preserves camera when cursor already fits`() {
        assertEquals(
            0f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetY(
                cameraOffsetYPx = 0f,
                scaledCellHeightPx = 10f,
                viewportHeightPx = 200,
                totalRows = 30,
                cursorY = 18,
            ),
            0.001f,
        )
        assertEquals(
            100f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetY(
                cameraOffsetYPx = 0f,
                scaledCellHeightPx = 10f,
                viewportHeightPx = 200,
                totalRows = 30,
                cursorY = 29,
            ),
            0.001f,
        )
        assertEquals(
            80f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetY(
                cameraOffsetYPx = 100f,
                scaledCellHeightPx = 10f,
                viewportHeightPx = 200,
                totalRows = 30,
                cursorY = 8,
            ),
            0.001f,
        )
    }

    @Test
    fun `cursor follow pans horizontally to keep typing visible`() {
        assertEquals(
            0f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                panActive = false,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 0f,
                preferredCameraOffsetXPx = 0f,
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
                panActive = false,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 0f,
                preferredCameraOffsetXPx = 0f,
                scaledCellWidthPx = 10f,
                viewportWidthPx = 100,
                totalCols = 30,
                cursorX = 19,
            ),
            0.001f,
        )
        assertEquals(
            0f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                panActive = false,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 100f,
                preferredCameraOffsetXPx = 0f,
                scaledCellWidthPx = 10f,
                viewportWidthPx = 100,
                totalCols = 30,
                cursorX = 6,
            ),
            0.001f,
        )
    }

    @Test
    fun `horizontal cursor follow restores left edge when cursor fits from origin`() {
        assertEquals(
            0f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                panActive = false,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 180f,
                preferredCameraOffsetXPx = 0f,
                scaledCellWidthPx = 10f,
                viewportWidthPx = 100,
                totalCols = 30,
                cursorX = 6,
            ),
            0.001f,
        )
    }

    @Test
    fun `horizontal cursor follow restores user panned edge when cursor fits there`() {
        assertEquals(
            10f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                panActive = false,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 180f,
                preferredCameraOffsetXPx = 10f,
                scaledCellWidthPx = 10f,
                viewportWidthPx = 100,
                totalCols = 30,
                cursorX = 6,
            ),
            0.001f,
        )
    }

    @Test
    fun `horizontal cursor follow keeps temporary camera when cursor fits neither preference nor current edge`() {
        assertEquals(
            100f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                panActive = false,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 100f,
                preferredCameraOffsetXPx = 0f,
                scaledCellWidthPx = 10f,
                viewportWidthPx = 100,
                totalCols = 40,
                cursorX = 12,
            ),
            0.001f,
        )
    }

    @Test
    fun `horizontal cursor follow pans right from saved preference only when cursor does not fit`() {
        assertEquals(
            130f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                panActive = false,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 0f,
                preferredCameraOffsetXPx = 0f,
                scaledCellWidthPx = 10f,
                viewportWidthPx = 100,
                totalCols = 40,
                cursorX = 21,
            ),
            0.001f,
        )
    }

    @Test
    fun `horizontal cursor follow clamps stale preferred edge to terminal bounds`() {
        assertEquals(
            200f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                panActive = false,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 200f,
                preferredCameraOffsetXPx = 500f,
                scaledCellWidthPx = 10f,
                viewportWidthPx = 100,
                totalCols = 30,
                cursorX = 25,
            ),
            0.001f,
        )
    }

    @Test
    fun `horizontal cursor follow is disabled while panning or in scrollback`() {
        assertEquals(
            42f,
            TerminalViewportPolicy.autoFollowCursorCameraOffsetX(
                panActive = true,
                scrollbackOffsetRows = 0,
                cameraOffsetXPx = 42f,
                preferredCameraOffsetXPx = 0f,
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
                panActive = false,
                scrollbackOffsetRows = 3,
                cameraOffsetXPx = 42f,
                preferredCameraOffsetXPx = 0f,
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
                zoomFactor = DefaultTerminalZoom + 0.1f,
                panOffsetCols = 0,
                panOffsetRows = 0,
                totalRows = 100,
                visibleRows = 20,
                cursorY = 99,
            ),
        )
        assertFalse(
            TerminalViewportPolicy.shouldAutoFollowCursor(
                zoomFactor = DefaultTerminalZoom,
                panOffsetCols = 1,
                panOffsetRows = 0,
                totalRows = 100,
                visibleRows = 20,
                cursorY = 99,
            ),
        )
        assertFalse(
            TerminalViewportPolicy.shouldAutoFollowCursor(
                zoomFactor = DefaultTerminalZoom,
                panOffsetCols = 0,
                panOffsetRows = 1,
                totalRows = 100,
                visibleRows = 20,
                cursorY = 99,
            ),
        )
        assertFalse(
            TerminalViewportPolicy.shouldAutoFollowCursor(
                zoomFactor = DefaultTerminalZoom,
                panOffsetCols = 0,
                panOffsetRows = 0,
                totalRows = 20,
                visibleRows = 20,
                cursorY = 19,
            ),
        )
        assertFalse(
            TerminalViewportPolicy.shouldAutoFollowCursor(
                zoomFactor = DefaultTerminalZoom,
                panOffsetCols = 0,
                panOffsetRows = 0,
                totalRows = 100,
                visibleRows = 20,
                cursorY = 92,
            ),
        )
    }

    @Test
    fun `auto follow enabled in live camera mode at default zoom`() {
        assertTrue(
            TerminalViewportPolicy.shouldAutoFollowCursor(
                zoomFactor = DefaultTerminalZoom,
                panOffsetCols = 0,
                panOffsetRows = 0,
                totalRows = 100,
                visibleRows = 20,
                cursorY = 99,
            ),
        )
    }

    @Test
    fun `initial live camera bottom aligns when cursor fits bottom viewport`() {
        assertEquals(
            800f,
            TerminalViewportPolicy.initialLiveCameraOffsetY(
                scaledCellHeightPx = 10f,
                viewportHeightPx = 200,
                totalRows = 100,
                cursorVisible = true,
                cursorY = 88,
            ),
            0.001f,
        )
    }

    @Test
    fun `initial live camera bottom aligns even when cursor is above bottom viewport`() {
        assertEquals(
            800f,
            TerminalViewportPolicy.initialLiveCameraOffsetY(
                scaledCellHeightPx = 10f,
                viewportHeightPx = 200,
                totalRows = 100,
                cursorVisible = true,
                cursorY = 10,
            ),
            0.001f,
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

    @Test
    fun `viewport change preserves bottom anchor across cell scale changes`() {
        assertEquals(
            1700f,
            TerminalViewportPolicy.preserveBottomAnchorOnViewportChange(
                cameraOffsetYPx = 400f,
                previousViewportHeightPx = 600,
                previousScaledCellHeightPx = 10f,
                nextViewportHeightPx = 300,
                nextScaledCellHeightPx = 20f,
                totalRows = 100,
            ),
            0.001f,
        )
    }

    @Test
    fun `restore preserves live bottom when row count advanced after capture`() {
        assertEquals(
            410f,
            TerminalViewportPolicy.restoreCameraOffsetY(
                savedCameraOffsetYPx = 400f,
                savedViewportHeightPx = 200,
                savedScaledCellHeightPx = 10f,
                savedTotalRows = 60,
                nextViewportHeightPx = 200,
                nextScaledCellHeightPx = 10f,
                nextTotalRows = 61,
            ),
            0.001f,
        )
    }

    @Test
    fun `restore preserves manual camera when row count advanced after capture`() {
        assertEquals(
            350f,
            TerminalViewportPolicy.restoreCameraOffsetY(
                savedCameraOffsetYPx = 350f,
                savedViewportHeightPx = 200,
                savedScaledCellHeightPx = 10f,
                savedTotalRows = 60,
                nextViewportHeightPx = 200,
                nextScaledCellHeightPx = 10f,
                nextTotalRows = 61,
            ),
            0.001f,
        )
    }

    @Test
    fun `restore preserves manual bottom anchor when viewport shrinks`() {
        assertEquals(
            400f,
            TerminalViewportPolicy.restoreCameraOffsetY(
                savedCameraOffsetYPx = 350f,
                savedViewportHeightPx = 200,
                savedScaledCellHeightPx = 10f,
                savedTotalRows = 60,
                nextViewportHeightPx = 150,
                nextScaledCellHeightPx = 10f,
                nextTotalRows = 60,
            ),
            0.001f,
        )
    }

    @Test
    fun `restore preserves manual bottom anchor when viewport grows`() {
        assertEquals(
            300f,
            TerminalViewportPolicy.restoreCameraOffsetY(
                savedCameraOffsetYPx = 350f,
                savedViewportHeightPx = 200,
                savedScaledCellHeightPx = 10f,
                savedTotalRows = 60,
                nextViewportHeightPx = 250,
                nextScaledCellHeightPx = 10f,
                nextTotalRows = 60,
            ),
            0.001f,
        )
    }

    @Test
    fun `restore clamps manual bottom anchor to current terminal bounds`() {
        assertEquals(
            200f,
            TerminalViewportPolicy.restoreCameraOffsetY(
                savedCameraOffsetYPx = 350f,
                savedViewportHeightPx = 200,
                savedScaledCellHeightPx = 10f,
                savedTotalRows = 60,
                nextViewportHeightPx = 300,
                nextScaledCellHeightPx = 10f,
                nextTotalRows = 50,
            ),
            0.001f,
        )
    }

    @Test
    fun `restore preserves manual bottom content when cell height changes`() {
        assertEquals(
            850f,
            TerminalViewportPolicy.restoreCameraOffsetY(
                savedCameraOffsetYPx = 350f,
                savedViewportHeightPx = 200,
                savedScaledCellHeightPx = 10f,
                savedTotalRows = 60,
                nextViewportHeightPx = 250,
                nextScaledCellHeightPx = 20f,
                nextTotalRows = 60,
            ),
            0.001f,
        )
    }
}
