package systems.pkt.lingon.work

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import systems.pkt.lingon.notifications.shouldResetWallCursor

class BackgroundWallForegroundServiceTest {
    @Test
    fun shouldResetCursorWhenRelayWatermarkFallsBehindStoredCursor() {
        assertTrue(shouldResetWallCursor(since = 36L, nextId = 6L, eventCount = 0))
    }

    @Test
    fun shouldNotResetCursorWhenNoNewWallsExist() {
        assertFalse(shouldResetWallCursor(since = 36L, nextId = 36L, eventCount = 0))
    }

    @Test
    fun shouldNotResetCursorWhenEventsAreReturned() {
        assertFalse(shouldResetWallCursor(since = 36L, nextId = 6L, eventCount = 1))
    }

    @Test
    fun shouldPollBackgroundWallOnlyWhenAppIsBackgrounded() {
        assertFalse(shouldPollBackgroundWall(appInForeground = true))
        assertTrue(shouldPollBackgroundWall(appInForeground = false))
    }
}
