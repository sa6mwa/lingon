package systems.pkt.lingon.terminal

enum class TerminalViewportMode {
    LiveBottom,
    Manual,
    CursorFollow,
}

data class TerminalViewportState(
    val cameraOffsetXPx: Float,
    val preferredCameraOffsetXPx: Float,
    val cameraOffsetYPx: Float,
    val scrollRemainderY: Float,
    val viewportHeightPx: Int,
    val scaledCellHeightPx: Float,
    val totalRows: Int,
    val mode: TerminalViewportMode = TerminalViewportMode.Manual,
)
