package systems.pkt.lingon.notifications

import org.junit.Assert.assertEquals
import org.junit.Test

class DedupingWallNotifierTest {
    @Test
    fun suppressesDuplicateMessageAcrossSourcesWithinWindow() {
        val delegate = RecordingWallNotifier()
        var nowMs = 1_000L
        val notifier = DedupingWallNotifier(
            delegate = delegate,
            nowProvider = { nowMs },
        )

        notifier.notifyWall("relay", "hello world")
        nowMs += 1_000L
        notifier.notifyWall("relay", "hello world")

        assertEquals(listOf("relay\nhello world"), delegate.deliveries)
    }

    @Test
    fun allowsDifferentMessagesWithinWindow() {
        val delegate = RecordingWallNotifier()
        var nowMs = 1_000L
        val notifier = DedupingWallNotifier(
            delegate = delegate,
            nowProvider = { nowMs },
        )

        notifier.notifyWall("relay", "first")
        nowMs += 1_000L
        notifier.notifyWall("relay", "second")

        assertEquals(
            listOf("relay\nfirst", "relay\nsecond"),
            delegate.deliveries,
        )
    }

    @Test
    fun allowsRepeatAfterDedupeWindowExpires() {
        val delegate = RecordingWallNotifier()
        var nowMs = 1_000L
        val notifier = DedupingWallNotifier(
            delegate = delegate,
            nowProvider = { nowMs },
        )

        notifier.notifyWall("relay", "hello world")
        nowMs += 30_001L
        notifier.notifyWall("relay", "hello world")

        assertEquals(
            listOf("relay\nhello world", "relay\nhello world"),
            delegate.deliveries,
        )
    }

    private class RecordingWallNotifier : systems.pkt.lingon.viewmodel.WallNotifier {
        val deliveries = mutableListOf<String>()

        override fun notifyWall(sender: String, message: String) {
            deliveries += "${sender.trim()}\n${message.trim()}"
        }
    }
}
