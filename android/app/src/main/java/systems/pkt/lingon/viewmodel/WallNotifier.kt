package systems.pkt.lingon.viewmodel

data class WallNotification(
    val endpoint: String = "",
    val eventId: Long = 0,
    val sender: String,
    val sourceSessionName: String = "",
    val message: String,
)

interface WallNotifier {
    fun notifyWall(notification: WallNotification): Boolean
}

object NoopWallNotifier : WallNotifier {
    override fun notifyWall(notification: WallNotification): Boolean = false
}
