package systems.pkt.lingon.data

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import java.nio.file.Files
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

class WallWorkStateStoreTest {
    @Test
    fun cursorIsScopedByEndpoint() = runTest {
        val dataStore = PreferenceDataStoreFactory.create(
            scope = CoroutineScope(Dispatchers.IO + SupervisorJob()),
            produceFile = { Files.createTempFile("lingon-wall", ".preferences_pb").toFile() },
        )
        val store = WallWorkStateStore(dataStore)
        store.saveCursor("https://a.example/v1", 42L)

        assertEquals(42L, store.loadCursor("https://a.example/v1"))
        assertEquals(0L, store.loadCursor("https://b.example/v1"))
    }

    @Test
    fun clearRemovesCursorState() = runTest {
        val dataStore = PreferenceDataStoreFactory.create(
            scope = CoroutineScope(Dispatchers.IO + SupervisorJob()),
            produceFile = { Files.createTempFile("lingon-wall", ".preferences_pb").toFile() },
        )
        val store = WallWorkStateStore(dataStore)
        store.saveCursor("https://a.example/v1", 99L)
        assertEquals(99L, store.loadCursor("https://a.example/v1"))

        store.clear()
        assertEquals(0L, store.loadCursor("https://a.example/v1"))
    }
}
