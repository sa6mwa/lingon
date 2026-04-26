package systems.pkt.lingon.notifications
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import systems.pkt.lingon.data.WallWorkStateStore
import systems.pkt.lingon.viewmodel.WallNotification
import systems.pkt.lingon.viewmodel.WallNotifier

interface WallDeliveryCoordinator {
    suspend fun deliver(notification: WallNotification): Boolean
    suspend fun advanceCursor(endpoint: String, cursor: Long)
}

class MonotonicWallDeliveryCoordinator(
    private val stateStore: WallWorkStateStore,
    private val notifier: WallNotifier,
) : WallDeliveryCoordinator {
    private val deliveryMu = Mutex()

    override suspend fun deliver(notification: WallNotification): Boolean {
        return deliveryMu.withLock {
            val body = notification.message.trim()
            if (body.isBlank()) {
                return@withLock true
            }
            if (!stateStore.shouldDeliver(notification.endpoint, notification.eventId)) {
                return@withLock true
            }
            if (notifier.notifyWall(notification.copy(message = body))) {
                stateStore.recordDelivered(notification.endpoint, notification.eventId)
                return@withLock true
            }
            false
        }
    }

    override suspend fun advanceCursor(endpoint: String, cursor: Long) {
        stateStore.advanceCursor(endpoint, cursor)
    }
}

object NoopWallDeliveryCoordinator : WallDeliveryCoordinator {
    override suspend fun deliver(notification: WallNotification): Boolean = false

    override suspend fun advanceCursor(endpoint: String, cursor: Long) {}
}
