package systems.pkt.lingon.data

import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Test

class AuthRefreshInterceptorTest {
    @Test
    fun shareTokenWebsocketRequestBypassesAuthRefresh() {
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

            client.newCall(Request.Builder().url(server.url("/v1/ws/client?token=abc")).build()).execute().use { response ->
                assertEquals(200, response.code)
            }

            val request = server.takeRequest()
            assertEquals("/v1/ws/client?token=abc", request.path)
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

    private class TestCookieJar : CookieJar {
        override fun loadForRequest(url: HttpUrl): List<Cookie> = emptyList()

        override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
            // no-op for tests
        }
    }
}
