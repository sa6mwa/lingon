package systems.pkt.lingon.debug

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.core.stringSetPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

private val Context.dataStore by preferencesDataStore(name = "lingon")

class DebugEndpointReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != ACTION_SET_ENDPOINT) return
        val endpoint = intent.getStringExtra(EXTRA_ENDPOINT)?.trim().orEmpty()
        if (endpoint.isBlank()) return
        val pending = goAsync()
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        scope.launch {
            context.dataStore.edit { prefs ->
                prefs[ENDPOINT_KEY] = endpoint
                val saved = (prefs[SAVED_ENDPOINTS_KEY] ?: emptySet()).toMutableSet()
                saved.add(endpoint)
                prefs[SAVED_ENDPOINTS_KEY] = saved
            }
            pending.finish()
        }
    }

    companion object {
        const val ACTION_SET_ENDPOINT = "systems.pkt.lingon.DEBUG_SET_ENDPOINT"
        const val EXTRA_ENDPOINT = "endpoint"
        private val ENDPOINT_KEY = stringPreferencesKey("api_base_url")
        private val SAVED_ENDPOINTS_KEY = stringSetPreferencesKey("saved_endpoints")
    }
}
