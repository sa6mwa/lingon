package systems.pkt.lingon.ui

import android.view.KeyEvent as AndroidKeyEvent
import java.io.ByteArrayOutputStream

internal data class SoftInputDispatch(
    val text: String? = null,
    val bytes: ByteArray? = null,
    val nextCtrlActive: Boolean,
    val nextAltActive: Boolean,
)

internal fun dispatchSoftInput(
    payload: String,
    ctrlActive: Boolean,
    altActive: Boolean,
): SoftInputDispatch {
    if (payload.isEmpty()) {
        return SoftInputDispatch(nextCtrlActive = ctrlActive, nextAltActive = altActive)
    }
    if (!ctrlActive && !altActive && payload.indexOfAny(charArrayOf('\n', '\r')) < 0) {
        return SoftInputDispatch(text = payload, nextCtrlActive = false, nextAltActive = false)
    }
    val bytes = buildModifiedBytes(payload, ctrlActive, altActive)
    val nextCtrlActive = if (ctrlActive && bytes.isNotEmpty()) {
        false
    } else {
        ctrlActive
    }
    val nextAltActive = if (altActive && bytes.isNotEmpty()) {
        false
    } else {
        altActive
    }
    return SoftInputDispatch(
        bytes = bytes,
        nextCtrlActive = nextCtrlActive,
        nextAltActive = nextAltActive,
    )
}

internal const val SNAPSHOT_MODE_APP_CURSOR = 1 shl 4

internal fun buildModifiedBytes(input: String, ctrlActive: Boolean, altActive: Boolean): ByteArray {
    val out = ByteArrayOutputStream()
    for (ch in input) {
        if (ch == '\n' || ch == '\r') {
            // Always emit a plain terminal carriage return for Enter.
            out.write('\r'.code)
            continue
        }
        if (ctrlActive) {
            val lower = ch.lowercaseChar()
            val ctrlByte = when {
                lower in 'a'..'z' -> lower.code - 'a'.code + 1
                ch == ' ' -> 0
                else -> null
            }
            if (ctrlByte != null) {
                if (altActive) {
                    out.write(0x1b)
                }
                out.write(ctrlByte)
                continue
            }
        }
        if (altActive) {
            out.write(0x1b)
        }
        out.write(ch.toString().toByteArray(Charsets.UTF_8))
    }
    return out.toByteArray()
}

internal fun translateAppCursorKeys(input: ByteArray, appCursorActive: Boolean): ByteArray {
    if (!appCursorActive || input.isEmpty()) {
        return input
    }
    val out = ByteArrayOutputStream(input.size)
    var i = 0
    while (i < input.size) {
        if (
            input[i] == 0x1b.toByte() &&
            i + 2 < input.size &&
            input[i + 1] == '['.code.toByte()
        ) {
            when (input[i + 2].toInt().toChar()) {
                'A', 'B', 'C', 'D', 'F', 'H' -> {
                    out.write(0x1b)
                    out.write('O'.code)
                    out.write(input[i + 2].toInt())
                    i += 3
                    continue
                }
            }
        }
        out.write(input[i].toInt())
        i += 1
    }
    return out.toByteArray()
}

internal fun hardwareKeyBytes(keyCode: Int): ByteArray? {
    return when (keyCode) {
        AndroidKeyEvent.KEYCODE_ENTER -> byteArrayOf('\r'.code.toByte())
        AndroidKeyEvent.KEYCODE_DEL -> byteArrayOf(0x7f.toByte())
        AndroidKeyEvent.KEYCODE_TAB -> byteArrayOf('\t'.code.toByte())
        AndroidKeyEvent.KEYCODE_ESCAPE -> byteArrayOf(0x1b.toByte())
        AndroidKeyEvent.KEYCODE_DPAD_UP -> "\u001b[A".encodeToByteArray()
        AndroidKeyEvent.KEYCODE_DPAD_DOWN -> "\u001b[B".encodeToByteArray()
        AndroidKeyEvent.KEYCODE_DPAD_RIGHT -> "\u001b[C".encodeToByteArray()
        AndroidKeyEvent.KEYCODE_DPAD_LEFT -> "\u001b[D".encodeToByteArray()
        AndroidKeyEvent.KEYCODE_MOVE_HOME -> "\u001b[H".encodeToByteArray()
        AndroidKeyEvent.KEYCODE_MOVE_END -> "\u001b[F".encodeToByteArray()
        else -> null
    }
}
