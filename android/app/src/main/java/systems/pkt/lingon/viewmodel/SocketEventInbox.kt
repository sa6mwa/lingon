package systems.pkt.lingon.viewmodel

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.ClosedSendChannelException
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.yield

// A connection owns one consumer and at most eight pending callback payloads.
// post is called by the WebSocket reader: a full inbox backpressures that reader.
// Main remains free to render or close the connection between ordered events.
internal class SocketEventInbox(scope: CoroutineScope, capacity: Int = 8) {
    private val events = Channel<suspend () -> Unit>(capacity)
    private val consumer = scope.launch(start = CoroutineStart.UNDISPATCHED) {
        for (event in events) {
            event()
            yield()
        }
    }.also { job ->
        job.invokeOnCompletion { events.cancel() }
    }

    fun post(event: suspend () -> Unit) {
        val result = events.trySend(event)
        if (result.isSuccess || result.isClosed) return
        try {
            runBlocking { send(event) }
        } catch (_: ClosedSendChannelException) {
            // The socket became obsolete while its reader was backpressured.
        } catch (_: CancellationException) {
            // Consumer disposal also releases a reader blocked in send.
        }
    }

    suspend fun send(event: suspend () -> Unit) {
        events.send(event)
    }

    fun close() {
        events.cancel()
        consumer.cancel()
    }
}
