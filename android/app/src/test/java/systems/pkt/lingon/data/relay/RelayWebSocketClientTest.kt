package systems.pkt.lingon.data.relay

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import java.nio.file.Files
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import okhttp3.CookieJar
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import systems.pkt.lingon.data.HttpClientProvider
import systems.pkt.lingon.data.certs.CertificateStore

class RelayWebSocketClientTest {
    @Test
    fun cleartextShareTokenWebsocketRequestCarriesTokenFallback() {
        val client = RelayWebSocketClient(testHttpClientProvider())

        val request = client.buildWebSocketRequest(
            connectOptions(
                baseUrl = "http://127.0.0.1:8080/v1".toHttpUrl(),
                shareToken = "LGE-cleartext-token",
            ),
        )

        assertEquals("/v1/ws/client?token=LGE-cleartext-token", request.url.encodedPathWithQuery())
        assertEquals("1", request.header("X-Lingon-Share-Session"))
    }

    @Test
    fun httpsShareTokenWebsocketRequestUsesShareSessionCookieOnly() {
        val client = RelayWebSocketClient(testHttpClientProvider())

        val request = client.buildWebSocketRequest(
            connectOptions(
                baseUrl = "https://relay.example/v1".toHttpUrl(),
                shareToken = "LGE-secure-token",
            ),
        )

        assertEquals("/v1/ws/client", request.url.encodedPathWithQuery())
        assertNull(request.url.queryParameter("token"))
        assertEquals("1", request.header("X-Lingon-Share-Session"))
    }

    private fun connectOptions(
        baseUrl: okhttp3.HttpUrl,
        shareToken: String?,
    ): RelayWebSocketClient.ConnectOptions {
        return RelayWebSocketClient.ConnectOptions(
            baseUrl = baseUrl,
            sessionId = "shared-real",
            shareToken = shareToken,
            clientId = "android-test",
            cols = 80,
            rows = 24,
            wantsControl = false,
        )
    }

    private fun okhttp3.HttpUrl.encodedPathWithQuery(): String {
        val query = encodedQuery
        return if (query.isNullOrBlank()) encodedPath else "$encodedPath?$query"
    }

    private fun testHttpClientProvider(): HttpClientProvider {
        val dataStore = PreferenceDataStoreFactory.create(
            scope = CoroutineScope(Dispatchers.IO + SupervisorJob()),
            produceFile = { Files.createTempFile("lingon-ws-client", ".preferences_pb").toFile() },
        )
        return HttpClientProvider(CertificateStore(dataStore), CookieJar.NO_COOKIES)
    }
}
