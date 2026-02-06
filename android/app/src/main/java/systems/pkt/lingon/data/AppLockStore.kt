package systems.pkt.lingon.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.intPreferencesKey
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch

class AppLockStore(
    private val dataStore: DataStore<Preferences>,
    private val scope: CoroutineScope,
) {
    private val timeoutMinutesKey = intPreferencesKey("app_lock_timeout_minutes")

    val timeoutMinutesFlow: Flow<Int> = dataStore.data.map { prefs ->
        normalizeTimeout(prefs[timeoutMinutesKey] ?: DefaultTimeoutMinutes)
    }

    fun setTimeoutMinutes(value: Int) {
        val normalized = normalizeTimeout(value)
        scope.launch(Dispatchers.IO) {
            dataStore.edit { prefs ->
                prefs[timeoutMinutesKey] = normalized
            }
        }
    }

    private fun normalizeTimeout(value: Int): Int {
        return AppLockTimeoutPolicy.normalize(value)
    }

    companion object {
        const val DefaultTimeoutMinutes = AppLockTimeoutPolicy.defaultTimeoutMinutes
    }
}
