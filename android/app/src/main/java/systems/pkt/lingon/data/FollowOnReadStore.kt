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

class FollowOnReadStore(
    private val dataStore: DataStore<Preferences>,
    private val scope: CoroutineScope,
) {
    val enabledFlow: Flow<Boolean> = dataStore.data.map { prefs ->
        prefs[enabledKey] ?: false
    }

    fun setEnabled(value: Boolean) {
        scope.launch(Dispatchers.IO) {
            dataStore.edit { prefs ->
                prefs[enabledKey] = value
            }
        }
    }

    private companion object {
        val enabledKey = booleanPreferencesKey("follow_on_read_enabled")
    }
}
