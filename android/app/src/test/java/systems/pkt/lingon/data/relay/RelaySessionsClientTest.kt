package systems.pkt.lingon.data.relay

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import okhttp3.CookieJar
import okhttp3.Cookie
import okhttp3.HttpUrl
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import systems.pkt.lingon.data.ApiException
import systems.pkt.lingon.data.EndpointStore
import systems.pkt.lingon.data.HttpClientProvider
import systems.pkt.lingon.data.certs.CertificateStore

class RelaySessionsClientTest {
    @get:Rule val temporaryFolder = TemporaryFolder()

    @Test
    fun reportsUncertainResponseWithoutRepeatingCreation() = runBlocking {
        withClient(MockResponse().setBody("{\"session_id\":")) { client, server ->
            try {
                client.manageSession(SessionManagementRequest("once", "host", "create"))
                throw AssertionError("accepted a truncated acknowledgement")
            } catch (err: ApiException) {
                assertTrue(err.message.orEmpty().contains("outcome unknown"))
            }
            assertEquals(1, server.requestCount)
        }
    }

    @Test
    fun submitsOwnerManagementRequestAndDecodesUnicodeName() = runBlocking {
        withClient(MockResponse().setBody("{\"session_id\":\"host\",\"name\":\"日本語 🦊\"}")) { client, server ->
            val result = client.manageSession(SessionManagementRequest("rename-once", "host", "rename", "日本語 🦊"))
            assertEquals("host", result.sessionId)
            assertEquals("日本語 🦊", result.name)
            val request = server.takeRequest()
            assertEquals("POST", request.method)
            assertEquals("/v1/sessions/manage", request.path)
            assertTrue(request.getHeader("Cookie").orEmpty().contains("lingon_access=owner"))
            assertTrue(request.body.readUtf8().contains("rename-once"))
            assertEquals(1, server.requestCount)
        }
    }

    @Test
    fun missingOwnerAuthenticationIsRejectedBeforeSendingOperation() = runBlocking {
        withClient(MockResponse(), authorized = false) { client, server ->
            try {
                client.manageSession(SessionManagementRequest("once", "host", "create"))
                throw AssertionError("accepted unauthenticated management")
            } catch (err: ApiException) {
                assertEquals(401, err.statusCode)
                assertTrue(err.message.orEmpty().contains("rejected"))
            }
            assertEquals(0, server.requestCount)
        }
    }

    private suspend fun withClient(response: MockResponse, authorized: Boolean = true, block: suspend (RelaySessionsClient, MockWebServer) -> Unit) {
        val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())
        val server = MockWebServer()
        server.enqueue(response)
        server.start()
        try {
            val preferences = temporaryFolder.newFolder().resolve("client.preferences_pb")
            val dataStore = PreferenceDataStoreFactory.create(scope = scope, produceFile = { preferences })
            val endpoints = EndpointStore(dataStore, scope)
            endpoints.setEndpoint(server.url("/v1/").toString())
            val owner = Cookie.Builder().name("lingon_access").value("owner").hostOnlyDomain(server.url("/").host).path("/").expiresAt(Long.MAX_VALUE).build()
            val cookies = object : CookieJar {
                override fun loadForRequest(url: HttpUrl) = if (authorized) listOf(owner) else emptyList()
                override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {}
            }
            block(RelaySessionsClient(HttpClientProvider(CertificateStore(dataStore), cookies), endpoints), server)
        } finally {
            scope.cancel()
            server.shutdown()
        }
    }
}
