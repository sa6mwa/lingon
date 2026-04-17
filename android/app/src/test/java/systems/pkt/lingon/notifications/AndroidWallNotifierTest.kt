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
    fun formatWallContentPrefixesMessageWithSource() {
        assertEquals(
            "alice@10.0.0.1#build-host: hello operators",
            formatWallContent("alice@10.0.0.1", "build-host", "hello operators"),
        )
    }
}
