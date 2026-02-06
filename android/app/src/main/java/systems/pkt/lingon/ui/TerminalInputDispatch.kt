package systems.pkt.lingon.ui

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
