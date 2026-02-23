package systems.pkt.lingon.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.floatPreferencesKey
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import systems.pkt.lingon.DefaultTerminalZoom
import systems.pkt.lingon.MaxTerminalZoom
import systems.pkt.lingon.MinTerminalZoom

class ZoomStore(
    private val dataStore: DataStore<Preferences>,
    private val scope: CoroutineScope,
) {
    private val zoomKey = floatPreferencesKey("terminal_zoom_factor")

    val zoomFlow: Flow<Float> = dataStore.data.map { prefs ->
        normalize(prefs[zoomKey] ?: DefaultTerminalZoom)
    }

    suspend fun getZoom(): Float {
        return zoomFlow.first()
    }

    fun setZoom(value: Float) {
        val normalized = normalize(value)
        scope.launch {
            dataStore.edit { prefs ->
                prefs[zoomKey] = normalized
            }
        }
    }

    private fun normalize(value: Float): Float {
        return value.coerceIn(MinTerminalZoom, MaxTerminalZoom)
    }
}
