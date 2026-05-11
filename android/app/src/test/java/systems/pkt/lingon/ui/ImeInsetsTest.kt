package systems.pkt.lingon.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertEquals
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

    @Test
    fun fallbackPaddingIsZeroWhenComposeReportsImeInset() {
        assertEquals(
            0,
            terminalImeFallbackPaddingPx(
                imeBottomPx = 640,
                navigationBarsBottomPx = 96,
                visibleFrameBottomOcclusionPx = 640,
            ),
        )
    }

    @Test
    fun fallbackPaddingSubtractsNavigationBarWhenImeInsetIsMissing() {
        assertEquals(
            544,
            terminalImeFallbackPaddingPx(
                imeBottomPx = 0,
                navigationBarsBottomPx = 96,
                visibleFrameBottomOcclusionPx = 640,
            ),
        )
    }

    @Test
    fun fallbackPaddingIgnoresNavigationOnlyOcclusion() {
        assertEquals(
            0,
            terminalImeFallbackPaddingPx(
                imeBottomPx = 0,
                navigationBarsBottomPx = 96,
                visibleFrameBottomOcclusionPx = 96,
            ),
        )
    }

    @Test
    fun effectiveImeBottomUsesFallbackWhenComposeInsetIsMissing() {
        assertEquals(
            640,
            effectiveTerminalImeBottomPx(
                imeBottomPx = 0,
                navigationBarsBottomPx = 96,
                fallbackPaddingPx = 544,
            ),
        )
    }
}
