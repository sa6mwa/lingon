package systems.pkt.lingon.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import kotlinx.coroutines.flow.first

class WallWorkStateStore(
    private val dataStore: DataStore<Preferences>,
) {
    suspend fun loadCursor(endpoint: String): Long {
        val cleaned = endpoint.trim()
        if (cleaned.isBlank()) {
            return 0L
        }
        val prefs = dataStore.data.first()
        val storedEndpoint = prefs[endpointKey].orEmpty()
        if (storedEndpoint != cleaned) {
            return 0L
        }
        return prefs[cursorKey] ?: 0L
    }

    suspend fun saveCursor(endpoint: String, cursor: Long) {
        val cleaned = endpoint.trim()
        if (cleaned.isBlank() || cursor <= 0L) {
            return
        }
        dataStore.edit { prefs ->
            prefs[endpointKey] = cleaned
            prefs[cursorKey] = cursor
        }
    }

    suspend fun clear() {
        dataStore.edit { prefs ->
            prefs.remove(endpointKey)
            prefs.remove(cursorKey)
        }
    }

    private companion object {
        val endpointKey = stringPreferencesKey("wall_work_endpoint")
        val cursorKey = longPreferencesKey("wall_work_cursor")
    }
}
