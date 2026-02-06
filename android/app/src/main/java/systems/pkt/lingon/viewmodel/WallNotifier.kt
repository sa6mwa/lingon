package systems.pkt.lingon.viewmodel

interface WallNotifier {
    fun notifyWall(sender: String, message: String)
}

object NoopWallNotifier : WallNotifier {
    override fun notifyWall(sender: String, message: String) {}
}

