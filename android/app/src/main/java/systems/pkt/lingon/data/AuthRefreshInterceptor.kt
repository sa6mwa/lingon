package systems.pkt.lingon.data

import okhttp3.CookieJar
import okhttp3.HttpUrl
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.ResponseBody.Companion.toResponseBody

private const val accessCookieName = "lingon_access"
private const val refreshCookieName = "lingon_refresh"
private const val shareCookieName = "bifrons_share_session"
private const val refreshSkewMillis = 10_000L

private class AuthRefreshInterceptor(
    private val endpoint: HttpUrl,
    private val refreshClient: OkHttpClient,
    private val cookieJar: CookieJar,
    private val refreshLock: Any,
) : Interceptor {
    private enum class RefreshOutcome {
        Fresh,
        MissingRefresh,
        RefreshFailed,
    }

    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        if (isAuthEndpoint(request.url) || isShareSessionWsEndpoint(request)) {
            return chain.proceed(request)
        }

        when (ensureFreshAccess(force = false)) {
            RefreshOutcome.Fresh -> {
            }
            RefreshOutcome.MissingRefresh -> {
                return localUnauthorizedResponse(request, "missing refresh token")
            }
            RefreshOutcome.RefreshFailed -> {
                return localUnauthorizedResponse(request, "refresh failed")
            }
        }

        val response = chain.proceed(request)
        if (response.code != 401 || !canRetry(request)) {
            return response
        }
        if (ensureFreshAccess(force = true) != RefreshOutcome.Fresh) {
            return response
        }

        response.close()
        return chain.proceed(request)
    }

    private fun ensureFreshAccess(force: Boolean): RefreshOutcome {
        synchronized(refreshLock) {
            val now = System.currentTimeMillis()
            val cookies = cookieJar.loadForRequest(endpoint)
            val accessCookie = cookies.firstOrNull { it.name == accessCookieName }
            val refreshCookie = cookies.firstOrNull { it.name == refreshCookieName }
            val accessFresh = accessCookie != null && accessCookie.expiresAt > now + refreshSkewMillis
            val accessUsable = accessCookie != null && accessCookie.expiresAt > now
            if (!force && accessFresh) {
                return RefreshOutcome.Fresh
            }
            if (refreshCookie == null || refreshCookie.expiresAt <= now) {
                return if (accessUsable) RefreshOutcome.Fresh else RefreshOutcome.MissingRefresh
            }
            val refreshed = executeRefresh()
            if (!force) {
                if (refreshed) {
                    return RefreshOutcome.Fresh
                }
                return if (accessUsable) RefreshOutcome.Fresh else RefreshOutcome.RefreshFailed
            }
            return if (refreshed) RefreshOutcome.Fresh else RefreshOutcome.RefreshFailed
        }
    }

    private fun executeRefresh(): Boolean {
        val refreshUrl = endpoint.newBuilder()
            .addPathSegments("auth/refresh")
            .build()
        val request = Request.Builder()
            .url(refreshUrl)
            .post("{}".toRequestBody(jsonMediaType))
            .build()
        return runCatching {
            refreshClient.newCall(request).execute().use { response ->
                response.isSuccessful
            }
        }.getOrDefault(false)
    }

    private fun isAuthEndpoint(url: HttpUrl): Boolean {
        return when {
            url.encodedPath.endsWith("/auth/login") -> true
            url.encodedPath.endsWith("/auth/refresh") -> true
            url.encodedPath.endsWith("/auth/logout") -> true
            url.encodedPath.endsWith("/auth/logout/clients") -> true
            url.encodedPath.endsWith("/auth/share") -> true
            else -> false
        }
    }

    private fun isShareSessionWsEndpoint(request: Request): Boolean {
        val url = request.url
        if (!url.encodedPath.endsWith("/ws/client")) return false
        if (request.header("X-Lingon-Share-Session") != "1") return false
        return cookieJar.loadForRequest(url).any { it.name == shareCookieName && it.value.isNotBlank() }
    }

    private fun canRetry(request: Request): Boolean {
        val body = request.body ?: return true
        return !body.isOneShot()
    }

    private fun localUnauthorizedResponse(request: Request, message: String): Response {
        return Response.Builder()
            .request(request)
            .protocol(Protocol.HTTP_1_1)
            .code(401)
            .message(message)
            .body("".toResponseBody(jsonMediaType))
            .build()
    }

    companion object {
        private val jsonMediaType = "application/json; charset=utf-8".toMediaType()
    }
}

internal fun authRefreshInterceptor(
    endpoint: HttpUrl,
    refreshClient: OkHttpClient,
    cookieJar: CookieJar,
    refreshLock: Any,
): Interceptor {
    return AuthRefreshInterceptor(endpoint, refreshClient, cookieJar, refreshLock)
}
