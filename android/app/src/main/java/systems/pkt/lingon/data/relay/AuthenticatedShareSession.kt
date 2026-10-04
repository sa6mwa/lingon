package systems.pkt.lingon.data.relay

import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl
import okhttp3.OkHttpClient

/** Share metadata and the private transport that received its authentication cookie. */
class AuthenticatedShareSession internal constructor(
    val session: RelayShareSession,
    private val endpoint: HttpUrl,
    private val token: String,
    private val client: OkHttpClient,
) {
    internal fun clientFor(options: RelayWebSocketClient.ConnectOptions): OkHttpClient {
        require(options.baseUrl == endpoint && options.shareToken == token &&
            options.sessionId == session.sessionId.trim()) {
            "share authentication does not match the selected connection"
        }
        return client
    }
}

/** One grant per authentication attempt; account cookies never enter this jar. */
internal class ShareSessionCookieJar(
    private val nowMillis: () -> Long = System::currentTimeMillis,
) : CookieJar {
    @Volatile private var grant: Cookie? = null

    override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
        val next = cookies.lastOrNull { it.name == "bifrons_share_session" } ?: return
        grant = next.takeIf { it.expiresAt > nowMillis() }
    }

    override fun loadForRequest(url: HttpUrl): List<Cookie> {
        val current = grant ?: return emptyList()
        return if (current.expiresAt > nowMillis() && current.matches(url)) listOf(current) else emptyList()
    }
}
