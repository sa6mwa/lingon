package systems.pkt.lingon.data.relay

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import okhttp3.HttpUrl
import okhttp3.Request
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import systems.pkt.lingon.data.HttpClientProvider
import systems.pkt.lingon.data.ApiException
import systems.pkt.lingon.data.EndpointStore
import systems.pkt.lingon.data.LingonJson

class RelaySessionsClient(
    private val httpClientProvider: HttpClientProvider,
    private val endpointStore: EndpointStore,
) {
    suspend fun listSessions(): List<RelaySession> {
        val base = baseEndpoint()
        val request = Request.Builder()
            .url(buildUrl(base, "sessions"))
            .get()
            .build()
        return executeSessionsJson(request, base)
    }

    fun streamSessions(): Flow<List<RelaySession>> = emptyFlow()

    suspend fun listWallEvents(sinceId: Long, limit: Int): RelayWallEventsPage {
        val base = baseEndpoint()
        val builder = buildUrl(base, "wall/events").newBuilder()
        if (sinceId > 0) {
            builder.addQueryParameter("since", sinceId.toString())
        }
        if (limit > 0) {
            builder.addQueryParameter("limit", limit.toString())
        }
        val request = Request.Builder()
            .url(builder.build())
            .get()
            .build()
        return executeWallEventsJson(request, base)
    }

    private fun buildUrl(base: HttpUrl, path: String): HttpUrl {
        return base.newBuilder()
            .addPathSegments(path.trimStart('/'))
            .build()
    }

    private suspend fun baseEndpoint(): HttpUrl {
        val rawBase = endpointStore.getEndpoint().trim()
        return rawBase.toHttpUrlOrNull() ?: throw ApiException("invalid endpoint URL")
    }

    private suspend fun executeSessionsJson(request: Request, base: HttpUrl): List<RelaySession> {
        return withContext(Dispatchers.IO) {
            httpClientProvider.clientFor(base).newCall(request).execute().use { response ->
                val body = response.body.string()
                if (response.code == 429) {
                    val retryAfter = response.header("Retry-After")?.toIntOrNull()
                    throw ApiException("rate limited", response.code, retryAfter)
                }
                if (!response.isSuccessful) {
                    throw ApiException("request failed", response.code)
                }
                decodeSessions(body)
            }
        }
    }

    private suspend fun executeWallEventsJson(request: Request, base: HttpUrl): RelayWallEventsPage {
        return withContext(Dispatchers.IO) {
            httpClientProvider.clientFor(base).newCall(request).execute().use { response ->
                val body = response.body.string()
                if (response.code == 429) {
                    val retryAfter = response.header("Retry-After")?.toIntOrNull()
                    throw ApiException("rate limited", response.code, retryAfter)
                }
                if (!response.isSuccessful) {
                    throw ApiException("request failed", response.code)
                }
                decodeWallEvents(body)
            }
        }
    }

    private fun decodeSessions(body: String): List<RelaySession> {
        val trimmed = body.trim()
        if (trimmed.isEmpty() || trimmed == "null") {
            return emptyList()
        }
        return LingonJson.decodeFromString(ListSerializer(RelaySession.serializer()), trimmed)
    }

    private fun decodeWallEvents(body: String): RelayWallEventsPage {
        val trimmed = body.trim()
        if (trimmed.isEmpty() || trimmed == "null") {
            return RelayWallEventsPage()
        }
        return LingonJson.decodeFromString(RelayWallEventsPage.serializer(), trimmed)
    }
}
