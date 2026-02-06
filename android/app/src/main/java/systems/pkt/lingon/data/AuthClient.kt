package systems.pkt.lingon.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.encodeToString
import kotlinx.serialization.decodeFromString
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

class AuthClient(
    private val httpClientProvider: HttpClientProvider,
    private val endpointStore: EndpointStore,
) {
    suspend fun login(username: String, password: String, totp: String): AuthTokens {
        val payload = mapOf(
            "username" to username,
            "password" to password,
            "totp" to totp,
            "client_type" to "android",
        )
        val base = baseEndpoint()
        val request = buildPost(base, "auth/login", payload)
        return executeJson(request, base)
    }

    suspend fun logout() {
        val base = baseEndpoint()
        val request = buildPost(base, "auth/logout", emptyMap<String, String>())
        executeJson<Unit>(request, base)
    }

    suspend fun clearAuth() {
        httpClientProvider.clearCookies()
    }

    suspend fun refresh(): AuthTokens {
        val base = baseEndpoint()
        val request = buildPost(base, "auth/refresh", emptyMap<String, String>())
        return executeJson(request, base)
    }

    private suspend fun buildPost(base: HttpUrl, path: String, payload: Map<String, String>): Request {
        val url = buildUrl(base, path)
        val jsonBody = LingonJson.encodeToString(payload)
        val body = jsonBody.toRequestBody(JSON)
        return Request.Builder().url(url).post(body).build()
    }

    private fun buildUrl(base: HttpUrl, path: String): HttpUrl {
        val builder = base.newBuilder()
        builder.addPathSegments(path.trimStart('/'))
        return builder.build()
    }

    private suspend fun baseEndpoint(): HttpUrl {
        val rawBase = endpointStore.getEndpoint().trim()
        return rawBase.toHttpUrlOrNull() ?: throw ApiException("invalid endpoint URL")
    }

    private suspend inline fun <reified T> executeJson(request: Request, base: HttpUrl): T {
        return withContext(Dispatchers.IO) {
            httpClientProvider.clientFor(base).newCall(request).execute().use { response ->
                val body = response.body.string()
                if (!response.isSuccessful) {
                    val message = runCatching {
                        LingonJson.decodeFromString(ErrorResponse.serializer(), body).error
                    }.getOrNull() ?: "request failed"
                    throw ApiException(message, response.code)
                }
                if (T::class == Unit::class) {
                    @Suppress("UNCHECKED_CAST")
                    return@use Unit as T
                }
                LingonJson.decodeFromString(body)
            }
        }
    }

    companion object {
        private val JSON = "application/json; charset=utf-8".toMediaType()
    }
}

class ApiException(
    message: String,
    val statusCode: Int? = null,
    val retryAfterSeconds: Int? = null,
) : Exception(message)
