package systems.pkt.lingon.notifications

import systems.pkt.lingon.viewmodel.WallNotification
import systems.pkt.lingon.viewmodel.WallNotifier

class DedupingWallNotifier(
    private val delegate: WallNotifier,
) : WallNotifier {
    private val lock = Any()
    private val deliveredEventKeys = LinkedHashSet<String>()

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
        val endpoint = notification.endpoint.trim()
        if (endpoint.isBlank()) {
            delegate.notifyWall(notification.copy(message = body))
            return
        }
        val eventKey = "$endpoint#$eventID"
        synchronized(lock) {
            if (deliveredEventKeys.contains(eventKey)) {
                return
            }
            deliveredEventKeys.add(eventKey)
        }
        delegate.notifyWall(notification.copy(message = body))
    }
}
