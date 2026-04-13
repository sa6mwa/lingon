package systems.pkt.lingon.notifications

import org.junit.Assert.assertEquals
import org.junit.Test
import systems.pkt.lingon.viewmodel.WallNotification

class DedupingWallNotifierTest {
    @Test
    fun suppressesDuplicateEventAcrossSources() {
        val delegate = RecordingWallNotifier()
        val notifier = DedupingWallNotifier(delegate = delegate)

        notifier.notifyWall(WallNotification(eventId = 42, sender = "relay", message = "hello world"))
        notifier.notifyWall(WallNotification(eventId = 42, sender = "relay", message = "hello world"))

        assertEquals(listOf(42L to "relay\nhello world"), delegate.deliveries)
    }

    @Test
    fun allowsDifferentEventIdsForSameMessage() {
        val delegate = RecordingWallNotifier()
        val notifier = DedupingWallNotifier(delegate = delegate)

        notifier.notifyWall(WallNotification(eventId = 7, sender = "relay", message = "hello world"))
        notifier.notifyWall(WallNotification(eventId = 8, sender = "relay", message = "hello world"))

        assertEquals(
            listOf(7L to "relay\nhello world", 8L to "relay\nhello world"),
            delegate.deliveries,
        )
    }

    @Test
    fun passesThroughUnknownEventIdWithoutDedupe() {
        val delegate = RecordingWallNotifier()
        val notifier = DedupingWallNotifier(delegate = delegate)

        notifier.notifyWall(WallNotification(eventId = 0, sender = "relay", message = "hello world"))
        notifier.notifyWall(WallNotification(eventId = 0, sender = "relay", message = "hello world"))

        assertEquals(
            listOf(0L to "relay\nhello world", 0L to "relay\nhello world"),
            delegate.deliveries,
        )
    }

    private class RecordingWallNotifier : systems.pkt.lingon.viewmodel.WallNotifier {
        val deliveries = mutableListOf<Pair<Long, String>>()

        override fun notifyWall(notification: WallNotification) {
            deliveries += notification.eventId to "${notification.sender.trim()}\n${notification.message.trim()}"
        }
    }
}
