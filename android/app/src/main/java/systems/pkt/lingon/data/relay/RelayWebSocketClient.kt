package systems.pkt.lingon.data.relay

import com.google.protobuf.ByteString as ProtoByteString
import android.util.Log
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString as OkioByteString
import okio.ByteString.Companion.toByteString
import systems.pkt.lingon.data.HttpClientProvider
import systems.pkt.lingon.data.LingonJson
import systems.pkt.lingon.protocol.Frame
import systems.pkt.lingon.protocol.Hello
import systems.pkt.lingon.protocol.In
import systems.pkt.lingon.protocol.Command
import systems.pkt.lingon.protocol.CommandKind
import systems.pkt.lingon.protocol.Resize

open class RelayWebSocketClient(private val httpClientProvider: HttpClientProvider) {
    data class ConnectOptions(
        val baseUrl: HttpUrl,
        val sessionId: String?,
        val shareToken: String? = null,
        val clientId: String,
        val cols: Int,
        val rows: Int,
        val wantsControl: Boolean,
        val lastSeq: Long = 0,
        val clientType: String = "android",
    )

    interface Listener {
        fun onOpen(webSocket: WebSocket) {}
        fun onFrame(webSocket: WebSocket, frame: Frame)
        fun onFailure(webSocket: WebSocket, t: Throwable?, response: Response?) {}
        fun onClosed(webSocket: WebSocket, code: Int, reason: String?) {}
    }

    open fun connect(options: ConnectOptions, listener: Listener): WebSocket {
        val client = httpClientProvider.clientFor(options.baseUrl)
        val request = buildWebSocketRequest(options)
        return client.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                val hello = Hello.newBuilder()
                    .setClientId(options.clientId)
                    .setCols(options.cols)
                    .setRows(options.rows)
                    .setWantsControl(options.wantsControl)
                    .setLastSeq(options.lastSeq)
                    .setClientType(options.clientType)
                    .build()
                val frameBuilder = Frame.newBuilder().setHello(hello)
                if (!options.sessionId.isNullOrBlank()) {
                    frameBuilder.sessionId = options.sessionId
                }
                val frame = frameBuilder.build()
                webSocket.send(frame.toOkioByteString())
                listener.onOpen(webSocket)
            }

            override fun onMessage(webSocket: WebSocket, bytes: OkioByteString) {
                val parsed = runCatching { Frame.parseFrom(bytes.toByteArray()) }
                if (parsed.isFailure) {
                    if (isLoggable("lingon-ws", Log.DEBUG)) {
                        Log.w("lingon-ws", "rx parse failed len=${bytes.size}", parsed.exceptionOrNull())
                    }
                    return
                }
                val frame = parsed.getOrThrow()
                if (isLoggable("lingon-ws", Log.DEBUG)) {
                    Log.d(
                        "lingon-ws",
                        "rx seq=${frame.seq} type=${frameType(frame)} session=${frame.sessionId} len=${bytes.size}",
                    )
                }
                listener.onFrame(webSocket, frame)
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                if (isLoggable("lingon-ws", Log.DEBUG)) {
                    Log.w("lingon-ws", "rx text len=${text.length}")
                }
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                listener.onFailure(webSocket, t, response)
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                listener.onClosed(webSocket, code, reason)
            }
        })
    }

    internal fun buildWebSocketRequest(options: ConnectOptions): Request {
        val requestBuilder = Request.Builder().url(buildWsUrl(options.baseUrl, options.shareToken))
        if (!options.shareToken.isNullOrBlank()) {
            requestBuilder.header("X-Lingon-Share-Session", "1")
        }
        return requestBuilder.build()
    }

    open fun sendInput(webSocket: WebSocket, data: ByteArray) {
        val input = In.newBuilder().setData(ProtoByteString.copyFrom(data)).build()
        val frame = Frame.newBuilder().setIn(input).build()
        webSocket.send(frame.toOkioByteString())
    }

    open fun sendInput(webSocket: WebSocket, text: String) {
        sendInput(webSocket, text.toByteArray())
    }

    open fun sendResize(webSocket: WebSocket, cols: Int, rows: Int) {
        val resize = Resize.newBuilder().setCols(cols).setRows(rows).build()
        val frame = Frame.newBuilder().setResize(resize).build()
        webSocket.send(frame.toOkioByteString())
    }

    open fun sendCommand(webSocket: WebSocket, kind: CommandKind) {
        if (kind == CommandKind.COMMAND_KIND_UNSPECIFIED) return
        val command = Command.newBuilder().setKind(kind).build()
        val frame = Frame.newBuilder().setCommand(command).build()
        webSocket.send(frame.toOkioByteString())
    }

    open fun authenticateShareSession(baseUrl: HttpUrl, shareToken: String): RelayShareSession {
        val client = httpClientProvider.clientFor(baseUrl)
        val url = baseUrl.newBuilder()
            .addPathSegments("auth/share")
            .build()
        val body = LingonJson.encodeToString(ShareAuthRequest(token = shareToken))
        val request = Request.Builder()
            .url(url)
            .post(body.toRequestBody(jsonMediaType))
            .build()
        client.newCall(request).execute().use { response ->
            if (!response.isSuccessful) {
                throw IllegalStateException("share authentication failed: HTTP ${response.code}")
            }
            val raw = response.body?.string().orEmpty()
            val shareSession = LingonJson.decodeFromString<RelayShareSession>(raw)
            if (shareSession.sessionId.isBlank()) {
                throw IllegalStateException("share authentication failed: missing session")
            }
            return shareSession
        }
    }

    private fun buildWsUrl(baseUrl: HttpUrl, shareToken: String?): HttpUrl {
        val builder = baseUrl.newBuilder()
            .addPathSegments("ws/client")
        if (!baseUrl.isHttps && !shareToken.isNullOrBlank()) {
            builder.addQueryParameter("token", shareToken)
        }
        return builder.build()
    }

    private fun frameType(frame: Frame): String {
        return when {
            frame.hasSnapshot() -> "snapshot"
            frame.hasDiff() -> "diff"
            frame.hasWelcome() -> "welcome"
            frame.hasControl() -> "control"
            frame.hasScrollback() -> "scrollback"
            frame.hasSessions() -> "sessions"
            frame.hasError() -> "error"
            frame.hasOut() -> "out"
            frame.hasCommand() -> "command"
            frame.hasWallInactivityStatus() -> "wall_inactivity_status"
            frame.hasHello() -> "hello"
            frame.hasIn() -> "in"
            frame.hasResize() -> "resize"
            else -> "unknown"
        }
    }

    private fun isLoggable(tag: String, level: Int): Boolean {
        return runCatching { Log.isLoggable(tag, level) }.getOrDefault(false)
    }

    companion object {
        private val jsonMediaType = "application/json; charset=utf-8".toMediaType()
    }
}

@Serializable
private data class ShareAuthRequest(
    val token: String,
)

private fun Frame.toOkioByteString(): OkioByteString {
    return toByteArray().toByteString()
}
