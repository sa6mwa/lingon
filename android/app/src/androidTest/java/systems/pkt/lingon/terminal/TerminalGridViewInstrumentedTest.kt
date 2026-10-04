package systems.pkt.lingon.terminal

import android.graphics.Bitmap
import android.graphics.Canvas
import android.os.SystemClock
import android.view.MotionEvent
import android.view.View
import androidx.compose.ui.graphics.Color
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import systems.pkt.lingon.DefaultTerminalZoom

@RunWith(AndroidJUnit4::class)
class TerminalGridViewInstrumentedTest {
    @Test
    fun wideRuneSelectionCopiesOriginalTextAndAnchorsContinuationTouches() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        instrumentation.runOnMainSync {
            val snapshot = terminalSnapshot(listOf("日\u0000本\u0000 \u0000\u0000 ok"), 10)
            snapshot.runes[5] = 0x1f642
            val view = TerminalGridView(instrumentation.targetContext).apply {
                measure(exactlyMeasureSpec(720), exactlyMeasureSpec(240))
                layout(0, 0, 720, 240)
                update(snapshot = snapshot, fontSizeSp = 14, minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1, hostCols = 10, hostRows = 1, fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom, panResetNonce = 0, scrollbackOffsetRows = 0,
                    imeVisible = false, isLoading = false)
                setSelectionMode(true)
            }
            fun select(firstCol: Int, lastCol: Int) {
                val cellWidth = view.getScaledCellWidthForTesting()
                val y = view.getScaledCellHeightForTesting() * 0.5f
                val time = SystemClock.uptimeMillis()
                view.dispatchTouchEvent(MotionEvent.obtain(time, time, MotionEvent.ACTION_DOWN, cellWidth * (firstCol + 0.5f), y, 0))
                view.dispatchTouchEvent(MotionEvent.obtain(time, time + 16, MotionEvent.ACTION_MOVE, cellWidth * (lastCol + 0.5f), y, 0))
                view.dispatchTouchEvent(MotionEvent.obtain(time, time + 32, MotionEvent.ACTION_UP, cellWidth * (lastCol + 0.5f), y, 0))
            }
            select(0, 9)
            assertEquals("日本 🙂 ok", view.selectedText())
            select(3, 6)
            assertEquals("本 🙂", view.selectedText())
            select(6, 3)
            assertEquals("本 🙂", view.selectedText())
        }
    }

    @Test
    fun selectionDragCopiesCellsWithWhiteHighlightAndTwoFingerPanPreservesSelection() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        instrumentation.runOnMainSync {
            val view = TerminalGridView(instrumentation.targetContext).apply {
                measure(exactlyMeasureSpec(200), exactlyMeasureSpec(100))
                layout(0, 0, 200, 100)
                update(
                    snapshot = terminalSnapshot(rows = listOf("hello world".padEnd(40)), cols = 40),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 40,
                    hostRows = 1,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                )
                setSelectionMode(true)
            }
            val cellWidth = view.getScaledCellWidthForTesting()
            val cellHeight = view.getScaledCellHeightForTesting()
            val downTime = SystemClock.uptimeMillis()
            view.dispatchTouchEvent(MotionEvent.obtain(downTime, downTime, MotionEvent.ACTION_DOWN, cellWidth * 1.5f, cellHeight * 0.5f, 0))
            view.dispatchTouchEvent(MotionEvent.obtain(downTime, downTime + 16, MotionEvent.ACTION_MOVE, cellWidth * 3.5f, cellHeight * 0.5f, 0))
            view.dispatchTouchEvent(MotionEvent.obtain(downTime, downTime + 32, MotionEvent.ACTION_UP, cellWidth * 3.5f, cellHeight * 0.5f, 0))
            assertEquals("ell", view.selectedText())

            val bitmap = Bitmap.createBitmap(200, 100, Bitmap.Config.ARGB_8888)
            view.draw(Canvas(bitmap))
            assertEquals(android.graphics.Color.WHITE, bitmap.getPixel((cellWidth * 2f + 1f).toInt(), 1))

            val initialCameraX = view.getCameraOffsetXForTesting()
            dispatchTwoFingerPan(view, 160f, 120f, 50f, downTime + 64)
            assertTrue(view.getCameraOffsetXForTesting() > initialCameraX)
            assertEquals("ell", view.selectedText())
        }
    }

    @Test
    fun httpsLinkTapOpensLinkWithoutFocusTapFallback() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        var openedUrl: String? = null
        var focused = false

        instrumentation.runOnMainSync {
            val view = TerminalGridView(instrumentation.targetContext).apply {
                setOnOpenLink { url ->
                    openedUrl = url
                    true
                }
                setOnTap {
                    focused = true
                }
                measure(exactlyMeasureSpec(720), exactlyMeasureSpec(240))
                layout(0, 0, 720, 240)
                update(
                    snapshot = terminalSnapshot(
                        rows = listOf("open https://example.test now"),
                        cols = 32,
                    ),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 32,
                    hostRows = 1,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                )
            }

            assertEquals("https://example.test", view.getLinkAtCellForTesting(row = 0, col = 5))
            val cellWidth = view.getScaledCellWidthForTesting()
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell width was not measured", cellWidth > 0f)
            assertTrue("terminal cell height was not measured", cellHeight > 0f)

            dispatchSinglePointerTap(view, x = cellWidth * 6.5f, y = cellHeight * 0.5f)

            assertEquals("https://example.test", openedUrl)
            assertFalse("link tap should not fall through to terminal focus", focused)
        }
    }

    @Test
    fun delimitedHttpsLinkTapOpensTrimmedLink() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        var openedUrl: String? = null

        instrumentation.runOnMainSync {
            val view = TerminalGridView(instrumentation.targetContext).apply {
                setOnOpenLink { url ->
                    openedUrl = url
                    true
                }
                measure(exactlyMeasureSpec(720), exactlyMeasureSpec(240))
                layout(0, 0, 720, 240)
                update(
                    snapshot = terminalSnapshot(
                        rows = listOf("open <https://example.test> now"),
                        cols = 33,
                    ),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 33,
                    hostRows = 1,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                )
            }

            assertEquals("https://example.test", view.getLinkAtCellForTesting(row = 0, col = 6))
            assertEquals(null, view.getLinkAtCellForTesting(row = 0, col = 26))
            val cellWidth = view.getScaledCellWidthForTesting()
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell width was not measured", cellWidth > 0f)
            assertTrue("terminal cell height was not measured", cellHeight > 0f)

            dispatchSinglePointerTap(view, x = cellWidth * 7.5f, y = cellHeight * 0.5f)

            assertEquals("https://example.test", openedUrl)
        }
    }

    @Test
    fun wideUnicodeLinkTapOnLastContinuationOpensFullUrl() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        instrumentation.runOnMainSync {
            val url = "https://example.test/日本"
            val prefix = "https://example.test/"
            var openedUrl: String? = null
            val view = TerminalGridView(instrumentation.targetContext).apply {
                setOnOpenLink { value ->
                    openedUrl = systems.pkt.lingon.ui.TerminalLinkOpener.createIntent(instrumentation.targetContext, value).dataString
                    true
                }
                measure(exactlyMeasureSpec(720), exactlyMeasureSpec(240))
                layout(0, 0, 720, 240)
                update(
                    snapshot = terminalSnapshot(listOf(prefix + "日\u0000本\u0000 now"), 40),
                    fontSizeSp = 14, minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1, hostCols = 40, hostRows = 1, fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom, panResetNonce = 0, scrollbackOffsetRows = 0,
                    imeVisible = false, isLoading = false,
                )
            }
            val lastContinuation = prefix.length + 3
            for (col in prefix.length..lastContinuation) assertEquals(url, view.getLinkAtCellForTesting(0, col))
            assertEquals(null, view.getLinkAtCellForTesting(0, lastContinuation + 1))
            dispatchSinglePointerTap(view, view.getScaledCellWidthForTesting() * (lastContinuation + 0.5f), view.getScaledCellHeightForTesting() * 0.5f)
            assertEquals(url, openedUrl)
        }
    }

    private fun exactlyMeasureSpec(size: Int): Int {
        return View.MeasureSpec.makeMeasureSpec(size, View.MeasureSpec.EXACTLY)
    }

    private fun terminalSnapshot(rows: List<String>, cols: Int): TerminalSnapshot {
        val runes = IntArray(rows.size * cols) { ' '.code }
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
            modes = IntArray(rows.size * cols),
            fg = IntArray(rows.size * cols),
            bg = IntArray(rows.size * cols),
            graphemes = null,
            cursorX = 0,
            cursorY = 0,
            cursorVisible = false,
            mode = 0,
            title = "",
        )
    }

    private fun dispatchSinglePointerTap(view: View, x: Float, y: Float) {
        val startTime = SystemClock.uptimeMillis()
        view.dispatchTouchEvent(MotionEvent.obtain(startTime, startTime, MotionEvent.ACTION_DOWN, x, y, 0))
        view.dispatchTouchEvent(MotionEvent.obtain(startTime, startTime + 32, MotionEvent.ACTION_UP, x, y, 0))
    }

    private fun dispatchTwoFingerPan(view: View, firstX: Float, secondX: Float, y: Float, time: Long) {
        view.dispatchTouchEvent(MotionEvent.obtain(time, time, MotionEvent.ACTION_DOWN, firstX, y, 0))
        fun multiEvent(action: Int, x1: Float, x2: Float, eventTime: Long): MotionEvent {
            val properties = arrayOf(
                MotionEvent.PointerProperties().apply { id = 0; toolType = MotionEvent.TOOL_TYPE_FINGER },
                MotionEvent.PointerProperties().apply { id = 1; toolType = MotionEvent.TOOL_TYPE_FINGER },
            )
            val coords = arrayOf(
                MotionEvent.PointerCoords().apply { x = x1; this.y = y; pressure = 1f; size = 1f },
                MotionEvent.PointerCoords().apply { x = x2; this.y = y; pressure = 1f; size = 1f },
            )
            return MotionEvent.obtain(time, eventTime, action, 2, properties, coords, 0, 0, 1f, 1f, 0, 0, 0, 0)
        }
        view.dispatchTouchEvent(multiEvent(MotionEvent.ACTION_POINTER_DOWN or (1 shl MotionEvent.ACTION_POINTER_INDEX_SHIFT), firstX, secondX, time + 8))
        view.dispatchTouchEvent(multiEvent(MotionEvent.ACTION_MOVE, firstX - 50f, secondX - 50f, time + 24))
        view.dispatchTouchEvent(multiEvent(MotionEvent.ACTION_POINTER_UP or (1 shl MotionEvent.ACTION_POINTER_INDEX_SHIFT), firstX - 50f, secondX - 50f, time + 32))
        view.dispatchTouchEvent(MotionEvent.obtain(time, time + 40, MotionEvent.ACTION_UP, firstX - 50f, y, 0))
    }
}
