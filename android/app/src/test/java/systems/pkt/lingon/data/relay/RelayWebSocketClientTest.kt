package systems.pkt.lingon.data.relay

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import java.nio.file.Files
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import okhttp3.CookieJar
import okhttp3.Cookie
import okhttp3.HttpUrl
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.Response
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.RecordedRequest
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import systems.pkt.lingon.data.ApiException
import org.junit.Test
import systems.pkt.lingon.data.HttpClientProvider
import systems.pkt.lingon.data.certs.CertificateStore

class RelayWebSocketClientTest {
    @Test
    fun shareCookieJarRetainsOnlyScopedUnexpiredShareGrant() {
        var now = 100L
        val jar = ShareSessionCookieJar { now }
        val endpoint = "https://relay.example/v1/ws/client".toHttpUrl()
        val grant = Cookie.Builder().name("bifrons_share_session").value("grant")
            .hostOnlyDomain(endpoint.host).path("/v1").secure().expiresAt(200L).build()
        val account = Cookie.Builder().name("lingon_access").value("account")
            .hostOnlyDomain(endpoint.host).path("/").build()
        jar.saveFromResponse(endpoint, listOf(account, grant))
        assertEquals(listOf(grant), jar.loadForRequest(endpoint))
        assertTrue(jar.loadForRequest(endpoint.newBuilder().scheme("http").build()).isEmpty())
        assertTrue(jar.loadForRequest(endpoint.newBuilder().host("other.example").build()).isEmpty())
        assertTrue(jar.loadForRequest(endpoint.newBuilder().encodedPath("/other").build()).isEmpty())
        now = 200L
        assertTrue(jar.loadForRequest(endpoint).isEmpty())
        val replacement = Cookie.Builder().name("bifrons_share_session").value("replacement")
            .hostOnlyDomain(endpoint.host).path("/v1").secure().expiresAt(300L).build()
        jar.saveFromResponse(endpoint, listOf(replacement, account))
        assertEquals(listOf(replacement), jar.loadForRequest(endpoint))
        jar.saveFromResponse(endpoint, listOf(grant))
        assertTrue(jar.loadForRequest(endpoint).isEmpty())
    }

    @Test
    fun shareConnectionRejectsMissingOrMismatchedAuthentication() {
        val provider = testHttpClientProvider()
        val client = RelayWebSocketClient(provider)
        val endpoint = "https://relay.example/v1".toHttpUrl()
        val authentication = AuthenticatedShareSession(
            RelayShareSession(sessionId = "shared-real", scope = "control"), endpoint,
            "token", provider.clientFor(endpoint),
        )
        val options = connectOptions(endpoint, "token").copy(shareAuthentication = authentication)
        val listener = object : RelayWebSocketClient.Listener {
            override fun onFrame(webSocket: WebSocket, frame: systems.pkt.lingon.protocol.Frame) {}
        }
        for (invalid in listOf(
            options.copy(shareAuthentication = null), options.copy(shareToken = "other-token"),
            options.copy(baseUrl = "https://other.example/v1".toHttpUrl()), options.copy(sessionId = "other-session"),
            options.copy(shareToken = null),
        )) assertThrows(IllegalArgumentException::class.java) { client.connect(invalid, listener) }
    }

    @Test
    fun successfulShareResponseWithoutHandshakeCookieIsRejected() {
        val server = MockWebServer()
        server.enqueue(MockResponse().setBody("{\"session_id\":\"shared-real\",\"scope\":\"view\"}"))
        server.start()
        try {
            val client = RelayWebSocketClient(testHttpClientProvider())
            val err = assertThrows(IllegalStateException::class.java) {
                client.authenticateShareSession(server.url("/v1"), "token")
            }
            assertEquals("share authentication failed: missing cookie", err.message)
        } finally {
            server.shutdown()
        }
    }
    @Test
    fun lateSuccessfulShareAuthenticationCannotReplaceCurrentHandshakeCookie() {
        assertShareAuthenticationIsolation(invalidOldToken = false)
    }

    @Test
    fun lateInvalidShareAuthenticationCannotDeleteCurrentHandshakeCookie() {
        assertShareAuthenticationIsolation(invalidOldToken = true)
    }

    private fun assertShareAuthenticationIsolation(invalidOldToken: Boolean) {
        val oldStarted = CountDownLatch(1)
        val releaseOld = CountDownLatch(1)
        val handshakeCookie = AtomicReference<String?>()
        val server = MockWebServer()
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                if (request.path?.startsWith("/v1/auth/share") == true) {
                    val old = request.body.readUtf8().contains("old-token")
                    if (old) {
                        oldStarted.countDown()
                        check(releaseOld.await(10, TimeUnit.SECONDS)) { "old response was not released" }
                        if (invalidOldToken) return MockResponse().setResponseCode(401)
                            .addHeader("Set-Cookie", "bifrons_share_session=; Path=/; Max-Age=0")
                    }
                    val name = if (old) "old" else "new"
                    return MockResponse().setBody("{\"session_id\":\"$name-session\",\"scope\":\"control\"}")
                        .addHeader("Set-Cookie", "bifrons_share_session=$name-cookie; Path=/")
                }
                if (request.path?.startsWith("/v1/ws/client") == true) {
                    handshakeCookie.set(request.getHeader("Cookie"))
                    return MockResponse().withWebSocketUpgrade(object : WebSocketListener() {
                        override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                            webSocket.close(code, reason)
                        }
                    })
                }
                return MockResponse().setResponseCode(404)
            }
        }
        server.start()
        val baseUrl = server.url("/v1")
        val accountJar = TestCookieJar()
        val accountCookie = Cookie.Builder().name("lingon_access").value("account-cookie")
            .hostOnlyDomain(baseUrl.host).path("/").build()
        accountJar.saveFromResponse(baseUrl, listOf(accountCookie))
        val provider = testHttpClientProvider(accountJar)
        val client = RelayWebSocketClient(provider)
        val executor = Executors.newSingleThreadExecutor()
        var socket: WebSocket? = null
        try {
            val old = executor.submit<Throwable?> {
                runCatching { client.authenticateShareSession(baseUrl, "old-token") }.exceptionOrNull()
            }
            assertTrue("old authentication did not start", oldStarted.await(10, TimeUnit.SECONDS))
            val current = client.authenticateShareSession(baseUrl, "new-token")
            releaseOld.countDown()
            val oldError = old.get(10, TimeUnit.SECONDS)
            if (invalidOldToken) assertEquals(401, (oldError as ApiException).statusCode) else assertNull(oldError)
            val opened = CountDownLatch(1)
            val failure = AtomicReference<Throwable?>()
            socket = client.connect(RelayWebSocketClient.ConnectOptions(
                baseUrl = baseUrl, sessionId = current.session.sessionId, shareToken = "new-token",
                shareAuthentication = current,
                clientId = "isolated-share-test", cols = 80, rows = 24, wantsControl = true,
            ), object : RelayWebSocketClient.Listener {
                override fun onOpen(webSocket: WebSocket) { opened.countDown() }
                override fun onFrame(webSocket: WebSocket, frame: systems.pkt.lingon.protocol.Frame) {}
                override fun onFailure(webSocket: WebSocket, t: Throwable?, response: Response?) {
                    failure.set(t ?: AssertionError("websocket failed"))
                    opened.countDown()
                }
            })
            assertTrue("handshake did not finish", opened.await(10, TimeUnit.SECONDS))
            assertNull(failure.get())
            assertEquals("bifrons_share_session=new-cookie", handshakeCookie.get())
            assertEquals(listOf(accountCookie), accountJar.loadForRequest(baseUrl))
        } finally {
            releaseOld.countDown()
            socket?.cancel()
            executor.shutdownNow()
            assertTrue("authentication executor did not stop", executor.awaitTermination(10, TimeUnit.SECONDS))
            provider.clientFor(baseUrl).connectionPool.evictAll()
            provider.clientFor(baseUrl).dispatcher.executorService.shutdownNow()
            server.shutdown()
        }
    }

    private class TestCookieJar : CookieJar {
        private val cookies = mutableMapOf<String, Cookie>()
        @Synchronized override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
            cookies.forEach {
                if (it.expiresAt <= System.currentTimeMillis()) this.cookies.remove(it.name)
                else this.cookies[it.name] = it
            }
        }
        @Synchronized override fun loadForRequest(url: HttpUrl): List<Cookie> = cookies.values.filter { it.matches(url) }
    }
    @Test
    fun shareAuthenticationPreservesHttpStatusAndRetryAfter() {
        val server = MockWebServer()
        server.enqueue(MockResponse().setResponseCode(429).addHeader("Retry-After", "5"))
        server.start()
        try {
            val client = RelayWebSocketClient(testHttpClientProvider())
            val err = assertThrows(ApiException::class.java) {
                client.authenticateShareSession(server.url("/v1"), "token")
            }
            assertEquals(429, err.statusCode)
            assertEquals(5, err.retryAfterSeconds)
        } finally {
            server.shutdown()
        }
    }
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

    private fun testHttpClientProvider(cookieJar: CookieJar = CookieJar.NO_COOKIES): HttpClientProvider {
        val dataStore = PreferenceDataStoreFactory.create(
            scope = CoroutineScope(Dispatchers.IO + SupervisorJob()),
            produceFile = { Files.createTempFile("lingon-ws-client", ".preferences_pb").toFile() },
        )
        return HttpClientProvider(CertificateStore(dataStore), cookieJar)
    }
}
