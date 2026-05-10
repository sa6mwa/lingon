package systems.pkt.lingon.notifications

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import systems.pkt.lingon.data.WallWorkStateStore
import systems.pkt.lingon.viewmodel.WallNotification
import systems.pkt.lingon.viewmodel.WallNotifier

interface WallDeliveryCoordinator {
    suspend fun deliver(notification: WallNotification): Boolean
    suspend fun consumeInApp(notification: WallNotification): Boolean
    suspend fun advanceCursor(endpoint: String, cursor: Long)
}

class MonotonicWallDeliveryCoordinator(
    private val stateStore: WallWorkStateStore,
    private val notifier: WallNotifier,
    private val shouldPostNotification: () -> Boolean = { true },
) : WallDeliveryCoordinator {
    override suspend fun deliver(notification: WallNotification): Boolean {
        return deliveryMu.withLock {
            val body = notification.message.trim()
            if (body.isBlank()) {
                return@withLock true
            }
            if (!stateStore.shouldDeliver(notification.endpoint, notification.eventId)) {
                return@withLock true
            }
            if (!shouldPostNotification()) {
                return@withLock false
            }
            val claim = stateStore.claimDelivery(notification.endpoint, notification.eventId)
                ?: return@withLock true
            try {
                if (notifier.notifyWall(notification.copy(message = body))) {
                    return@withLock true
                }
            } catch (err: CancellationException) {
                stateStore.rollbackDeliveryClaim(claim)
                throw err
            } catch (_: Exception) {
                stateStore.rollbackDeliveryClaim(claim)
                return@withLock false
            }
            stateStore.rollbackDeliveryClaim(claim)
            false
        }
    }

    override suspend fun consumeInApp(notification: WallNotification): Boolean {
        return deliveryMu.withLock {
            if (stateStore.claimDelivery(notification.endpoint, notification.eventId) == null) {
                return@withLock false
            }
            notification.message.trim().isNotBlank()
        }
    }

    override suspend fun advanceCursor(endpoint: String, cursor: Long) {
        stateStore.advanceCursor(endpoint, cursor)
    }

    private companion object {
        val deliveryMu = Mutex()
    }
}

object NoopWallDeliveryCoordinator : WallDeliveryCoordinator {
    override suspend fun deliver(notification: WallNotification): Boolean = false

    override suspend fun consumeInApp(notification: WallNotification): Boolean = false

    override suspend fun advanceCursor(endpoint: String, cursor: Long) {}
}
