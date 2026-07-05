package systems.pkt.lingon.data

import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Test

class AuthRefreshInterceptorTest {
    @Test
    fun shareAuthRequestBypassesAuthRefresh() {
        val server = MockWebServer()
        server.enqueue(MockResponse().setResponseCode(200).setBody("ok"))
        server.start()
        try {
            val endpoint = server.url("/v1")
            val cookieJar = TestCookieJar()
            val refreshClient = OkHttpClient.Builder().cookieJar(cookieJar).build()
            val client = OkHttpClient.Builder()
                .cookieJar(cookieJar)
                .addInterceptor(authRefreshInterceptor(endpoint, refreshClient, cookieJar, Any()))
                .build()

            val request = Request.Builder()
                .url(server.url("/v1/auth/share"))
                .post("{}".toRequestBody("application/json".toMediaType()))
                .build()
            client.newCall(request).execute().use { response ->
                assertEquals(200, response.code)
            }

            val recorded = server.takeRequest()
            assertEquals("/v1/auth/share", recorded.path)
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun nonShareRequestWithoutRefreshTokenGetsLocalUnauthorized() {
        val server = MockWebServer()
        server.start()
        try {
            val endpoint = server.url("/v1")
            val cookieJar = TestCookieJar()
            val refreshClient = OkHttpClient.Builder().cookieJar(cookieJar).build()
            val client = OkHttpClient.Builder()
                .cookieJar(cookieJar)
                .addInterceptor(authRefreshInterceptor(endpoint, refreshClient, cookieJar, Any()))
                .build()

            client.newCall(Request.Builder().url(server.url("/v1/sessions")).build()).execute().use { response ->
                assertEquals(401, response.code)
            }

            assertEquals(0, server.requestCount)
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun shareSessionWebsocketRequestBypassesAuthRefresh() {
        val server = MockWebServer()
        server.enqueue(MockResponse().setResponseCode(200).setBody("ok"))
        server.start()
        try {
            val endpoint = server.url("/v1")
            val cookieJar = TestCookieJar(
                Cookie.Builder()
                    .name("bifrons_share_session")
                    .value("share-session")
                    .domain(endpoint.host)
                    .path("/")
                    .build(),
            )
            val refreshClient = OkHttpClient.Builder().cookieJar(cookieJar).build()
            val client = OkHttpClient.Builder()
                .cookieJar(cookieJar)
                .addInterceptor(authRefreshInterceptor(endpoint, refreshClient, cookieJar, Any()))
                .build()

            val request = Request.Builder()
                .url(server.url("/v1/ws/client"))
                .header("X-Lingon-Share-Session", "1")
                .build()
            client.newCall(request).execute().use { response ->
                assertEquals(200, response.code)
            }

            val recorded = server.takeRequest()
            assertEquals("/v1/ws/client", recorded.path)
        } finally {
            server.shutdown()
        }
    }

    private class TestCookieJar : CookieJar {
        private val cookies: MutableList<Cookie> = mutableListOf()

        constructor(vararg initial: Cookie) {
            cookies.addAll(initial)
        }

        override fun loadForRequest(url: HttpUrl): List<Cookie> = cookies.toList()

        override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
            this.cookies.addAll(cookies)
        }
    }
}
