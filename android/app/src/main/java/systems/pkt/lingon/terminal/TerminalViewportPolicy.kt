package systems.pkt.lingon.terminal

import systems.pkt.lingon.DefaultTerminalZoom
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min

internal object TerminalViewportPolicy {
    private const val zoomEpsilon = 0.001f

    fun effectiveRenderScale(
        zoomFactor: Float,
        fitScale: Float,
    ): Float {
        if (fitScale <= 0f) return zoomFactor
        return fitScale * (zoomFactor / DefaultTerminalZoom)
    }

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

    fun autoFollowCursorCameraOffsetX(
        zoomFactor: Float,
        panActive: Boolean,
        scrollbackOffsetRows: Int,
        cameraOffsetXPx: Float,
        scaledCellWidthPx: Float,
        viewportWidthPx: Int,
        totalCols: Int,
        cursorX: Int,
    ): Float {
        if (zoomFactor <= DefaultTerminalZoom + zoomEpsilon) return cameraOffsetXPx
        if (panActive) return cameraOffsetXPx
        if (scrollbackOffsetRows > 0) return cameraOffsetXPx
        if (scaledCellWidthPx <= 0f || viewportWidthPx <= 0 || totalCols <= 0) return cameraOffsetXPx

        val maxOffsetXPx = max(0f, (totalCols * scaledCellWidthPx) - viewportWidthPx.toFloat())
        if (maxOffsetXPx <= 0f) return 0f

        val current = cameraOffsetXPx.coerceIn(0f, maxOffsetXPx)
        val clampedCursorX = cursorX.coerceIn(0, totalCols - 1)
        val cursorLeftPx = clampedCursorX * scaledCellWidthPx
        val cursorRightPx = cursorLeftPx + scaledCellWidthPx
        val marginPx = scaledCellWidthPx
        val viewportRightPx = current + viewportWidthPx

        val desired = when {
            cursorLeftPx < current + marginPx -> cursorLeftPx - marginPx
            cursorRightPx > viewportRightPx - marginPx -> cursorRightPx + marginPx - viewportWidthPx
            else -> current
        }
        return desired.coerceIn(0f, maxOffsetXPx)
    }

    fun scrollbackRowsToExitForLiveReentry(
        scrollbackOffsetRows: Int,
        cameraOffsetYPx: Float,
        scaledCellHeightPx: Float,
    ): Int {
        if (scrollbackOffsetRows <= 0) return 0
        if (cameraOffsetYPx <= 0f || scaledCellHeightPx <= 0f) return 0
        val topRow = floor(cameraOffsetYPx / scaledCellHeightPx).toInt().coerceAtLeast(0)
        if (topRow <= 0) return 0
        return min(topRow, scrollbackOffsetRows)
    }

    fun preserveBottomAnchorOnHeightChange(
        cameraOffsetYPx: Float,
        previousViewportHeightPx: Int,
        nextViewportHeightPx: Int,
        totalRows: Int,
        scaledCellHeightPx: Float,
    ): Float {
        if (previousViewportHeightPx <= 0 || nextViewportHeightPx <= 0) return cameraOffsetYPx
        if (previousViewportHeightPx == nextViewportHeightPx) return cameraOffsetYPx
        if (totalRows <= 0 || scaledCellHeightPx <= 0f) return cameraOffsetYPx

        val maxOffsetYPx = max(0f, (totalRows * scaledCellHeightPx) - nextViewportHeightPx.toFloat())
        return (cameraOffsetYPx + (previousViewportHeightPx - nextViewportHeightPx)).coerceIn(0f, maxOffsetYPx)
    }

    fun shouldSnapToLiveBottom(
        fitToViewWidth: Boolean,
        zoomFactor: Float,
        scrollbackOffsetRows: Int,
    ): Boolean {
        if (!fitToViewWidth) return false
        if (zoomFactor > DefaultTerminalZoom + zoomEpsilon) return false
        if (scrollbackOffsetRows > 0) return false
        return true
    }

    fun bottomAlignedCameraOffsetY(
        totalRows: Int,
        scaledCellHeightPx: Float,
        viewportHeightPx: Int,
    ): Float {
        if (totalRows <= 0 || scaledCellHeightPx <= 0f || viewportHeightPx <= 0) return 0f
        return max(0f, (totalRows * scaledCellHeightPx) - viewportHeightPx.toFloat())
    }
}
