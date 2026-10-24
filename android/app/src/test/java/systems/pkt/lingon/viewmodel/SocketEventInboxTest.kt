package systems.pkt.lingon.viewmodel

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class SocketEventInboxTest {
    @Test
    fun slowConsumerBackpressuresAndPreservesEveryEventInOrder() = runTest {
        val release = CompletableDeferred<Unit>()
        val processed = mutableListOf<Int>()
        val inbox = SocketEventInbox(this, capacity = 2)
        var accepted = 0
        val producer = launch {
            for (i in 0 until 100) {
                inbox.send {
                    if (i == 0) release.await()
                    processed.add(i)
                }
                accepted++
            }
        }
        runCurrent()
        assertEquals(3, accepted) // one consumer payload plus two buffered events
        assertFalse(producer.isCompleted)
        assertTrue(processed.isEmpty())
        release.complete(Unit)
        runCurrent()
        assertTrue(producer.isCompleted)
        assertEquals((0 until 100).toList(), processed)
        inbox.close()
    }

    @Test
    fun disposalReleasesBlockedProducerAndDiscardsObsoleteEvents() = runTest {
        val hold = CompletableDeferred<Unit>()
        val processed = mutableListOf<Int>()
        val inbox = SocketEventInbox(this, capacity = 1)
        val producer = launch {
            for (i in 0 until 100) inbox.send { hold.await(); processed.add(i) }
        }
        runCurrent()
        assertFalse(producer.isCompleted)
        inbox.close()
        runCurrent()
        assertTrue(producer.isCancelled)
        assertTrue(processed.isEmpty())
    }
}
