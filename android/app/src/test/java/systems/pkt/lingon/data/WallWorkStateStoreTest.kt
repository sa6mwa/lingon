package systems.pkt.lingon.data

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import java.nio.file.Files
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

class WallWorkStateStoreTest {
    @Test
    fun cursorIsScopedByEndpoint() = runTest {
        val store = newStore()
        store.saveCursor("https://a.example/v1", 42L)
        store.saveCursor("https://b.example/v1", 99L)

        assertEquals(42L, store.loadCursor("https://a.example/v1"))
        assertEquals(99L, store.loadCursor("https://b.example/v1"))
    }

    @Test
    fun clearRemovesCursorState() = runTest {
        val store = newStore()
        store.saveCursor("https://a.example/v1", 99L)
        assertEquals(99L, store.loadCursor("https://a.example/v1"))

        store.clear()
        assertEquals(0L, store.loadCursor("https://a.example/v1"))
    }

    @Test
    fun advanceCursorIsMonotonic() = runTest {
        val store = newStore()

        assertEquals(7L, store.advanceCursor("https://a.example/v1", 7L))
        assertEquals(7L, store.advanceCursor("https://a.example/v1", 3L))
        assertEquals(7L, store.loadCursor("https://a.example/v1"))
        assertEquals(11L, store.advanceCursor("https://a.example/v1", 11L))
        assertEquals(11L, store.loadCursor("https://a.example/v1"))
    }

    @Test
    fun shouldDeliverAndAdvanceSuppressesReplayForSameEndpoint() = runTest {
        val store = newStore()

        assertEquals(true, store.shouldDeliverAndAdvance("https://a.example/v1", 42L))
        assertEquals(false, store.shouldDeliverAndAdvance("https://a.example/v1", 42L))
        assertEquals(true, store.shouldDeliverAndAdvance("https://a.example/v1", 41L))
        assertEquals(true, store.shouldDeliverAndAdvance("https://a.example/v1", 43L))
    }

    @Test
    fun clearCursorRemovesOnlyRequestedEndpoint() = runTest {
        val store = newStore()
        store.saveCursor("https://a.example/v1", 7L)
        store.saveCursor("https://b.example/v1", 9L)

        store.clearCursor("https://a.example/v1")

        assertEquals(0L, store.loadCursor("https://a.example/v1"))
        assertEquals(9L, store.loadCursor("https://b.example/v1"))
    }

    private fun newStore(): WallWorkStateStore {
        val dataStore = PreferenceDataStoreFactory.create(
            scope = CoroutineScope(Dispatchers.IO + SupervisorJob()),
            produceFile = { Files.createTempFile("lingon-wall", ".preferences_pb").toFile() },
        )
        return WallWorkStateStore(dataStore)
    }
}
