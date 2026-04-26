package systems.pkt.lingon.notifications

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import java.nio.file.Files
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import systems.pkt.lingon.data.WallWorkStateStore
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

    private fun notification(eventId: Long): WallNotification {
        return WallNotification(
            endpoint = "https://relay.example/v1",
            eventId = eventId,
            sender = "alice@example",
            sourceSessionName = "host-1",
            message = "hello",
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

    private class SlowRecordingNotifier : WallNotifier {
        val deliveries = AtomicInteger(0)

        override fun notifyWall(notification: WallNotification): Boolean {
            deliveries.incrementAndGet()
            Thread.sleep(100L)
            return true
        }
    }
}
