package systems.pkt.lingon.notifications

import org.junit.Assert.assertEquals
import org.junit.Test
import systems.pkt.lingon.viewmodel.WallNotification

class DedupingWallNotifierTest {
    @Test
    fun suppressesDuplicateEventAcrossSources() {
        val delegate = RecordingWallNotifier()
        val notifier = DedupingWallNotifier(delegate = delegate)

        notifier.notifyWall(
            WallNotification(endpoint = "https://relay-a/v1", eventId = 42, sender = "relay", message = "hello world"),
        )
        notifier.notifyWall(
            WallNotification(endpoint = "https://relay-a/v1", eventId = 42, sender = "relay", message = "hello world"),
        )

        assertEquals(listOf("https://relay-a/v1#42" to "relay\nhello world"), delegate.deliveries)
    }

    @Test
    fun allowsDifferentEventIdsForSameMessage() {
        val delegate = RecordingWallNotifier()
        val notifier = DedupingWallNotifier(delegate = delegate)

        notifier.notifyWall(
            WallNotification(endpoint = "https://relay-a/v1", eventId = 7, sender = "relay", message = "hello world"),
        )
        notifier.notifyWall(
            WallNotification(endpoint = "https://relay-a/v1", eventId = 8, sender = "relay", message = "hello world"),
        )

        assertEquals(
            listOf("https://relay-a/v1#7" to "relay\nhello world", "https://relay-a/v1#8" to "relay\nhello world"),
            delegate.deliveries,
        )
    }

    @Test
    fun allowsSameEventIdAcrossDifferentEndpoints() {
        val delegate = RecordingWallNotifier()
        val notifier = DedupingWallNotifier(delegate = delegate)

        notifier.notifyWall(
            WallNotification(endpoint = "https://relay-a/v1", eventId = 42, sender = "relay", message = "hello world"),
        )
        notifier.notifyWall(
            WallNotification(endpoint = "https://relay-b/v1", eventId = 42, sender = "relay", message = "hello world"),
        )

        assertEquals(
            listOf("https://relay-a/v1#42" to "relay\nhello world", "https://relay-b/v1#42" to "relay\nhello world"),
            delegate.deliveries,
        )
    }

    @Test
    fun passesThroughUnknownEventIdOrEndpointWithoutDedupe() {
        val delegate = RecordingWallNotifier()
        val notifier = DedupingWallNotifier(delegate = delegate)

        notifier.notifyWall(WallNotification(eventId = 0, sender = "relay", message = "hello world"))
        notifier.notifyWall(WallNotification(eventId = 0, sender = "relay", message = "hello world"))
        notifier.notifyWall(WallNotification(eventId = 42, sender = "relay", message = "hello world"))
        notifier.notifyWall(WallNotification(eventId = 42, sender = "relay", message = "hello world"))

        assertEquals(
            listOf(
                "0" to "relay\nhello world",
                "0" to "relay\nhello world",
                "42" to "relay\nhello world",
                "42" to "relay\nhello world",
            ),
            delegate.deliveries,
        )
    }

    private class RecordingWallNotifier : systems.pkt.lingon.viewmodel.WallNotifier {
        val deliveries = mutableListOf<Pair<String, String>>()

        override fun notifyWall(notification: WallNotification) {
            val endpoint = notification.endpoint.trim()
            val key = if (endpoint.isBlank()) {
                notification.eventId.toString()
            } else {
                "$endpoint#${notification.eventId}"
            }
            deliveries += key to "${notification.sender.trim()}\n${notification.message.trim()}"
        }
    }
}
