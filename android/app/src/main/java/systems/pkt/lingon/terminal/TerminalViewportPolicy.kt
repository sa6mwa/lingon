package systems.pkt.lingon.terminal

import systems.pkt.lingon.DefaultTerminalZoom

internal object TerminalViewportPolicy {
    private const val zoomEpsilon = 0.001f

    fun shouldAutoFollowCursor(
        imeVisible: Boolean,
        fitToViewWidth: Boolean,
        zoomFactor: Float,
        panOffsetCols: Int,
        panOffsetRows: Int,
        totalRows: Int,
        visibleRows: Int,
    ): Boolean {
        if (!imeVisible || !fitToViewWidth) return false
        if (zoomFactor > DefaultTerminalZoom + zoomEpsilon) return false
        if (panOffsetCols != 0 || panOffsetRows != 0) return false
        if (totalRows <= 0 || visibleRows <= 0) return false
        return visibleRows < totalRows
    }

    fun autoFollowStartRow(
        cursorY: Int,
        totalRows: Int,
        visibleRows: Int,
    ): Int {
        if (totalRows <= 0 || visibleRows <= 0 || visibleRows >= totalRows) return 0
        val maxOffset = totalRows - visibleRows
        val clampedCursor = cursorY.coerceIn(0, totalRows - 1)
        if (clampedCursor >= totalRows - 1) {
            return maxOffset
        }
        val centered = clampedCursor - (visibleRows / 2)
        return centered.coerceIn(0, maxOffset)
    }
}
