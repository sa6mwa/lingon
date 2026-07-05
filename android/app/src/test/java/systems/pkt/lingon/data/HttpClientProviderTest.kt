package systems.pkt.lingon.data

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import java.nio.file.Files
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import okhttp3.Cookie
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.Request
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import systems.pkt.lingon.data.certs.CertificateStore

class HttpClientProviderTest {
    @Test
    fun clearPreventsQueuedCookiePersistenceFromRestoringAuth() = runTest {
        val dataStore = PreferenceDataStoreFactory.create(
            scope = backgroundScope,
            produceFile = {
                Files.createTempFile("lingon-cookie-clear", ".preferences_pb").toFile()
            },
        )
        val endpoint = "https://example.test/v1".toHttpUrl()
        val cookieJar = PersistentCookieJar(dataStore, backgroundScope)

        seedCookies(
            cookieJar = cookieJar,
            endpoint = endpoint,
            accessValue = "access",
            accessExpiresAt = System.currentTimeMillis() + 60_000,
            refreshValue = "refresh",
            refreshExpiresAt = System.currentTimeMillis() + 120_000,
        )
        cookieJar.clear()
        advanceUntilIdle()

        val reloaded = PersistentCookieJar(dataStore, backgroundScope)
        val cookies = reloaded.loadForRequest(endpoint)
        assertFalse(cookies.any { it.name == "lingon_access" || it.name == "lingon_refresh" })
    }

    @Test
    fun sessionCookiesAreNotPersistedAcrossJarReload() = runTest {
        val dataStore = PreferenceDataStoreFactory.create(
            scope = backgroundScope,
            produceFile = {
                Files.createTempFile("lingon-session-cookie", ".preferences_pb").toFile()
            },
        )
        val endpoint = "https://example.test/v1".toHttpUrl()
        val cookieJar = PersistentCookieJar(dataStore, backgroundScope)
        val sessionCookie = Cookie.Builder()
            .name("session_only")
            .value("secret")
            .hostOnlyDomain(endpoint.host)
            .path(endpoint.encodedPath)
            .httpOnly()
            .build()

        cookieJar.saveFromResponse(endpoint, listOf(sessionCookie))
        assertTrue(cookieJar.loadForRequest(endpoint).any { it.name == "session_only" })
        advanceUntilIdle()

        val reloaded = PersistentCookieJar(dataStore, backgroundScope)
        assertFalse(reloaded.loadForRequest(endpoint).any { it.name == "session_only" })
    }

    @Test
    fun legacyPersistedSessionCookiesAreSkippedOnLoad() = runTest {
        val dataStore = PreferenceDataStoreFactory.create(
            scope = backgroundScope,
            produceFile = {
                Files.createTempFile("lingon-legacy-session-cookie", ".preferences_pb").toFile()
            },
        )
        val endpoint = "https://example.test/v1".toHttpUrl()
        val legacyRecord = CookieRecord(
            name = "session_only",
            value = "secret",
            domain = endpoint.host,
            path = endpoint.encodedPath,
            expiresAt = Long.MAX_VALUE,
            secure = false,
            httpOnly = true,
            hostOnly = true,
            persistent = false,
        )
        dataStore.edit { prefs ->
            prefs[stringPreferencesKey("cookies_json")] =
                LingonJson.encodeToString(ListSerializer(CookieRecord.serializer()), listOf(legacyRecord))
        }

        val cookieJar = PersistentCookieJar(dataStore, backgroundScope)
        assertFalse(cookieJar.loadForRequest(endpoint).any { it.name == "session_only" })
    }

    @Test
    fun preflightRefreshesWhenAccessTokenIsNearExpiry() {
        val server = MockWebServer()
        val refreshHits = AtomicInteger(0)
        val sessionsHits = AtomicInteger(0)
        val sessionsCookie = AtomicReference<String?>(null)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                return when (request.path) {
                    "/v1/auth/refresh" -> {
                        refreshHits.incrementAndGet()
                        MockResponse()
                            .setResponseCode(200)
                            .addHeader("Set-Cookie", "lingon_access=fresh; Path=/v1; Max-Age=3600; HttpOnly")
                            .setBody("{}")
                    }
                    "/v1/sessions" -> {
                        sessionsHits.incrementAndGet()
                        sessionsCookie.set(request.getHeader("Cookie"))
                        MockResponse().setResponseCode(200).setBody("[]")
                    }
                    else -> MockResponse().setResponseCode(404)
                }
            }
        }
        server.start()
        try {
            val endpoint = server.url("/v1")
            val cookieJar = testCookieJar()
            seedCookies(
                cookieJar = cookieJar,
                endpoint = endpoint,
                accessValue = "stale",
                accessExpiresAt = System.currentTimeMillis() + 5_000,
                refreshValue = "refresh",
                refreshExpiresAt = System.currentTimeMillis() + 60_000,
            )
            val provider = testProvider(cookieJar)

            val client = provider.clientFor(endpoint)
            val response = client.newCall(
                Request.Builder()
                    .url(endpoint.newBuilder().addPathSegments("sessions").build())
                    .get()
                    .build(),
            ).execute()
            response.use {
                assertEquals(200, it.code)
            }

            assertEquals(1, refreshHits.get())
            assertEquals(1, sessionsHits.get())
            assertTrue(sessionsCookie.get().orEmpty().contains("lingon_access=fresh"))
        } finally {
            server.close()
        }
    }

    @Test
    fun doesNotRefreshWhenAccessTokenIsStillFresh() {
        val server = MockWebServer()
        val refreshHits = AtomicInteger(0)
        val sessionsHits = AtomicInteger(0)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                return when (request.path) {
                    "/v1/auth/refresh" -> {
                        refreshHits.incrementAndGet()
                        MockResponse().setResponseCode(200).setBody("{}")
                    }
                    "/v1/sessions" -> {
                        sessionsHits.incrementAndGet()
                        MockResponse().setResponseCode(200).setBody("[]")
                    }
                    else -> MockResponse().setResponseCode(404)
                }
            }
        }
        server.start()
        try {
            val endpoint = server.url("/v1")
            val cookieJar = testCookieJar()
            seedCookies(
                cookieJar = cookieJar,
                endpoint = endpoint,
                accessValue = "fresh",
                accessExpiresAt = System.currentTimeMillis() + 60_000,
                refreshValue = "refresh",
                refreshExpiresAt = System.currentTimeMillis() + 120_000,
            )
            val provider = testProvider(cookieJar)

            val client = provider.clientFor(endpoint)
            val response = client.newCall(
                Request.Builder()
                    .url(endpoint.newBuilder().addPathSegments("sessions").build())
                    .get()
                    .build(),
            ).execute()
            response.use {
                assertEquals(200, it.code)
            }

            assertEquals(0, refreshHits.get())
            assertEquals(1, sessionsHits.get())
        } finally {
            server.close()
        }
    }

    @Test
    fun retriesOnceAfter401WhenRefreshSucceeds() {
        val server = MockWebServer()
        val refreshHits = AtomicInteger(0)
        val sessionsHits = AtomicInteger(0)
        val firstSessionsCookie = AtomicReference<String?>(null)
        val secondSessionsCookie = AtomicReference<String?>(null)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                return when (request.path) {
                    "/v1/auth/refresh" -> {
                        refreshHits.incrementAndGet()
                        MockResponse()
                            .setResponseCode(200)
                            .addHeader("Set-Cookie", "lingon_access=renewed; Path=/v1; Max-Age=3600; HttpOnly")
                            .setBody("{}")
                    }
                    "/v1/sessions" -> {
                        val hit = sessionsHits.incrementAndGet()
                        if (hit == 1) {
                            firstSessionsCookie.set(request.getHeader("Cookie"))
                            MockResponse().setResponseCode(401).setBody("{\"error\":\"unauthorized\"}")
                        } else {
                            secondSessionsCookie.set(request.getHeader("Cookie"))
                            MockResponse().setResponseCode(200).setBody("[]")
                        }
                    }
                    else -> MockResponse().setResponseCode(404)
                }
            }
        }
        server.start()
        try {
            val endpoint = server.url("/v1")
            val cookieJar = testCookieJar()
            seedCookies(
                cookieJar = cookieJar,
                endpoint = endpoint,
                accessValue = "stale",
                accessExpiresAt = System.currentTimeMillis() + 120_000,
                refreshValue = "refresh",
                refreshExpiresAt = System.currentTimeMillis() + 300_000,
            )
            val provider = testProvider(cookieJar)

            val client = provider.clientFor(endpoint)
            val response = client.newCall(
                Request.Builder()
                    .url(endpoint.newBuilder().addPathSegments("sessions").build())
                    .get()
                    .build(),
            ).execute()
            response.use {
                assertEquals(200, it.code)
            }

            assertEquals(1, refreshHits.get())
            assertEquals(2, sessionsHits.get())
            assertTrue(firstSessionsCookie.get().orEmpty().contains("lingon_access=stale"))
            assertTrue(secondSessionsCookie.get().orEmpty().contains("lingon_access=renewed"))
        } finally {
            server.close()
        }
    }

    @Test
    fun doesNotHitProtectedEndpointWhenAccessExpiredAndRefreshMissing() {
        val server = MockWebServer()
        val refreshHits = AtomicInteger(0)
        val sessionsHits = AtomicInteger(0)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                return when (request.path) {
                    "/v1/auth/refresh" -> {
                        refreshHits.incrementAndGet()
                        MockResponse().setResponseCode(401).setBody("{\"error\":\"unauthorized\"}")
                    }
                    "/v1/sessions" -> {
                        sessionsHits.incrementAndGet()
                        MockResponse().setResponseCode(401).setBody("{\"error\":\"unauthorized\"}")
                    }
                    else -> MockResponse().setResponseCode(404)
                }
            }
        }
        server.start()
        try {
            val endpoint = server.url("/v1")
            val cookieJar = testCookieJar()
            seedAccessCookie(
                cookieJar = cookieJar,
                endpoint = endpoint,
                value = "expired",
                expiresAt = System.currentTimeMillis() - 10_000,
            )
            val provider = testProvider(cookieJar)

            val client = provider.clientFor(endpoint)
            val response = client.newCall(
                Request.Builder()
                    .url(endpoint.newBuilder().addPathSegments("sessions").build())
                    .get()
                    .build(),
            ).execute()
            response.use {
                assertEquals(401, it.code)
            }

            assertEquals(0, refreshHits.get())
            assertEquals(0, sessionsHits.get())
        } finally {
            server.close()
        }
    }

    private fun testProvider(cookieJar: PersistentCookieJar): HttpClientProvider {
        val certStore = CertificateStore(testDataStore())
        return HttpClientProvider(certStore, cookieJar)
    }

    private fun testCookieJar(): PersistentCookieJar {
        return PersistentCookieJar(testDataStore(), CoroutineScope(Dispatchers.IO + SupervisorJob()))
    }

    private fun testDataStore() = PreferenceDataStoreFactory.create(
        scope = CoroutineScope(Dispatchers.IO + SupervisorJob()),
        produceFile = {
            Files.createTempFile("lingon-http-client-provider", ".preferences_pb").toFile()
        },
    )

    private fun seedCookies(
        cookieJar: PersistentCookieJar,
        endpoint: HttpUrl,
        accessValue: String,
        accessExpiresAt: Long,
        refreshValue: String,
        refreshExpiresAt: Long,
    ) {
        val path = endpoint.encodedPath.ifBlank { "/" }
        cookieJar.saveFromResponse(
            endpoint,
            listOf(
                Cookie.Builder()
                    .name("lingon_access")
                    .value(accessValue)
                    .hostOnlyDomain(endpoint.host)
                    .path(path)
                    .expiresAt(accessExpiresAt)
                    .httpOnly()
                    .build(),
                Cookie.Builder()
                    .name("lingon_refresh")
                    .value(refreshValue)
                    .hostOnlyDomain(endpoint.host)
                    .path(path)
                    .expiresAt(refreshExpiresAt)
                    .httpOnly()
                    .build(),
            ),
        )
    }

    private fun seedAccessCookie(
        cookieJar: PersistentCookieJar,
        endpoint: HttpUrl,
        value: String,
        expiresAt: Long,
    ) {
        val path = endpoint.encodedPath.ifBlank { "/" }
        cookieJar.saveFromResponse(
            endpoint,
            listOf(
                Cookie.Builder()
                    .name("lingon_access")
                    .value(value)
                    .hostOnlyDomain(endpoint.host)
                    .path(path)
                    .expiresAt(expiresAt)
                    .httpOnly()
                    .build(),
            ),
        )
    }
}
