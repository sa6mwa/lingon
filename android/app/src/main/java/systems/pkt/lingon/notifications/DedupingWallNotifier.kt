package systems.pkt.lingon.notifications

import systems.pkt.lingon.viewmodel.WallNotifier

class DedupingWallNotifier(
    private val delegate: WallNotifier,
    private val nowProvider: () -> Long = System::currentTimeMillis,
) : WallNotifier {
    private val lock = Any()
    private val recentNotifications = LinkedHashMap<String, Long>()

    override fun notifyWall(sender: String, message: String) {
        val body = message.trim()
        if (body.isBlank()) {
            return
        }
        val now = nowProvider()
        val key = wallNotificationKey(sender, body)
        synchronized(lock) {
            pruneOldEntries(now)
            val seenAt = recentNotifications[key]
            if (seenAt != null && kotlin.math.abs(now - seenAt) <= dedupeWindowMs) {
                return
            }
            recentNotifications[key] = now
        }
        delegate.notifyWall(sender, body)
    }

    private fun pruneOldEntries(now: Long) {
        val pruneBefore = now - dedupeWindowMs
        val iterator = recentNotifications.entries.iterator()
        while (iterator.hasNext()) {
            val entry = iterator.next()
            if (entry.value < pruneBefore) {
                iterator.remove()
            }
        }
    }

    private fun wallNotificationKey(sender: String, message: String): String {
        return "${sender.trim()}\n${message.trim()}"
    }

    private companion object {
        private const val dedupeWindowMs = 30_000L
    }
}
