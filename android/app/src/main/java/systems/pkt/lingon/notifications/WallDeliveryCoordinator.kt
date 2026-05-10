package systems.pkt.lingon.notifications

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import systems.pkt.lingon.data.WallWorkStateStore
import systems.pkt.lingon.data.relay.RelayWallEventsPage
import systems.pkt.lingon.viewmodel.WallNotification
import systems.pkt.lingon.viewmodel.WallNotifier

interface WallDeliveryCoordinator {
    suspend fun deliver(notification: WallNotification): Boolean
    suspend fun consumeInApp(notification: WallNotification): Boolean
    suspend fun pollOnce(
        endpoint: String,
        pageLimit: Int,
        fetchPage: suspend (since: Long, limit: Int) -> RelayWallEventsPage,
    ): WallPollResult
}

enum class WallPollStatus {
    Completed,
    Blocked,
    Reset,
}

data class WallPollResult(
    val status: WallPollStatus,
    val since: Long,
    val cursor: Long,
)

class MonotonicWallDeliveryCoordinator(
    private val stateStore: WallWorkStateStore,
    private val notifier: WallNotifier,
    private val shouldPostNotification: () -> Boolean = { true },
) : WallDeliveryCoordinator {
    override suspend fun deliver(notification: WallNotification): Boolean {
        return deliveryMu.withLock {
            if (!stateStore.shouldDeliver(notification.endpoint, notification.eventId)) {
                return@withLock true
            }
            if (!deliverNotificationLocked(notification)) return@withLock false
            stateStore.advanceCursor(notification.endpoint, notification.eventId)
            true
        }
    }

    override suspend fun consumeInApp(notification: WallNotification): Boolean {
        return deliveryMu.withLock {
            if (!stateStore.shouldDeliver(notification.endpoint, notification.eventId)) {
                return@withLock false
            }
            stateStore.advanceCursor(notification.endpoint, notification.eventId)
            notification.message.trim().isNotBlank()
        }
    }

    override suspend fun pollOnce(
        endpoint: String,
        pageLimit: Int,
        fetchPage: suspend (since: Long, limit: Int) -> RelayWallEventsPage,
    ): WallPollResult {
        val cleanedEndpoint = endpoint.trim()
        if (cleanedEndpoint.isBlank()) {
            return WallPollResult(WallPollStatus.Completed, since = 0L, cursor = 0L)
        }
        return deliveryMu.withLock {
            val since = stateStore.loadCursor(cleanedEndpoint)
            val page = fetchPage(since, pageLimit)
            if (shouldResetWallCursor(since, page.nextId, page.events.size)) {
                stateStore.clearCursor(cleanedEndpoint)
                return@withLock WallPollResult(WallPollStatus.Reset, since = since, cursor = 0L)
            }

            var cursor = since
            for (event in page.events) {
                if (event.id <= cursor) {
                    continue
                }
                val body = event.message.trim()
                if (body.isBlank()) {
                    cursor = stateStore.advanceCursor(cleanedEndpoint, event.id)
                    continue
                }
                val consumed = deliverNotificationLocked(
                    WallNotification(
                        endpoint = cleanedEndpoint,
                        eventId = event.id,
                        sender = event.sender,
                        sourceSessionName = event.sessionName ?: "",
                        message = body,
                    ),
                )
                if (!consumed) {
                    return@withLock WallPollResult(WallPollStatus.Blocked, since = since, cursor = cursor)
                }
                cursor = stateStore.advanceCursor(cleanedEndpoint, event.id)
            }
            if (page.nextId > cursor) {
                cursor = stateStore.advanceCursor(cleanedEndpoint, page.nextId)
            }
            WallPollResult(WallPollStatus.Completed, since = since, cursor = cursor)
        }
    }

    private fun deliverNotificationLocked(notification: WallNotification): Boolean {
        val body = notification.message.trim()
        if (body.isBlank()) {
            return true
        }
        if (!shouldPostNotification()) {
            return false
        }
        return try {
            notifier.notifyWall(notification.copy(message = body))
        } catch (err: CancellationException) {
            throw err
        } catch (_: Exception) {
            false
        }
    }

    private companion object {
        val deliveryMu = Mutex()
    }
}

object NoopWallDeliveryCoordinator : WallDeliveryCoordinator {
    override suspend fun deliver(notification: WallNotification): Boolean = false

    override suspend fun consumeInApp(notification: WallNotification): Boolean = false

    override suspend fun pollOnce(
        endpoint: String,
        pageLimit: Int,
        fetchPage: suspend (since: Long, limit: Int) -> RelayWallEventsPage,
    ): WallPollResult = WallPollResult(WallPollStatus.Completed, since = 0L, cursor = 0L)
}

internal fun shouldResetWallCursor(since: Long, nextId: Long, eventCount: Int): Boolean =
    since > 0L && eventCount == 0 && nextId in 0 until since
