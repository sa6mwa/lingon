package systems.pkt.lingon.terminal

import androidx.compose.ui.graphics.Color
import org.junit.Assert.assertEquals
import org.junit.Test

class TerminalPaletteTest {
    private val palette = TerminalPalette(
        defaultFg = Color.White,
        defaultBg = Color.Black,
    )

    @Test
    fun defaultAnsiPaletteUsesBrighterReadableBaseColors() {
        assertEquals(Color(0xFFCC6666), DefaultAnsiPalette.ansi16[1])
        assertEquals(Color(0xFF7FB069), DefaultAnsiPalette.ansi16[2])
        assertEquals(Color(0xFFD7BA7D), DefaultAnsiPalette.ansi16[3])
        assertEquals(Color(0xFF78B4FF), DefaultAnsiPalette.ansi16[4])
        assertEquals(Color(0xFFC586C0), DefaultAnsiPalette.ansi16[5])
        assertEquals(Color(0xFF5FB3B3), DefaultAnsiPalette.ansi16[6])
    }

    @Test
    fun resolveColorUsesReadableAnsiBlue() {
        assertEquals(Color(0xFF78B4FF), resolveColor(COLOR_INDEXED or 4, palette, isForeground = true))
    }

    @Test
    fun defaultAnsiPaletteUsesReadableBrightBlue() {
        assertEquals(Color(0xFFA0CDFF), DefaultAnsiPalette.ansi16[12])
    }

    @Test
    fun resolveColorKeepsTruecolorUnchanged() {
        val encoded = COLOR_TRUE or 0x123456

        assertEquals(Color(0xFF123456), resolveColor(encoded, palette, isForeground = true))
    }
}
