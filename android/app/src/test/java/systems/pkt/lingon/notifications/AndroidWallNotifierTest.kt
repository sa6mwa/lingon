package systems.pkt.lingon.notifications

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test
import systems.pkt.lingon.viewmodel.WallNotification

class AndroidWallNotifierTest {
    @Test
    fun wallNotificationIdIsStableForSameEvent() {
        val notification = wallNotification(eventId = 42L, message = "hello")

        assertEquals(wallNotificationId(notification), wallNotificationId(notification.copy()))
        assertEquals(wallNotificationTag(notification), wallNotificationTag(notification.copy()))
    }

    @Test
    fun wallNotificationIdDiffersForDistinctEvents() {
        assertNotEquals(
            wallNotificationId(wallNotification(eventId = 42L, message = "hello")),
            wallNotificationId(wallNotification(eventId = 43L, message = "hello")),
        )
    }

    @Test
    fun wallNotificationIdUsesStableFallbackIdentityForSameMessageWithoutEventId() {
        val notification = wallNotification(eventId = 0L, message = "hello")

        assertEquals(wallNotificationId(notification), wallNotificationId(notification.copy()))
        assertEquals(wallNotificationTag(notification), wallNotificationTag(notification.copy()))
    }

    @Test
    fun wallNotificationIdUsesDistinctFallbackIdentityForDistinctMessagesWithoutEventId() {
        assertNotEquals(
            wallNotificationId(wallNotification(eventId = 0L, message = "hello one")),
            wallNotificationId(wallNotification(eventId = 0L, message = "hello two")),
        )
        assertNotEquals(
            wallNotificationTag(wallNotification(eventId = 0L, message = "hello one")),
            wallNotificationTag(wallNotification(eventId = 0L, message = "hello two")),
        )
    }

    @Test
    fun wallNotificationIdKeepsContentInIdentityEvenWhenEventIdMatches() {
        assertNotEquals(
            wallNotificationId(wallNotification(eventId = 55L, message = "hello one")),
            wallNotificationId(wallNotification(eventId = 55L, message = "hello two")),
        )
    }

    @Test
    fun formatWallSourceAppendsHumanSessionName() {
        assertEquals(
            "alice@10.0.0.1#build-host",
            formatWallSource("alice@10.0.0.1", "build-host"),
        )
    }

    @Test
    fun formatWallContentDoesNotRepeatSourceWhenMessagePresent() {
        assertEquals(
            "hello operators",
            formatWallContent("alice@10.0.0.1", "build-host", "hello operators"),
        )
    }

    @Test
    fun formatWallBodyReturnsTrimmedMessageOnly() {
        assertEquals(
            "hello operators",
            formatWallBody("  hello operators  "),
        )
    }

    @Test
    fun formatWallContentFallsBackToSourceWhenMessageEmpty() {
        assertEquals(
            "alice@10.0.0.1#build-host",
            formatWallContent("alice@10.0.0.1", "build-host", "   "),
        )
    }

    private fun wallNotification(eventId: Long, message: String): WallNotification {
        return WallNotification(
            endpoint = "https://example.test",
            eventId = eventId,
            sender = "alice@10.0.0.1",
            sourceSessionName = "build-host",
            message = message,
        )
    }
}
