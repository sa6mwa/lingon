package systems.pkt.lingon.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch

class TerminalResizeStore(
    private val dataStore: DataStore<Preferences>,
    private val scope: CoroutineScope,
) {
    private val resizeKey = booleanPreferencesKey("terminal_resize_host_enabled")

    val resizeFlow: Flow<Boolean> = dataStore.data.map { prefs ->
        prefs[resizeKey] ?: false
    }

    fun setResizeEnabled(value: Boolean) {
        scope.launch(Dispatchers.IO) {
            dataStore.edit { prefs ->
                prefs[resizeKey] = value
            }
        }
    }
}
