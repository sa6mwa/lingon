package systems.pkt.lingon.terminal

import androidx.compose.ui.graphics.Color

const val COLOR_DEFAULT = 0
const val COLOR_INDEXED = 1 shl 24
const val COLOR_TRUE = 2 shl 24
const val COLOR_INDEXED_256 = 3 shl 24
const val COLOR_FLAG_MASK = -0x1000000
const val COLOR_VALUE_MASK = 0x00ffffff

const val MODE_BOLD = 1 shl 0
const val MODE_FAINT = 1 shl 1
const val MODE_ITALIC = 1 shl 2
const val MODE_UNDERLINE = 1 shl 3
const val MODE_BLINK = 1 shl 4
const val MODE_INVERSE = 1 shl 5
const val MODE_HIDDEN = 1 shl 6

class TerminalPalette(
    val defaultFg: Color,
    val defaultBg: Color,
    val ansi16: List<Color> = DefaultAnsiPalette.ansi16,
    val ansi256: List<Color> = DefaultAnsiPalette.ansi256,
    val cursor: Color = defaultFg,
)

object DefaultAnsiPalette {
    val ansi16: List<Color> = listOf(
        Color(0xFF000000),
        Color(0xFFCC6666),
        Color(0xFF7FB069),
        Color(0xFFD7BA7D),
        Color(0xFF78B4FF),
        Color(0xFFC586C0),
        Color(0xFF5FB3B3),
        Color(0xFFD4D4D4),
        Color(0xFF808080),
        Color(0xFFFF0000),
        Color(0xFF00FF00),
        Color(0xFFFFFF00),
        Color(0xFFA0CDFF),
        Color(0xFFFF00FF),
        Color(0xFF00FFFF),
        Color(0xFFFFFFFF),
    )

    val ansi256: List<Color> = build256()

    private fun build256(): List<Color> {
        val out = ArrayList<Color>(256)
        out.addAll(ansi16)
        val steps = intArrayOf(0, 95, 135, 175, 215, 255)
        for (r in steps) {
            for (g in steps) {
                for (b in steps) {
                    if (out.size >= 256) break
                    val argb = 0xFF000000L or (r.toLong() shl 16) or (g.toLong() shl 8) or b.toLong()
                    out.add(Color(argb))
                }
            }
        }
        var gray = 8
        while (out.size < 256) {
            val value = gray.coerceIn(0, 255)
            val argb = 0xFF000000L or (value.toLong() shl 16) or (value.toLong() shl 8) or value.toLong()
            out.add(Color(argb))
            gray += 10
        }
        return out
    }
}

fun resolveColor(encoded: Int, palette: TerminalPalette, isForeground: Boolean): Color {
    if (encoded == COLOR_DEFAULT) {
        return if (isForeground) palette.defaultFg else palette.defaultBg
    }
    val flag = encoded and COLOR_FLAG_MASK
    val raw = encoded and COLOR_VALUE_MASK
    return when (flag) {
        COLOR_INDEXED -> {
            val idx = raw.coerceIn(0, palette.ansi16.lastIndex)
            palette.ansi16[idx]
        }
        COLOR_INDEXED_256 -> {
            val idx = raw.coerceIn(0, palette.ansi256.lastIndex)
            palette.ansi256[idx]
        }
        COLOR_TRUE -> {
            val r = (raw shr 16) and 0xff
            val g = (raw shr 8) and 0xff
            val b = raw and 0xff
            val argb = 0xFF000000L or (r.toLong() shl 16) or (g.toLong() shl 8) or b.toLong()
            Color(argb)
        }
        else -> if (isForeground) palette.defaultFg else palette.defaultBg
    }
}

fun applyFaint(color: Color): Color = color.copy(alpha = color.alpha * 0.6f)
