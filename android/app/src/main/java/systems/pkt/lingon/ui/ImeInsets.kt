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
