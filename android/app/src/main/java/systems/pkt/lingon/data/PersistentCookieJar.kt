package systems.pkt.lingon.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl

class PersistentCookieJar(
    private val dataStore: DataStore<Preferences>,
    private val scope: CoroutineScope,
) : CookieJar {
    private val mutex = Mutex()
    private val lock = Any()
    @Volatile
    private var loaded = false
    private val cookiesKey = stringPreferencesKey("cookies_json")
    private val cookieStore = mutableMapOf<String, MutableList<Cookie>>()

    override fun loadForRequest(url: HttpUrl): List<Cookie> {
        runBlocking {
            ensureLoaded()
        }
        val out: List<Cookie>
        var mutated = false
        synchronized(lock) {
            val now = System.currentTimeMillis()
            val found = mutableListOf<Cookie>()
            val emptyDomains = mutableListOf<String>()
            for ((domain, list) in cookieStore) {
                val iter = list.iterator()
                while (iter.hasNext()) {
                    val cookie = iter.next()
                    if (cookie.expiresAt < now) {
                        iter.remove()
                        mutated = true
                        continue
                    }
                    if (cookie.matches(url)) {
                        found.add(cookie)
                    }
                }
                if (list.isEmpty()) {
                    emptyDomains.add(domain)
                    mutated = true
                }
            }
            if (emptyDomains.isNotEmpty()) {
                emptyDomains.forEach { cookieStore.remove(it) }
            }
            out = found
        }
        if (mutated) {
            persistAsync()
        }
        return out
    }

    override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
        runBlocking {
            ensureLoaded()
        }
        var mutated = false
        synchronized(lock) {
            val now = System.currentTimeMillis()
            cookies.forEach { cookie ->
                val domainKey = cookie.domain
                val list = cookieStore.getOrPut(domainKey) { mutableListOf() }
                val removed = list.removeAll { existing ->
                    existing.name == cookie.name &&
                        existing.domain == cookie.domain &&
                        existing.path == cookie.path
                }
                if (removed) {
                    mutated = true
                }
                if (cookie.expiresAt <= now) {
                    return@forEach
                }
                list.add(cookie)
                mutated = true
            }
        }
        if (mutated) {
            persistAsync()
        }
    }

    private suspend fun ensureLoaded() {
        if (loaded) return
        mutex.withLock {
            if (loaded) return
            val prefs = dataStore.data.first()
            val raw = prefs[cookiesKey]
            if (!raw.isNullOrBlank()) {
                val records = runCatching {
                    LingonJson.decodeFromString(ListSerializer(CookieRecord.serializer()), raw)
                }.getOrNull() ?: emptyList()
                synchronized(lock) {
                    for (record in records) {
                        if (!record.persistent) continue
                        val cookie = record.toCookie() ?: continue
                        val list = cookieStore.getOrPut(cookie.domain) { mutableListOf() }
                        list.add(cookie)
                    }
                }
            }
            loaded = true
        }
    }

    private fun persistAsync() {
        scope.launch(Dispatchers.IO) {
            mutex.withLock {
                val snapshot = synchronized(lock) {
                    cookieStore.values.flatten()
                        .filter { it.persistent }
                        .map { CookieRecord.fromCookie(it) }
                }
                val encoded = LingonJson.encodeToString(ListSerializer(CookieRecord.serializer()), snapshot)
                dataStore.edit { prefs ->
                    if (snapshot.isEmpty()) {
                        prefs.remove(cookiesKey)
                    } else {
                        prefs[cookiesKey] = encoded
                    }
                }
            }
        }
    }

    suspend fun clear() {
        mutex.withLock {
            synchronized(lock) {
                cookieStore.clear()
            }
            dataStore.edit { prefs ->
                prefs.remove(cookiesKey)
            }
            loaded = true
        }
    }
}

@Serializable
data class CookieRecord(
    val name: String,
    val value: String,
    val domain: String,
    val path: String,
    val expiresAt: Long,
    val secure: Boolean,
    val httpOnly: Boolean,
    val hostOnly: Boolean,
    val persistent: Boolean,
) {
    fun toCookie(): Cookie? {
        val builder = Cookie.Builder()
            .name(name)
            .value(value)
            .path(path)
        if (persistent) {
            builder.expiresAt(expiresAt)
        }
        if (hostOnly) {
            builder.hostOnlyDomain(domain)
        } else {
            builder.domain(domain)
        }
        if (secure) builder.secure()
        if (httpOnly) builder.httpOnly()
        return runCatching { builder.build() }.getOrNull()
    }

    companion object {
        fun fromCookie(cookie: Cookie): CookieRecord {
            return CookieRecord(
                name = cookie.name,
                value = cookie.value,
                domain = cookie.domain,
                path = cookie.path,
                expiresAt = cookie.expiresAt,
                secure = cookie.secure,
                httpOnly = cookie.httpOnly,
                hostOnly = cookie.hostOnly,
                persistent = cookie.persistent,
            )
        }
    }
}
