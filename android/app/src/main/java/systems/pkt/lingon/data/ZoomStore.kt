package systems.pkt.lingon.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.floatPreferencesKey
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import systems.pkt.lingon.DefaultTerminalZoom
import systems.pkt.lingon.MaxTerminalZoom
import systems.pkt.lingon.MinTerminalZoom
import java.security.MessageDigest

class ZoomStore(
    private val dataStore: DataStore<Preferences>,
    private val scope: CoroutineScope,
) {
    suspend fun loadZoom(endpoint: String, sessionId: String): Float {
        val key = zoomKey(endpoint, sessionId)
        val prefs = dataStore.data
        return normalize(prefs.first()[key] ?: DefaultTerminalZoom)
    }

    fun saveZoom(endpoint: String, sessionId: String, value: Float) {
        val normalized = normalize(value)
        val key = zoomKey(endpoint, sessionId)
        scope.launch {
            dataStore.edit { prefs ->
                prefs[key] = normalized
            }
        }
    }

    private fun zoomKey(endpoint: String, sessionId: String): Preferences.Key<Float> {
        val digest = MessageDigest.getInstance("SHA-256")
        val raw = "${endpoint.trim()}\n${sessionId.trim()}".toByteArray(Charsets.UTF_8)
        val hashed = digest.digest(raw).joinToString("") { byte -> "%02x".format(byte) }
        return floatPreferencesKey("terminal_zoom_factor_$hashed")
    }

    private fun normalize(value: Float): Float {
        return value.coerceIn(MinTerminalZoom, MaxTerminalZoom)
    }
}
