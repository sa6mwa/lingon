package systems.pkt.lingon.ui

internal fun isTerminalImeVisible(
    imeBottomPx: Int,
    navigationBarsBottomPx: Int,
): Boolean {
    if (imeBottomPx <= 0) {
        return false
    }
    return imeBottomPx > navigationBarsBottomPx
}

internal fun terminalImeFallbackPaddingPx(
    imeBottomPx: Int,
    navigationBarsBottomPx: Int,
    visibleFrameBottomOcclusionPx: Int,
): Int {
    if (isTerminalImeVisible(imeBottomPx, navigationBarsBottomPx)) {
        return 0
    }
    return (visibleFrameBottomOcclusionPx - navigationBarsBottomPx).coerceAtLeast(0)
}

internal fun effectiveTerminalImeBottomPx(
    imeBottomPx: Int,
    navigationBarsBottomPx: Int,
    fallbackPaddingPx: Int,
): Int {
    if (fallbackPaddingPx <= 0) {
        return imeBottomPx
    }
    return maxOf(imeBottomPx, navigationBarsBottomPx + fallbackPaddingPx)
}
