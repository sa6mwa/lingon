package systems.pkt.lingon.data

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import java.nio.file.Files
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class LastActiveSessionStoreTest {
    @Test
    fun loadReturnsSessionWhenEndpointMatches() = runTest {
        val dataStore = PreferenceDataStoreFactory.create(
            scope = backgroundScope,
            produceFile = { Files.createTempFile("lingon-last-active-session", ".preferences_pb").toFile() },
        )
        val store = LastActiveSessionStore(dataStore, backgroundScope)

        store.save("https://a.example/v1", "session-a")

        assertEquals("session-a", store.load("https://a.example/v1"))
    }

    @Test
    fun loadReturnsNullForDifferentEndpoint() = runTest {
        val dataStore = PreferenceDataStoreFactory.create(
            scope = backgroundScope,
            produceFile = { Files.createTempFile("lingon-last-active-session", ".preferences_pb").toFile() },
        )
        val store = LastActiveSessionStore(dataStore, backgroundScope)

        store.save("https://a.example/v1", "session-a")

        assertNull(store.load("https://b.example/v1"))
    }

    @Test
    fun clearRemovesStoredState() = runTest {
        val dataStore = PreferenceDataStoreFactory.create(
            scope = backgroundScope,
            produceFile = { Files.createTempFile("lingon-last-active-session", ".preferences_pb").toFile() },
        )
        val store = LastActiveSessionStore(dataStore, backgroundScope)

        store.save("https://a.example/v1", "session-a")
        assertEquals("session-a", store.load("https://a.example/v1"))

        store.clear()
        assertNull(store.load("https://a.example/v1"))
    }
}
