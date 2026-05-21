package systems.pkt.lingon.notifications

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.delay
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import systems.pkt.lingon.data.WallWorkStateStore
import systems.pkt.lingon.data.relay.RelayWallEvent
import systems.pkt.lingon.data.relay.RelayWallEventsPage
import systems.pkt.lingon.viewmodel.WallNotification
import systems.pkt.lingon.viewmodel.WallNotifier

class WallDeliveryCoordinatorTest {
    @Test
    fun failedNotificationDoesNotAdvanceCursor() = runTest {
        val store = newStore()
        val notifier = RecordingNotifier(succeeds = false)
        val coordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val notification = notification(eventId = 42L)

        assertEquals(false, coordinator.deliver(notification))
        assertEquals(false, coordinator.deliver(notification))

        assertEquals(0L, store.loadCursor(notification.endpoint))
        assertEquals(2, notifier.deliveries.size)
    }

    @Test
    fun thrownNotificationDoesNotAdvanceCursorAndAllowsRetry() = runTest {
        val store = newStore()
        val notifier = ThrowOnceNotifier()
        val coordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val notification = notification(eventId = 42L)

        assertEquals(false, coordinator.deliver(notification))
        assertEquals(true, coordinator.deliver(notification))

        assertEquals(42L, store.loadCursor(notification.endpoint))
        assertEquals(2, notifier.deliveries.get())
    }

    @Test
    fun thrownNotificationRestoresPreviousCursor() = runTest {
        val store = newStore()
        val notifier = ThrowOnceNotifier()
        val coordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val endpoint = "https://relay.example/v1"
        store.advanceCursor(endpoint, 40L)

        assertEquals(false, coordinator.deliver(notification(eventId = 42L)))

        assertEquals(40L, store.loadCursor(endpoint))
        assertEquals(true, coordinator.deliver(notification(eventId = 41L)))
        assertEquals(41L, store.loadCursor(endpoint))
    }

    @Test
    fun cancelledNotificationRollsBackClaimAndPropagates() = runTest {
        val store = newStore()
        val coordinator = MonotonicWallDeliveryCoordinator(store, CancellingNotifier())
        val notification = notification(eventId = 42L)

        try {
            coordinator.deliver(notification)
            error("expected cancellation")
        } catch (_: CancellationException) {
            // Expected.
        }

        assertEquals(0L, store.loadCursor(notification.endpoint))
    }

    @Test
    fun successfulNotificationAdvancesCursorAndSuppressesReplay() = runTest {
        val store = newStore()
        val notifier = RecordingNotifier(succeeds = true)
        val coordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val notification = notification(eventId = 42L)

        assertEquals(true, coordinator.deliver(notification))
        assertEquals(true, coordinator.deliver(notification))

        assertEquals(42L, store.loadCursor(notification.endpoint))
        assertEquals(1, notifier.deliveries.size)
    }

    @Test
    fun olderEventAfterNewerEventDoesNotReplayOrMoveCursorBackward() = runTest {
        val store = newStore()
        val notifier = RecordingNotifier(succeeds = true)
        val coordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val newer = notification(eventId = 43L)
        val older = notification(eventId = 42L)

        assertEquals(true, coordinator.deliver(newer))
        assertEquals(true, coordinator.deliver(older))

        assertEquals(43L, store.loadCursor(newer.endpoint))
        assertEquals(listOf(43L), notifier.deliveries.map { it.eventId })
    }

    @Test
    fun concurrentDeliveryOfSameEventPostsOnlyOnce() = runTest {
        val store = newStore()
        val notifier = SlowRecordingNotifier()
        val coordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val notification = notification(eventId = 42L)

        awaitAll(
            async(Dispatchers.Default) { coordinator.deliver(notification) },
            async(Dispatchers.Default) { coordinator.deliver(notification) },
        )

        assertEquals(42L, store.loadCursor(notification.endpoint))
        assertEquals(1, notifier.deliveries.get())
    }

    @Test
    fun concurrentDeliveryThroughSeparateCoordinatorsPostsOnlyOnce() = runTest {
        val store = newStore()
        val notifier = SlowRecordingNotifier()
        val firstCoordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val secondCoordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val notification = notification(eventId = 42L)

        awaitAll(
            async(Dispatchers.Default) { firstCoordinator.deliver(notification) },
            async(Dispatchers.Default) { secondCoordinator.deliver(notification) },
        )

        assertEquals(42L, store.loadCursor(notification.endpoint))
        assertEquals(1, notifier.deliveries.get())
    }

    @Test
    fun inFlightFailedDeliveryThroughSeparateCoordinatorIsNotConsumed() = runTest {
        val store = newStore()
        val notifier = BlockingFailingNotifier()
        val firstCoordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val secondCoordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val notification = notification(eventId = 42L)

        val firstResult = async(Dispatchers.Default) { firstCoordinator.deliver(notification) }
        notifier.awaitEntered()

        val secondResult = async(Dispatchers.Default) { secondCoordinator.deliver(notification) }
        delay(100L)
        assertEquals(false, secondResult.isCompleted)

        notifier.release()
        assertEquals(false, firstResult.await())
        assertEquals(false, secondResult.await())
        assertEquals(0L, store.loadCursor(notification.endpoint))
    }

    @Test
    fun pollDoesNotExposeInFlightNotificationAsDeliveredCursor() = runTest {
        val store = newStore()
        val notifier = BlockingFailingNotifier()
        val coordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val endpoint = "https://relay.example/v1"
        store.advanceCursor(endpoint, 40L)

        val poll = async(Dispatchers.Default) {
            coordinator.pollOnce(endpoint, pageLimit = 100) { since, _ ->
                assertEquals(40L, since)
                page(wallEvent(42L, message = "notify me"), nextId = 42L)
            }
        }
        notifier.awaitEntered()

        assertEquals(
            "in-flight notification claims must not be visible as durable delivered cursor",
            40L,
            store.loadCursor(endpoint),
        )

        notifier.release()
        val result = poll.await()
        assertEquals(WallPollStatus.Blocked, result.status)
        assertEquals(40L, store.loadCursor(endpoint))
    }

    @Test
    fun pollDoesNotSkipFailedNotificationWhenLaterBlankEventExists() = runTest {
        val store = newStore()
        val failingCoordinator = MonotonicWallDeliveryCoordinator(store, RecordingNotifier(succeeds = false))
        val endpoint = "https://relay.example/v1"

        val failed = failingCoordinator.pollOnce(endpoint, pageLimit = 100) { since, _ ->
            assertEquals(0L, since)
            page(
                wallEvent(42L, message = "notify me"),
                wallEvent(43L, message = "   "),
                nextId = 43L,
            )
        }

        assertEquals(WallPollStatus.Blocked, failed.status)
        assertEquals(0L, store.loadCursor(endpoint))

        val notifier = RecordingNotifier(succeeds = true)
        val recoveringCoordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val recovered = recoveringCoordinator.pollOnce(endpoint, pageLimit = 100) { since, _ ->
            assertEquals(0L, since)
            page(
                wallEvent(42L, message = "notify me"),
                wallEvent(43L, message = "   "),
                nextId = 43L,
            )
        }

        assertEquals(WallPollStatus.Completed, recovered.status)
        assertEquals(43L, store.loadCursor(endpoint))
        assertEquals(listOf(42L), notifier.deliveries.map { it.eventId })
    }

    @Test
    fun concurrentPollsSerializeAndSecondPollSeesCommittedCursor() = runTest {
        val store = newStore()
        val notifier = BlockingSuccessNotifier()
        val coordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val endpoint = "https://relay.example/v1"
        val secondFetches = AtomicInteger(0)

        val first = async(Dispatchers.Default) {
            coordinator.pollOnce(endpoint, pageLimit = 100) { since, _ ->
                assertEquals(0L, since)
                page(wallEvent(42L, message = "notify me"), nextId = 42L)
            }
        }
        notifier.awaitEntered()

        val second = async(Dispatchers.Default) {
            coordinator.pollOnce(endpoint, pageLimit = 100) { since, _ ->
                secondFetches.incrementAndGet()
                assertEquals(42L, since)
                page(nextId = 42L)
            }
        }
        delay(100L)
        assertEquals("second poll should wait for first poll transaction", 0, secondFetches.get())

        notifier.release()
        assertEquals(WallPollStatus.Completed, first.await().status)
        assertEquals(WallPollStatus.Completed, second.await().status)
        assertEquals(42L, store.loadCursor(endpoint))
        assertEquals(1, notifier.deliveries.get())
    }

    @Test
    fun notificationSuppressionDoesNotConsumeEvent() = runTest {
        val store = newStore()
        val notifier = RecordingNotifier(succeeds = true)
        val coordinator = MonotonicWallDeliveryCoordinator(
            store,
            notifier,
            shouldPostNotification = { false },
        )
        val notification = notification(eventId = 42L)

        assertEquals(false, coordinator.deliver(notification))
        assertEquals(false, coordinator.deliver(notification))

        assertEquals(0L, store.loadCursor(notification.endpoint))
        assertEquals(0, notifier.deliveries.size)
    }

    @Test
    fun inAppConsumptionAdvancesCursorAndSuppressesReplayWithoutPostingNotification() = runTest {
        val store = newStore()
        val notifier = RecordingNotifier(succeeds = true)
        val coordinator = MonotonicWallDeliveryCoordinator(store, notifier)
        val notification = notification(eventId = 42L)

        assertEquals(true, coordinator.consumeInApp(notification))
        assertEquals(false, coordinator.consumeInApp(notification))

        assertEquals(42L, store.loadCursor(notification.endpoint))
        assertEquals(0, notifier.deliveries.size)
    }

    private fun notification(eventId: Long): WallNotification {
        return WallNotification(
            endpoint = "https://relay.example/v1",
            eventId = eventId,
            sender = "alice@example",
            sourceSessionName = "host-1",
            message = "hello",
        )
    }

    private fun page(vararg events: RelayWallEvent, nextId: Long): RelayWallEventsPage {
        return RelayWallEventsPage(events = events.toList(), nextId = nextId, hasMore = false)
    }

    private fun wallEvent(id: Long, message: String): RelayWallEvent {
        return RelayWallEvent(
            id = id,
            sender = "alice@example",
            sessionName = "host-1",
            message = message,
        )
    }

    private fun newStore(): WallWorkStateStore {
        val dataStore = PreferenceDataStoreFactory.create(
            scope = CoroutineScope(Dispatchers.IO + SupervisorJob()),
            produceFile = { Files.createTempFile("lingon-wall", ".preferences_pb").toFile() },
        )
        return WallWorkStateStore(dataStore)
    }

    private class RecordingNotifier(private val succeeds: Boolean) : WallNotifier {
        val deliveries = mutableListOf<WallNotification>()

        override fun notifyWall(notification: WallNotification): Boolean {
            deliveries += notification
            return succeeds
        }
    }

    private class ThrowOnceNotifier : WallNotifier {
        val deliveries = AtomicInteger(0)

        override fun notifyWall(notification: WallNotification): Boolean {
            if (deliveries.incrementAndGet() == 1) {
                throw IllegalStateException("notification platform rejected post")
            }
            return true
        }
    }

    private class CancellingNotifier : WallNotifier {
        override fun notifyWall(notification: WallNotification): Boolean {
            throw CancellationException("cancelled")
        }
    }

    private class SlowRecordingNotifier : WallNotifier {
        val deliveries = AtomicInteger(0)

        override fun notifyWall(notification: WallNotification): Boolean {
            deliveries.incrementAndGet()
            Thread.sleep(100L)
            return true
        }
    }

    private class BlockingFailingNotifier : WallNotifier {
        private val entered = CountDownLatch(1)
        private val release = CountDownLatch(1)

        override fun notifyWall(notification: WallNotification): Boolean {
            entered.countDown()
            if (!release.await(5, TimeUnit.SECONDS)) {
                error("timed out waiting to release failing notification")
            }
            return false
        }

        fun awaitEntered() {
            if (!entered.await(5, TimeUnit.SECONDS)) {
                error("timed out waiting for notification attempt")
            }
        }

        fun release() {
            release.countDown()
        }
    }

    private class BlockingSuccessNotifier : WallNotifier {
        val deliveries = AtomicInteger(0)
        private val entered = CountDownLatch(1)
        private val release = CountDownLatch(1)

        override fun notifyWall(notification: WallNotification): Boolean {
            deliveries.incrementAndGet()
            entered.countDown()
            if (!release.await(5, TimeUnit.SECONDS)) {
                error("timed out waiting to release successful notification")
            }
            return true
        }

        fun awaitEntered() {
            if (!entered.await(5, TimeUnit.SECONDS)) {
                error("timed out waiting for notification attempt")
            }
        }

        fun release() {
            release.countDown()
        }
    }
}
