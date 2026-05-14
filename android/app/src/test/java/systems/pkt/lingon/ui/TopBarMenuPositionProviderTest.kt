package systems.pkt.lingon.ui

import androidx.compose.ui.unit.IntRect
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.LayoutDirection
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TopBarMenuPositionProviderTest {
    @Test
    fun `positions menu below the top bar button instead of at the window top`() {
        val provider = TopBarMenuPositionProvider(
            verticalGapPx = 21,
            screenMarginPx = 21,
        )

        val position = provider.calculatePosition(
            anchorBounds = IntRect(left = 944, top = 80, right = 1049, bottom = 185),
            windowSize = IntSize(width = 1080, height = 2400),
            layoutDirection = LayoutDirection.Ltr,
            popupContentSize = IntSize(width = 735, height = 1050),
        )

        assertEquals(314, position.x)
        assertEquals(206, position.y)
        assertTrue(position.y > 185)
    }

    @Test
    fun `keeps menu attached to compact sidebar buttons`() {
        val provider = TopBarMenuPositionProvider(
            verticalGapPx = 8,
            screenMarginPx = 12,
        )

        val position = provider.calculatePosition(
            anchorBounds = IntRect(left = 12, top = 48, right = 40, bottom = 76),
            windowSize = IntSize(width = 2400, height = 1080),
            layoutDirection = LayoutDirection.Ltr,
            popupContentSize = IntSize(width = 640, height = 720),
        )

        assertEquals(12, position.x)
        assertEquals(84, position.y)
    }

    @Test
    fun `keeps menu inside the visible window when space below is limited`() {
        val provider = TopBarMenuPositionProvider(
            verticalGapPx = 12,
            screenMarginPx = 16,
        )

        val position = provider.calculatePosition(
            anchorBounds = IntRect(left = 700, top = 700, right = 780, bottom = 780),
            windowSize = IntSize(width = 800, height = 900),
            layoutDirection = LayoutDirection.Ltr,
            popupContentSize = IntSize(width = 360, height = 240),
        )

        assertEquals(420, position.x)
        assertEquals(644, position.y)
    }
}
