package systems.pkt.lingon.work

interface WallWorkScheduler {
    fun setEnabled(enabled: Boolean)
    fun resetCursor()
}

object NoopWallWorkScheduler : WallWorkScheduler {
    override fun setEnabled(enabled: Boolean) {
    }

    override fun resetCursor() {
    }
}
