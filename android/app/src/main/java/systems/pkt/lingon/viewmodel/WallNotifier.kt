package systems.pkt.lingon.viewmodel

data class WallNotification(
    val eventId: Long = 0,
    val sender: String,
    val message: String,
)

interface WallNotifier {
    fun notifyWall(notification: WallNotification)
}

object NoopWallNotifier : WallNotifier {
    override fun notifyWall(notification: WallNotification) {}
}
