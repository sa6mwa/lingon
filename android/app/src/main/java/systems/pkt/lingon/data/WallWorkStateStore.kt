package systems.pkt.lingon.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import kotlinx.coroutines.flow.first
import java.util.Base64

class WallWorkStateStore(
    private val dataStore: DataStore<Preferences>,
) {
    suspend fun loadCursor(endpoint: String): Long {
        val cleaned = endpoint.trim()
        if (cleaned.isBlank()) {
            return 0L
        }
        val prefs = dataStore.data.first()
        return parseCursorMap(prefs[cursorsKey])[cleaned] ?: 0L
    }

    suspend fun saveCursor(endpoint: String, cursor: Long): Long {
        return advanceCursor(endpoint, cursor)
    }

    suspend fun advanceCursor(endpoint: String, cursor: Long): Long {
        val cleaned = endpoint.trim()
        if (cleaned.isBlank() || cursor <= 0L) {
            return 0L
        }
        var nextCursor = 0L
        dataStore.edit { prefs ->
            val cursorMap = parseCursorMap(prefs[cursorsKey])
            val current = cursorMap[cleaned] ?: 0L
            nextCursor = maxOf(current, cursor)
            cursorMap[cleaned] = nextCursor
            prefs[cursorsKey] = encodeCursorMap(cursorMap)
        }
        return nextCursor
    }

    suspend fun shouldDeliverAndAdvance(endpoint: String, eventId: Long): Boolean {
        val cleaned = endpoint.trim()
        if (cleaned.isBlank() || eventId <= 0L) {
            return true
        }
        var shouldDeliver = false
        dataStore.edit { prefs ->
            val cursorMap = parseCursorMap(prefs[cursorsKey])
            val current = cursorMap[cleaned] ?: 0L
            if (eventId > current) {
                cursorMap[cleaned] = eventId
                prefs[cursorsKey] = encodeCursorMap(cursorMap)
                shouldDeliver = true
            }
        }
        return shouldDeliver
    }

    suspend fun claimDelivery(endpoint: String, eventId: Long): WallDeliveryClaim? {
        val cleaned = endpoint.trim()
        if (cleaned.isBlank() || eventId <= 0L) {
            return WallDeliveryClaim(cleaned, eventId, previousCursor = 0L, cursorBacked = false)
        }
        var claim: WallDeliveryClaim? = null
        dataStore.edit { prefs ->
            val cursorMap = parseCursorMap(prefs[cursorsKey])
            val current = cursorMap[cleaned] ?: 0L
            if (eventId > current) {
                cursorMap[cleaned] = eventId
                prefs[cursorsKey] = encodeCursorMap(cursorMap)
                claim = WallDeliveryClaim(cleaned, eventId, previousCursor = current, cursorBacked = true)
            }
        }
        return claim
    }

    suspend fun rollbackDeliveryClaim(claim: WallDeliveryClaim) {
        if (!claim.cursorBacked) {
            return
        }
        dataStore.edit { prefs ->
            val cursorMap = parseCursorMap(prefs[cursorsKey])
            if (cursorMap[claim.endpoint] != claim.eventId) {
                return@edit
            }
            if (claim.previousCursor > 0L) {
                cursorMap[claim.endpoint] = claim.previousCursor
            } else {
                cursorMap.remove(claim.endpoint)
            }
            prefs[cursorsKey] = encodeCursorMap(cursorMap)
        }
    }

    suspend fun shouldDeliver(endpoint: String, eventId: Long): Boolean {
        val cleaned = endpoint.trim()
        if (cleaned.isBlank() || eventId <= 0L) {
            return true
        }
        val prefs = dataStore.data.first()
        val current = parseCursorMap(prefs[cursorsKey])[cleaned] ?: 0L
        return eventId > current
    }

    suspend fun recordDelivered(endpoint: String, eventId: Long) {
        val cleaned = endpoint.trim()
        if (cleaned.isBlank() || eventId <= 0L) {
            return
        }
        dataStore.edit { prefs ->
            val cursorMap = parseCursorMap(prefs[cursorsKey])
            cursorMap[cleaned] = maxOf(cursorMap[cleaned] ?: 0L, eventId)
            prefs[cursorsKey] = encodeCursorMap(cursorMap)
        }
    }

    suspend fun clearCursor(endpoint: String) {
        val cleaned = endpoint.trim()
        if (cleaned.isBlank()) {
            return
        }
        dataStore.edit { prefs ->
            val cursorMap = parseCursorMap(prefs[cursorsKey])
            if (cursorMap.remove(cleaned) != null) {
                prefs[cursorsKey] = encodeCursorMap(cursorMap)
            }
        }
    }

    suspend fun clear() {
        dataStore.edit { prefs ->
            prefs.remove(cursorsKey)
        }
    }

    private companion object {
        val cursorsKey = stringPreferencesKey("wall_work_cursors")

        fun parseCursorMap(raw: String?): MutableMap<String, Long> {
            if (raw.isNullOrBlank()) {
                return mutableMapOf()
            }
            val parsed = mutableMapOf<String, Long>()
            raw.lineSequence().forEach { line ->
                val trimmed = line.trim()
                if (trimmed.isBlank()) {
                    return@forEach
                }
                val separator = trimmed.indexOf('=')
                if (separator <= 0 || separator >= trimmed.lastIndex) {
                    return@forEach
                }
                val encodedEndpoint = trimmed.substring(0, separator)
                val endpoint = runCatching {
                    String(Base64.getUrlDecoder().decode(encodedEndpoint))
                }.getOrNull()?.trim().orEmpty()
                if (endpoint.isBlank()) {
                    return@forEach
                }
                val value = trimmed.substring(separator + 1).toLongOrNull() ?: 0L
                if (value > 0L) {
                    parsed[endpoint] = value
                }
            }
            return parsed
        }

        fun encodeCursorMap(values: Map<String, Long>): String {
            if (values.isEmpty()) {
                return ""
            }
            return values.entries
                .asSequence()
                .filter { it.key.isNotBlank() && it.value > 0L }
                .sortedBy { it.key }
                .joinToString(separator = "\n") { (endpoint, cursor) ->
                    val encodedEndpoint = Base64.getUrlEncoder()
                        .withoutPadding()
                        .encodeToString(endpoint.trim().toByteArray())
                    "$encodedEndpoint=$cursor"
                }
        }
    }
}

class WallDeliveryClaim internal constructor(
    val endpoint: String,
    val eventId: Long,
    val previousCursor: Long,
    internal val cursorBacked: Boolean,
)
