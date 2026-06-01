package systems.pkt.lingon.terminal

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
}
