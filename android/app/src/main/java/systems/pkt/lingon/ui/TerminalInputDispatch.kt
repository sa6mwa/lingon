package systems.pkt.lingon.ui

import android.view.KeyEvent as AndroidKeyEvent
import java.io.ByteArrayOutputStream

internal enum class AltButtonMode {
    Off, Alt, Fn;

    fun next(): AltButtonMode = when (this) {
        Off -> Alt
        Alt -> Fn
        Fn -> Off
    }
}

internal fun fnFunctionKeyBytes(key: Char): ByteArray? {
    val sequence = when (key) {
        '1' -> "\u001bOP"
        '2' -> "\u001bOQ"
        '3' -> "\u001bOR"
        '4' -> "\u001bOS"
        '5' -> "\u001b[15~"
        '6' -> "\u001b[17~"
        '7' -> "\u001b[18~"
        '8' -> "\u001b[19~"
        '9' -> "\u001b[20~"
        '0' -> "\u001b[21~"
        '@' -> "\u001b[23~"
        '#' -> "\u001b[24~"
        else -> return null
    }
    return sequence.encodeToByteArray()
}

internal sealed interface FnInput {
    data object ToggleSelection : FnInput
    data object Copy : FnInput
    data object Paste : FnInput
    data class FunctionKey(val bytes: ByteArray) : FnInput
    data class Literal(val text: String) : FnInput
}

internal fun dispatchFnKey(key: Char): FnInput = when (key.lowercaseChar()) {
    's' -> FnInput.ToggleSelection
    'c' -> FnInput.Copy
    'p' -> FnInput.Paste
    else -> fnFunctionKeyBytes(key)?.let(FnInput::FunctionKey) ?: FnInput.Literal(key.toString())
}

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

internal fun modifiedCursorKeyBytes(
    input: ByteArray,
    ctrlActive: Boolean,
    altActive: Boolean,
    shiftActive: Boolean = false,
): ByteArray? {
    if (
        input.size != 3 ||
        input[0] != 0x1b.toByte() ||
        input[1] != '['.code.toByte()
    ) {
        return null
    }
    val final = input[2].toInt().toChar()
    if (final !in charArrayOf('A', 'B', 'C', 'D', 'F', 'H')) {
        return null
    }
    val modifier = cursorModifierParameter(ctrlActive, altActive, shiftActive)
    if (modifier == 1) {
        return input
    }
    return "\u001b[1;${modifier}${final}".encodeToByteArray()
}

private fun cursorModifierParameter(
    ctrlActive: Boolean,
    altActive: Boolean,
    shiftActive: Boolean,
): Int {
    var modifier = 1
    if (shiftActive) modifier += 1
    if (altActive) modifier += 2
    if (ctrlActive) modifier += 4
    return modifier
}

internal fun buildModifiedBytes(input: String, ctrlActive: Boolean, altActive: Boolean): ByteArray {
    modifiedCursorKeyBytes(input.encodeToByteArray(), ctrlActive, altActive)?.let { return it }
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

internal fun hardwareKeyBytes(
    keyCode: Int,
    ctrlActive: Boolean = false,
    altActive: Boolean = false,
    shiftActive: Boolean = false,
): ByteArray? {
    val functionIndex = keyCode - AndroidKeyEvent.KEYCODE_F1
    if (functionIndex in 0..11) {
        val modifier = cursorModifierParameter(ctrlActive, altActive, shiftActive)
        if (modifier == 1) return fnFunctionKeyBytes("1234567890@#"[functionIndex])
        val sequence = if (functionIndex < 4) {
            "\u001b[1;${modifier}${"PQRS"[functionIndex]}"
        } else {
            val parameter = intArrayOf(15, 17, 18, 19, 20, 21, 23, 24)[functionIndex - 4]
            "\u001b[${parameter};${modifier}~"
        }
        return sequence.encodeToByteArray()
    }
    val direct = when (keyCode) {
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
    direct ?: return null
    modifiedCursorKeyBytes(direct, ctrlActive, altActive, shiftActive)?.let { return it }
    if (altActive) {
        return byteArrayOf(0x1b) + direct
    }
    return direct
}

internal fun shouldForwardImeDeleteSurroundingTextAsBackspace(
    leftLength: Int,
    rightLength: Int,
): Boolean {
    if (leftLength < 0 || rightLength < 0) {
        return false
    }
    return leftLength > 0 && rightLength == 0
}
