package systems.pkt.lingon.terminal

import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.TextMeasurer
import androidx.compose.ui.unit.IntSize

fun measureCell(textMeasurer: TextMeasurer, style: TextStyle): IntSize {
    val result = textMeasurer.measure(AnnotatedString("M"), style)
    return result.size
}
