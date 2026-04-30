package systems.pkt.lingon.notifications

import org.junit.Assert.assertEquals
import org.junit.Test

class AndroidWallNotifierTest {
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
}
