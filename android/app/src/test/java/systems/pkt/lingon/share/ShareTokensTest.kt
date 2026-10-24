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
        assertEquals(endpoint, ShareTokens.findInText("Join: $encoded")?.endpoint)
    }

    @Test
    fun parseRejectsOversizedCandidateBeforeDecode() {
        val oversized = "LGE" + "A".repeat(7000)

        assertNull(ShareTokens.parse(oversized))
        assertNull(ShareTokens.findInText("share this $oversized"))
    }

    @Test
    fun discoveryUsesPayloadLengthAndPreservesWrappedFormatValidation() {
        val random = ByteArray(20) { (it * 13).toByte() }
        for (endpointLength in (10..18).toList() + 2048) {
            val endpoint = "https://r/" + "a".repeat(endpointLength - 10)
            val token = requireNotNull(ShareTokens.encodeEmbedded(random, endpoint))
            val parsed = ShareTokens.findInText("Join: " + token.lowercase().chunked(17).joinToString("\r\n\t") + " trailing words")
            assertEquals(endpoint, parsed?.endpoint)
            assertArrayEquals(random, parsed?.random)
            assertNull(ShareTokens.findInText(token.dropLast(2)))
            val corrupt = token.take(8) + (if (token[8] == 'A') 'B' else 'A') + token.drop(9)
            assertNull(ShareTokens.findInText(corrupt))
        }
        val valid = requireNotNull(ShareTokens.encodeBare(random))
        assertNull(ShareTokens.findInText("LGBZZ" + "0".repeat(35))) // Wrong decoded version.
        assertArrayEquals(random, ShareTokens.findInText("LGE04" + "0".repeat(35) + "\n" + valid)?.random) // Zero endpoint length.
    }

    @Test
    fun findInTextHandlesRepeatedMalformedPrefixesAndStillFindsValidToken() {
        val malformed = "LGE".repeat(1000)
        val random = ByteArray(20) { it.toByte() }
        val encoded = requireNotNull(ShareTokens.encodeEmbedded(random, "https://relay.example/v1"))
        assertNull(ShareTokens.findInText(malformed))
        assertArrayEquals(random, ShareTokens.findInText(malformed + "\n" + encoded)?.random)
    }

    @Test
    fun findInTextContinuesAfterInvalidAndOversizedCandidates() {
        val random = ByteArray(20) { (it * 7).toByte() }
        val endpoint = "https://relay.example/v1"
        for (encoded in listOf(requireNotNull(ShareTokens.encodeBare(random)), requireNotNull(ShareTokens.encodeEmbedded(random, endpoint)))) {
            for (prefix in listOf("LGBinvalid\n", "LGEbad LGBinvalid\t", "LGE" + "A".repeat(7000) + "\n")) {
                val parsed = ShareTokens.findInText(prefix + encoded.chunked(17).joinToString("\n") + "\nShared from chat")
                assertNotNull("valid token after ${prefix.take(25)}", parsed)
                assertArrayEquals(random, parsed?.random)
                assertEquals(if (encoded.startsWith("LGE")) endpoint else null, parsed?.endpoint)
            }
        }
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
