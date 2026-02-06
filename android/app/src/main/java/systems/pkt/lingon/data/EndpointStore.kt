package systems.pkt.lingon.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.core.stringSetPreferencesKey
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map

class EndpointStore(
    private val dataStore: DataStore<Preferences>,
    private val scope: CoroutineScope,
) {
    private val endpointKey = stringPreferencesKey("api_base_url")
    private val savedEndpointsKey = stringSetPreferencesKey("saved_endpoints")
    private val overrideFlow = MutableStateFlow<String?>(null)

    private val storedFlow: Flow<String?> = dataStore.data.map { prefs ->
        prefs[endpointKey]
    }
    private val savedFlow: Flow<Set<String>> = dataStore.data.map { prefs ->
        prefs[savedEndpointsKey] ?: emptySet()
    }
    val endpointFlow: Flow<String> = combine(storedFlow, overrideFlow) { stored, override ->
        override ?: stored ?: DEFAULT_ENDPOINT
    }
    val savedEndpointsFlow: Flow<List<String>> = combine(endpointFlow, savedFlow) { current, saved ->
        val cleaned = saved.filter { it.isNotBlank() }.toMutableSet()
        if (current.isNotBlank()) {
            cleaned.add(current)
        }
        val ordered = ArrayList<String>(cleaned.size)
        if (current.isNotBlank()) {
            ordered.add(current)
        }
        cleaned.sorted().forEach { endpoint ->
            if (endpoint != current) {
                ordered.add(endpoint)
            }
        }
        ordered
    }

    suspend fun getEndpoint(): String {
        return endpointFlow.first()
    }

    fun setEndpoint(value: String) {
        val cleaned = value.trim()
        overrideFlow.value = cleaned
        scope.launch(Dispatchers.IO) {
            dataStore.edit { prefs ->
                prefs[endpointKey] = cleaned
                val saved = (prefs[savedEndpointsKey] ?: emptySet()).toMutableSet()
                if (cleaned.isNotBlank()) {
                    saved.add(cleaned)
                    prefs[savedEndpointsKey] = saved
                }
            }
        }
    }

    fun rememberEndpoint(value: String) {
        val cleaned = value.trim()
        if (cleaned.isBlank()) return
        scope.launch(Dispatchers.IO) {
            dataStore.edit { prefs ->
                val saved = (prefs[savedEndpointsKey] ?: emptySet()).toMutableSet()
                saved.add(cleaned)
                prefs[savedEndpointsKey] = saved
            }
        }
    }

    companion object {
        const val DEFAULT_ENDPOINT = "https://localhost:12843/v1"
    }
}
