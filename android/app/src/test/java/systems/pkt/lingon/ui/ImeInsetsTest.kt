package systems.pkt.lingon.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ImeInsetsTest {
    @Test
    fun zeroImeInsetIsNotVisible() {
        assertFalse(isTerminalImeVisible(imeBottomPx = 0, navigationBarsBottomPx = 0))
    }

    @Test
    fun imeInsetMatchingNavigationBarIsNotVisible() {
        assertFalse(isTerminalImeVisible(imeBottomPx = 96, navigationBarsBottomPx = 96))
    }

    @Test
    fun imeInsetLargerThanNavigationBarIsVisible() {
        assertTrue(isTerminalImeVisible(imeBottomPx = 640, navigationBarsBottomPx = 96))
    }
}
