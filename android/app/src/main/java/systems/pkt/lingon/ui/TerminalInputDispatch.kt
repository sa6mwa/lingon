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

internal enum class FnModifierMode(val label: String, val ctrlActive: Boolean, val altActive: Boolean) {
    Off("CTRL", false, false),
    Ctrl("CTRL", true, false),
    Alt("ALT", false, true),
    CtrlAlt("C+A", true, true);

    fun next(): FnModifierMode = when (this) {
        Off -> Ctrl
        Ctrl -> Alt
        Alt -> CtrlAlt
        CtrlAlt -> Off
    }
}

internal fun functionKeyBytes(
    index: Int,
    ctrlActive: Boolean = false,
    altActive: Boolean = false,
    shiftActive: Boolean = false,
): ByteArray? {
    if (index !in 0..11) return null
    val modifier = cursorModifierParameter(ctrlActive, altActive, shiftActive)
    val sequence = if (index < 4) {
        val final = "PQRS"[index]
        if (modifier == 1) "\u001bO$final" else "\u001b[1;${modifier}$final"
    } else {
        val parameter = intArrayOf(15, 17, 18, 19, 20, 21, 23, 24)[index - 4]
        if (modifier == 1) "\u001b[${parameter}~" else "\u001b[${parameter};${modifier}~"
    }
    return sequence.encodeToByteArray()
}

internal fun fnFunctionKeyBytes(
    key: Char,
    shiftActive: Boolean = false,
    ctrlActive: Boolean = false,
    altActive: Boolean = false,
): ByteArray? {
    val index = "1234567890".indexOf(key)
    return functionKeyBytes(index, ctrlActive = ctrlActive, altActive = altActive, shiftActive = shiftActive)
}

internal sealed interface FnInput {
    data object Copy : FnInput
    data object Paste : FnInput
    data class FunctionKey(val bytes: ByteArray) : FnInput
    data class Literal(val text: String) : FnInput
}

internal fun dispatchFnKey(
    key: Char,
    shiftActive: Boolean = false,
    ctrlActive: Boolean = false,
    altActive: Boolean = false,
): FnInput = when (key.lowercaseChar()) {
    'c' -> FnInput.Copy
    'v' -> FnInput.Paste
    else -> fnFunctionKeyBytes(key, shiftActive, ctrlActive, altActive)?.let(FnInput::FunctionKey) ?: FnInput.Literal(key.toString())
}

// Keep literal runs together so UTF-16 surrogate pairs are encoded as one value.
// Flush before each shortcut to preserve its position relative to terminal input.
internal fun dispatchFnInput(
    payload: String,
    shiftActive: Boolean = false,
    ctrlActive: Boolean = false,
    altActive: Boolean = false,
): List<FnInput> = buildList {
    val literal = StringBuilder()
    fun flushLiteral() {
        if (literal.isNotEmpty()) {
            add(FnInput.Literal(literal.toString()))
            literal.setLength(0)
        }
    }
    for (key in payload) {
        if (key.lowercaseChar() in "cv" || key in '0'..'9') {
            flushLiteral()
            add(dispatchFnKey(key, shiftActive, ctrlActive, altActive))
        } else {
            literal.append(key)
        }
    }
    flushLiteral()
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

internal fun modifiedNavigationKeyBytes(
    input: ByteArray,
    ctrlActive: Boolean,
    altActive: Boolean,
    shiftActive: Boolean = false,
): ByteArray? {
    if (input.size !in 3..4 || input[0] != 0x1b.toByte() || input[1] != '['.code.toByte()) return null
    val final = input.last().toInt().toChar()
    val pageKey = input.size == 4 && input[2].toInt().toChar() in charArrayOf('5', '6') && final == '~'
    val cursorKey = input.size == 3 && final in charArrayOf('A', 'B', 'C', 'D', 'F', 'H')
    if (!pageKey && !cursorKey) return null
    val modifier = cursorModifierParameter(ctrlActive, altActive, shiftActive)
    if (modifier == 1) {
        return input
    }
    val keyParameter = if (pageKey) input[2].toInt().toChar() else '1'
    return "\u001b[$keyParameter;${modifier}${final}".encodeToByteArray()
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
    modifiedNavigationKeyBytes(input.encodeToByteArray(), ctrlActive, altActive)?.let { return it }
    val out = ByteArrayOutputStream()
    var index = 0
    while (index < input.length) {
        val codePoint = input.codePointAt(index)
        val length = Character.charCount(codePoint)
        val text = input.substring(index, index + length)
        index += length
        val ch = if (length == 1) text[0] else null
        if (ch == '\n' || ch == '\r') {
            if (altActive) out.write(0x1b)
            out.write('\r'.code)
            continue
        }
        if (ctrlActive && ch != null) {
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
        out.write(text.toByteArray(Charsets.UTF_8))
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
        return functionKeyBytes(functionIndex, ctrlActive, altActive, shiftActive)
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
    modifiedNavigationKeyBytes(direct, ctrlActive, altActive, shiftActive)?.let { return it }
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
