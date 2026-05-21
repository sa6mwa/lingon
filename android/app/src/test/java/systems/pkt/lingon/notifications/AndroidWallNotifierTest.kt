package systems.pkt.lingon.notifications

import android.app.NotificationManager
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
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
        assertNotEquals(
            wallNotificationTag(wallNotification(eventId = 42L, message = "hello")),
            wallNotificationTag(wallNotification(eventId = 43L, message = "hello")),
        )
    }

    @Test
    fun wallNotificationIdTreatsSameEventAsSameNotificationEvenIfMessageChanges() {
        assertEquals(
            wallNotificationId(wallNotification(eventId = 55L, message = "hello one")),
            wallNotificationId(wallNotification(eventId = 55L, message = "hello two")),
        )
        assertEquals(
            wallNotificationTag(wallNotification(eventId = 55L, message = "hello one")),
            wallNotificationTag(wallNotification(eventId = 55L, message = "hello two")),
        )
    }

    @Test
    fun wallNotificationIdScopesEventIdentityByEndpoint() {
        assertNotEquals(
            wallNotificationId(wallNotification(eventId = 42L, endpoint = "https://one.test", message = "hello")),
            wallNotificationId(wallNotification(eventId = 42L, endpoint = "https://two.test", message = "hello")),
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
    fun wallNotificationTagDistinguishesFallbackMessagesWhenIntegerIdsCollide() {
        val first = wallNotification(eventId = 0L, message = "Aa")
        val second = wallNotification(eventId = 0L, message = "BB")

        assertEquals(
            "fixture should exercise a Java String.hashCode collision on the integer notification id",
            wallNotificationId(first),
            wallNotificationId(second),
        )
        assertNotEquals(
            "notification tag must carry collision-resistant identity so Android does not replace distinct fallback messages",
            wallNotificationTag(first),
            wallNotificationTag(second),
        )
    }

    @Test
    fun wallNotificationTagIsStableForLongFallbackMessages() {
        val body = "x".repeat(2048)
        val notification = wallNotification(eventId = 0L, message = body)
        val tag = wallNotificationTag(notification)

        assertEquals(tag, wallNotificationTag(notification.copy()))
        assertEquals("wall:".length + 64, tag.length)
        assertTrue("tag should be a stable lowercase SHA-256 hex digest: $tag", tag.matches(Regex("wall:[0-9a-f]{64}")))
    }

    @Test
    fun wallNotificationChannelImportanceNoneIsNotPostable() {
        assertFalse(wallNotificationChannelCanPost(NotificationManager.IMPORTANCE_NONE))
    }

    @Test
    fun wallNotificationChannelVisibleImportancesArePostable() {
        assertTrue(wallNotificationChannelCanPost(NotificationManager.IMPORTANCE_MIN))
        assertTrue(wallNotificationChannelCanPost(NotificationManager.IMPORTANCE_LOW))
        assertTrue(wallNotificationChannelCanPost(NotificationManager.IMPORTANCE_DEFAULT))
        assertTrue(wallNotificationChannelCanPost(NotificationManager.IMPORTANCE_HIGH))
    }

    @Test
    fun wallNotificationVisibilityRequiresMatchingChannelTagAndId() {
        val notification = wallNotification(eventId = 42L, message = "hello")
        val tag = wallNotificationTag(notification)
        val id = wallNotificationId(notification)

        assertTrue(
            isWallNotificationStatusBarEntry(
                channelId = wallNotificationChannelId,
                tag = tag,
                id = id,
                expectedChannelId = wallNotificationChannelId,
                expectedTag = tag,
                expectedId = id,
            ),
        )
        assertFalse(
            isWallNotificationStatusBarEntry(
                channelId = "other",
                tag = tag,
                id = id,
                expectedChannelId = wallNotificationChannelId,
                expectedTag = tag,
                expectedId = id,
            ),
        )
        assertFalse(
            isWallNotificationStatusBarEntry(
                channelId = wallNotificationChannelId,
                tag = "wall:other",
                id = id,
                expectedChannelId = wallNotificationChannelId,
                expectedTag = tag,
                expectedId = id,
            ),
        )
        assertFalse(
            isWallNotificationStatusBarEntry(
                channelId = wallNotificationChannelId,
                tag = tag,
                id = id + 1,
                expectedChannelId = wallNotificationChannelId,
                expectedTag = tag,
                expectedId = id,
            ),
        )
    }

    @Test
    fun wallNotificationVisibilityKeepsCollidingIntegerIdsDistinctByTag() {
        val first = wallNotification(eventId = 0L, message = "Aa")
        val second = wallNotification(eventId = 0L, message = "BB")

        assertEquals(wallNotificationId(first), wallNotificationId(second))
        assertFalse(
            isWallNotificationStatusBarEntry(
                channelId = wallNotificationChannelId,
                tag = wallNotificationTag(second),
                id = wallNotificationId(second),
                expectedChannelId = wallNotificationChannelId,
                expectedTag = wallNotificationTag(first),
                expectedId = wallNotificationId(first),
            ),
        )
    }

    @Test
    fun wallNotificationIdTrimsFallbackIdentityFields() {
        assertEquals(
            wallNotificationId(
                wallNotification(
                    eventId = 0L,
                    endpoint = " https://example.test ",
                    sender = " alice@10.0.0.1 ",
                    sourceSessionName = " build-host ",
                    message = " hello ",
                ),
            ),
            wallNotificationId(
                wallNotification(
                    eventId = 0L,
                    endpoint = "https://example.test",
                    sender = "alice@10.0.0.1",
                    sourceSessionName = "build-host",
                    message = "hello",
                ),
            ),
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

    private fun wallNotification(
        eventId: Long,
        endpoint: String = "https://example.test",
        sender: String = "alice@10.0.0.1",
        sourceSessionName: String = "build-host",
        message: String,
    ): WallNotification {
        return WallNotification(
            endpoint = endpoint,
            eventId = eventId,
            sender = sender,
            sourceSessionName = sourceSessionName,
            message = message,
        )
    }
}
