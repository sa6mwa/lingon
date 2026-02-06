package systems.pkt.lingon.debug

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.core.stringSetPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import systems.pkt.lingon.data.EndpointStore
import systems.pkt.lingon.data.certs.CertificateStore

private val Context.dataStore by preferencesDataStore(name = "lingon")

class DebugCertReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != ACTION_ADD_CERT) return
        val pem = intent.getStringExtra(EXTRA_PEM)?.trim().orEmpty()
        if (pem.isBlank()) return
        val endpoint = intent.getStringExtra(EXTRA_ENDPOINT)?.trim().orEmpty()
        val pending = goAsync()
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        scope.launch {
            val resolvedEndpoint = if (endpoint.isNotBlank()) {
                endpoint
            } else {
                readEndpoint(context.dataStore)
            }
            context.dataStore.edit { prefs ->
                val saved = (prefs[SAVED_ENDPOINTS_KEY] ?: emptySet()).toMutableSet()
                saved.add(resolvedEndpoint)
                prefs[SAVED_ENDPOINTS_KEY] = saved
            }
            val store = CertificateStore(context.dataStore)
            runCatching { store.addCertificates(resolvedEndpoint, pem) }
            pending.finish()
        }
    }

    private fun readEndpoint(store: androidx.datastore.core.DataStore<Preferences>): String {
        return runBlocking {
            val prefs = store.data.first()
            prefs[ENDPOINT_KEY] ?: EndpointStore.DEFAULT_ENDPOINT
        }
    }

    companion object {
        const val ACTION_ADD_CERT = "systems.pkt.lingon.DEBUG_ADD_CERT"
        const val EXTRA_PEM = "pem"
        const val EXTRA_ENDPOINT = "endpoint"
        private val ENDPOINT_KEY = stringPreferencesKey("api_base_url")
        private val SAVED_ENDPOINTS_KEY = stringSetPreferencesKey("saved_endpoints")
    }
}
