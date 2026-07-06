package systems.pkt.lingon.share

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class ShareTokensTest {
    @Test
    fun embeddedEndpointLengthUsesUtf8Bytes() {
        val random = ByteArray(20) { it.toByte() }
        val endpoint = "https://lingon.example/å"

        val encoded = ShareTokens.encodeEmbedded(random, endpoint)
        assertNotNull(encoded)
        val parsed = ShareTokens.parse(requireNotNull(encoded))

        assertNotNull(parsed)
        assertEquals(ShareTokens.Kind.Embedded, parsed?.kind)
        assertArrayEquals(random, parsed?.random)
        assertEquals(endpoint, parsed?.endpoint)
    }

    @Test
    fun parseRejectsOversizedCandidateBeforeDecode() {
        val oversized = "LGE" + "A".repeat(7000)

        assertNull(ShareTokens.parse(oversized))
        assertNull(ShareTokens.findInText("share this $oversized"))
    }

    @Test
    fun findInTextAcceptsWrappedEmbeddedToken() {
        val random = ByteArray(20) { (it * 3).toByte() }
        val endpoint = "https://relay.example/v1/share/session/abcdef"
        val encoded = requireNotNull(ShareTokens.encodeEmbedded(random, endpoint))
        val wrapped = encoded.chunked(17).joinToString("\n")

        val parsed = ShareTokens.findInText("Join this Lingon session:\n$wrapped\nShared from chat")

        assertNotNull(parsed)
        assertEquals(ShareTokens.Kind.Embedded, parsed?.kind)
        assertArrayEquals(random, parsed?.random)
        assertEquals(endpoint, parsed?.endpoint)
    }
}
