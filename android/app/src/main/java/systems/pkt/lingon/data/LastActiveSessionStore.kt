package systems.pkt.lingon.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch

class LastActiveSessionStore(
    private val dataStore: DataStore<Preferences>,
    private val scope: CoroutineScope,
) {
    suspend fun load(endpoint: String): String? {
        val cleanedEndpoint = endpoint.trim()
        if (cleanedEndpoint.isBlank()) return null
        val prefs = dataStore.data.first()
        val storedEndpoint = prefs[endpointKey].orEmpty()
        if (storedEndpoint != cleanedEndpoint) return null
        return prefs[sessionIdKey]?.trim()?.ifBlank { null }
    }

    suspend fun save(endpoint: String, sessionId: String) {
        val cleanedEndpoint = endpoint.trim()
        val cleanedSessionId = sessionId.trim()
        if (cleanedEndpoint.isBlank() || cleanedSessionId.isBlank()) return
        dataStore.edit { prefs ->
            prefs[endpointKey] = cleanedEndpoint
            prefs[sessionIdKey] = cleanedSessionId
        }
    }

    fun saveAsync(endpoint: String, sessionId: String) {
        scope.launch {
            save(endpoint, sessionId)
        }
    }

    suspend fun clear() {
        dataStore.edit { prefs ->
            prefs.remove(endpointKey)
            prefs.remove(sessionIdKey)
        }
    }

    fun clearAsync() {
        scope.launch {
            clear()
        }
    }

    private companion object {
        val endpointKey = stringPreferencesKey("last_active_session_endpoint")
        val sessionIdKey = stringPreferencesKey("last_active_session_id")
    }
}
