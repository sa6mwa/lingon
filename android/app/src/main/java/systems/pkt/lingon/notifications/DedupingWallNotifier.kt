package systems.pkt.lingon.notifications

import systems.pkt.lingon.viewmodel.WallNotification
import systems.pkt.lingon.viewmodel.WallNotifier

class DedupingWallNotifier(
    private val delegate: WallNotifier,
) : WallNotifier {
    private val lock = Any()
    private val deliveredEventIDs = LinkedHashSet<Long>()

    override fun notifyWall(notification: WallNotification) {
        val body = notification.message.trim()
        if (body.isBlank()) {
            return
        }
        val eventID = notification.eventId
        if (eventID <= 0) {
            delegate.notifyWall(notification.copy(message = body))
            return
        }
        synchronized(lock) {
            if (deliveredEventIDs.contains(eventID)) {
                return
            }
            deliveredEventIDs.add(eventID)
        }
        delegate.notifyWall(notification.copy(message = body))
    }
}
