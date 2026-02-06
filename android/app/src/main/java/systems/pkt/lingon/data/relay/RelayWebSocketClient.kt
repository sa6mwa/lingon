package systems.pkt.lingon.data.relay

import com.google.protobuf.ByteString as ProtoByteString
import android.util.Log
import okhttp3.HttpUrl
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString as OkioByteString
import okio.ByteString.Companion.toByteString
import systems.pkt.lingon.data.HttpClientProvider
import systems.pkt.lingon.protocol.Frame
import systems.pkt.lingon.protocol.Hello
import systems.pkt.lingon.protocol.In
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
        val wsUrl = buildWsUrl(options.baseUrl, options.shareToken)
        val request = Request.Builder().url(wsUrl).build()
        val client = httpClientProvider.clientFor(options.baseUrl)
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
                    if (Log.isLoggable("lingon-ws", Log.DEBUG)) {
                        Log.w("lingon-ws", "rx parse failed len=${bytes.size}", parsed.exceptionOrNull())
                    }
                    return
                }
                val frame = parsed.getOrThrow()
                if (Log.isLoggable("lingon-ws", Log.DEBUG)) {
                    Log.d(
                        "lingon-ws",
                        "rx seq=${frame.seq} type=${frameType(frame)} session=${frame.sessionId} len=${bytes.size}",
                    )
                }
                listener.onFrame(webSocket, frame)
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                if (Log.isLoggable("lingon-ws", Log.DEBUG)) {
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

    open fun sendInput(webSocket: WebSocket, data: ByteArray) {
        val hex = data.joinToString(" ") { b -> "%02x".format(b) }
        Log.d("lingon-input", "sendInput bytes=$hex len=${data.size}")
        val input = In.newBuilder().setData(ProtoByteString.copyFrom(data)).build()
        val frame = Frame.newBuilder().setIn(input).build()
        webSocket.send(frame.toOkioByteString())
    }

    open fun sendInput(webSocket: WebSocket, text: String) {
        Log.d("lingon-input", "sendInput text=\"${text}\"")
        sendInput(webSocket, text.toByteArray())
    }

    open fun sendResize(webSocket: WebSocket, cols: Int, rows: Int) {
        val resize = Resize.newBuilder().setCols(cols).setRows(rows).build()
        val frame = Frame.newBuilder().setResize(resize).build()
        webSocket.send(frame.toOkioByteString())
    }

    private fun buildWsUrl(baseUrl: HttpUrl, shareToken: String?): HttpUrl {
        val builder = baseUrl.newBuilder()
            .addPathSegments("ws/client")
        if (!shareToken.isNullOrBlank()) {
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
            frame.hasHello() -> "hello"
            frame.hasIn() -> "in"
            frame.hasResize() -> "resize"
            else -> "unknown"
        }
    }
}

private fun Frame.toOkioByteString(): OkioByteString {
    return toByteArray().toByteString()
}
