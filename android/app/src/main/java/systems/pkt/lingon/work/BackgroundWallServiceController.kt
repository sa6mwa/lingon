package systems.pkt.lingon.work

interface BackgroundWallServiceController {
    fun setEnabled(enabled: Boolean)
}

object NoopBackgroundWallServiceController : BackgroundWallServiceController {
    override fun setEnabled(enabled: Boolean) {
    }
}
