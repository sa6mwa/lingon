package systems.pkt.lingon.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import java.nio.file.Files
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class BackgroundWallStoreTest {
    @Test
    fun defaultsToEnabledForExistingInstalls() = runTest {
        val (_, store) = newStore()

        assertTrue(store.enabledFlow.first())
    }

    @Test
    fun persistedDisableIsRespected() = runTest {
        val (dataStore, store) = newStore()
        dataStore.edit { prefs ->
            prefs[booleanPreferencesKey("background_wall_enabled")] = false
        }

        assertFalse(store.enabledFlow.first())
    }

    private fun newStore(): Pair<DataStore<Preferences>, BackgroundWallStore> {
        val dataStore = PreferenceDataStoreFactory.create(
            scope = CoroutineScope(Dispatchers.IO + SupervisorJob()),
            produceFile = { Files.createTempFile("lingon-background-wall", ".preferences_pb").toFile() },
        )
        return dataStore to BackgroundWallStore(dataStore, CoroutineScope(Dispatchers.IO + SupervisorJob()))
    }
}
