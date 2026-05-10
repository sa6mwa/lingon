package systems.pkt.lingon.terminal

data class TerminalViewportState(
    val cameraOffsetXPx: Float,
    val cameraOffsetYPx: Float,
    val scrollRemainderY: Float,
    val viewportHeightPx: Int,
    val scaledCellHeightPx: Float,
    val totalRows: Int,
)
