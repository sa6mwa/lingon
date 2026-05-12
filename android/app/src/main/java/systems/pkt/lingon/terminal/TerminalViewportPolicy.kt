package systems.pkt.lingon.terminal

import systems.pkt.lingon.DefaultTerminalZoom
import kotlin.math.abs
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min

internal object TerminalViewportPolicy {
    fun effectiveRenderScale(
        zoomFactor: Float,
        fitScale: Float,
    ): Float {
        if (fitScale <= 0f) return zoomFactor
        return fitScale * (zoomFactor / DefaultTerminalZoom)
    }

    fun autoFollowCursorCameraOffsetY(
        cameraOffsetYPx: Float,
        scaledCellHeightPx: Float,
        viewportHeightPx: Int,
        totalRows: Int,
        cursorY: Int,
    ): Float {
        if (totalRows <= 0 || scaledCellHeightPx <= 0f || viewportHeightPx <= 0) return cameraOffsetYPx

        val maxOffsetYPx = max(0f, (totalRows * scaledCellHeightPx) - viewportHeightPx.toFloat())
        val current = cameraOffsetYPx.coerceIn(0f, maxOffsetYPx)
        val clampedCursorY = cursorY.coerceIn(0, totalRows - 1)
        val cursorTopPx = clampedCursorY * scaledCellHeightPx
        val cursorBottomPx = cursorTopPx + scaledCellHeightPx
        val viewportBottomPx = current + viewportHeightPx

        val desired = when {
            cursorTopPx < current -> cursorTopPx
            cursorBottomPx > viewportBottomPx -> cursorBottomPx - viewportHeightPx
            else -> current
        }
        return desired.coerceIn(0f, maxOffsetYPx)
    }

    fun autoFollowCursorCameraOffsetX(
        panActive: Boolean,
        scrollbackOffsetRows: Int,
        cameraOffsetXPx: Float,
        preferredCameraOffsetXPx: Float,
        scaledCellWidthPx: Float,
        viewportWidthPx: Int,
        totalCols: Int,
        cursorX: Int,
    ): Float {
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
        val preferred = preferredCameraOffsetXPx.coerceIn(0f, maxOffsetXPx)

        if (cursorLeftPx >= preferred && cursorRightPx <= preferred + viewportWidthPx) {
            return preferred
        }

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

    fun preserveBottomAnchorOnViewportChange(
        cameraOffsetYPx: Float,
        previousViewportHeightPx: Int,
        previousScaledCellHeightPx: Float,
        nextViewportHeightPx: Int,
        nextScaledCellHeightPx: Float,
        totalRows: Int,
    ): Float {
        if (
            previousViewportHeightPx <= 0 ||
            nextViewportHeightPx <= 0 ||
            previousScaledCellHeightPx <= 0f ||
            nextScaledCellHeightPx <= 0f ||
            totalRows <= 0
        ) {
            return cameraOffsetYPx
        }
        val previousBottomRows = (cameraOffsetYPx + previousViewportHeightPx.toFloat()) / previousScaledCellHeightPx
        val maxOffsetYPx = max(0f, (totalRows * nextScaledCellHeightPx) - nextViewportHeightPx.toFloat())
        return ((previousBottomRows * nextScaledCellHeightPx) - nextViewportHeightPx.toFloat())
            .coerceIn(0f, maxOffsetYPx)
    }

    fun bottomAlignedCameraOffsetY(
        totalRows: Int,
        scaledCellHeightPx: Float,
        viewportHeightPx: Int,
    ): Float {
        if (totalRows <= 0 || scaledCellHeightPx <= 0f || viewportHeightPx <= 0) return 0f
        return max(0f, (totalRows * scaledCellHeightPx) - viewportHeightPx.toFloat())
    }

    fun restoreCameraOffsetY(
        savedCameraOffsetYPx: Float,
        savedViewportHeightPx: Int,
        savedScaledCellHeightPx: Float,
        savedTotalRows: Int,
        nextViewportHeightPx: Int,
        nextScaledCellHeightPx: Float,
        nextTotalRows: Int,
    ): Float {
        if (
            savedTotalRows <= 0 ||
            nextTotalRows <= 0 ||
            savedViewportHeightPx <= 0 ||
            nextViewportHeightPx <= 0 ||
            savedScaledCellHeightPx <= 0f ||
            nextScaledCellHeightPx <= 0f
        ) {
            return savedCameraOffsetYPx
        }
        val savedBottom = bottomAlignedCameraOffsetY(
            totalRows = savedTotalRows,
            scaledCellHeightPx = savedScaledCellHeightPx,
            viewportHeightPx = savedViewportHeightPx,
        )
        val tolerance = max(1f, savedScaledCellHeightPx * 0.1f)
        if (abs(savedCameraOffsetYPx - savedBottom) > tolerance) {
            val savedContentBottomRows =
                (savedCameraOffsetYPx + savedViewportHeightPx.toFloat()) / savedScaledCellHeightPx
            val nextMaxOffsetYPx = max(0f, (nextTotalRows * nextScaledCellHeightPx) - nextViewportHeightPx.toFloat())
            return ((savedContentBottomRows * nextScaledCellHeightPx) - nextViewportHeightPx.toFloat())
                .coerceIn(0f, nextMaxOffsetYPx)
        }
        return bottomAlignedCameraOffsetY(
            totalRows = nextTotalRows,
            scaledCellHeightPx = nextScaledCellHeightPx,
            viewportHeightPx = nextViewportHeightPx,
        )
    }
}
