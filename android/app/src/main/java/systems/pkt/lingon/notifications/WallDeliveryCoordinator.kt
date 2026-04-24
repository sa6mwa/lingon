package systems.pkt.lingon.notifications
import systems.pkt.lingon.data.WallWorkStateStore
import systems.pkt.lingon.viewmodel.WallNotification
import systems.pkt.lingon.viewmodel.WallNotifier

interface WallDeliveryCoordinator {
    suspend fun deliver(notification: WallNotification)
    suspend fun advanceCursor(endpoint: String, cursor: Long)
}

class MonotonicWallDeliveryCoordinator(
    private val stateStore: WallWorkStateStore,
    private val notifier: WallNotifier,
) : WallDeliveryCoordinator {
    override suspend fun deliver(notification: WallNotification) {
        val body = notification.message.trim()
        if (body.isBlank()) {
            return
        }
        if (!stateStore.shouldDeliverAndAdvance(notification.endpoint, notification.eventId)) {
            return
        }
        notifier.notifyWall(notification.copy(message = body))
    }

    override suspend fun advanceCursor(endpoint: String, cursor: Long) {
        stateStore.advanceCursor(endpoint, cursor)
    }
}

object NoopWallDeliveryCoordinator : WallDeliveryCoordinator {
    override suspend fun deliver(notification: WallNotification) {}

    override suspend fun advanceCursor(endpoint: String, cursor: Long) {}
}
