package systems.pkt.lingon.data.certs

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import java.io.ByteArrayInputStream
import java.security.MessageDigest
import java.security.cert.CertificateException
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.time.Instant
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.Serializable
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import systems.pkt.lingon.data.LingonJson

class CertificateStore(
    private val dataStore: DataStore<Preferences>,
) {
    private val certsKey = stringPreferencesKey("trusted_certs_json")

    val certificatesFlow: Flow<Map<String, List<StoredCert>>> = dataStore.data.map { prefs ->
        val raw = prefs[certsKey].orEmpty()
        if (raw.isBlank()) {
            emptyMap()
        } else {
            runCatching {
                normalizeCertificateEndpoints(
                    LingonJson.decodeFromString(CertificateState.serializer(), raw).endpoints,
                )
            }.getOrElse { emptyMap() }
        }
    }

    suspend fun list(endpoint: String): List<StoredCert> {
        val state = loadState()
        val key = normalizeCertificateEndpointKey(endpoint)
        val rawKey = endpoint.trim()
        return (state.endpoints[key].orEmpty() + state.endpoints[rawKey].orEmpty()).distinctBy { it.id }
    }

    fun listBlocking(endpoint: String): List<StoredCert> = runBlocking { list(endpoint) }

    suspend fun addCertificates(endpoint: String, pem: String): List<StoredCert> {
        val key = normalizeCertificateEndpointKey(endpoint)
        val rawKey = endpoint.trim()
        val certs = parseCertificates(pem)
        if (certs.isEmpty()) {
            throw CertificateException("no certificates found")
        }
        val now = Instant.now().toString()
        val additions = certs.map { cert ->
            val fingerprint = sha256Fingerprint(cert)
            StoredCert(
                id = fingerprint,
                pem = encodePem(cert),
                subject = cert.subjectX500Principal.name.orEmpty(),
                issuer = cert.issuerX500Principal.name.orEmpty(),
                fingerprint = fingerprint,
                addedAt = now,
            )
        }
        val state = loadState()
        val existing = (state.endpoints[key].orEmpty() + state.endpoints[rawKey].orEmpty()).associateBy { it.id }
        val merged = LinkedHashMap<String, StoredCert>(existing)
        additions.forEach { cert ->
            merged.putIfAbsent(cert.id, cert)
        }
        val updated = state.endpoints.toMutableMap()
        updated.remove(rawKey)
        updated[key] = merged.values.toList()
        persistState(CertificateState(updated))
        return merged.values.toList()
    }

    suspend fun removeCertificate(endpoint: String, certId: String) {
        val key = normalizeCertificateEndpointKey(endpoint)
        val rawKey = endpoint.trim()
        val state = loadState()
        val existing = (state.endpoints[key].orEmpty() + state.endpoints[rawKey].orEmpty()).distinctBy { it.id }
        val updatedList = existing.filterNot { it.id == certId }
        val updated = state.endpoints.toMutableMap()
        updated.remove(rawKey)
        if (updatedList.isEmpty()) {
            updated.remove(key)
        } else {
            updated[key] = updatedList
        }
        persistState(CertificateState(updated))
    }

    private suspend fun loadState(): CertificateState {
        val prefs = dataStore.data.first()
        val raw = prefs[certsKey].orEmpty()
        if (raw.isBlank()) {
            return CertificateState(emptyMap())
        }
        return runCatching {
            val decoded = LingonJson.decodeFromString(CertificateState.serializer(), raw)
            CertificateState(normalizeCertificateEndpoints(decoded.endpoints))
        }.getOrElse { CertificateState(emptyMap()) }
    }

    private suspend fun persistState(state: CertificateState) {
        val encoded = LingonJson.encodeToString(CertificateState.serializer(), state)
        dataStore.edit { prefs ->
            prefs[certsKey] = encoded
        }
    }

    private fun parseCertificates(pem: String): List<X509Certificate> {
        val factory = CertificateFactory.getInstance("X.509")
        val input = ByteArrayInputStream(pem.toByteArray(Charsets.UTF_8))
        val parsed = factory.generateCertificates(input)
        return parsed.filterIsInstance<X509Certificate>()
    }

    private fun encodePem(cert: X509Certificate): String {
        val base64 = java.util.Base64.getMimeEncoder(64, "\n".toByteArray())
            .encodeToString(cert.encoded)
        return "-----BEGIN CERTIFICATE-----\n$base64\n-----END CERTIFICATE-----\n"
    }

    private fun sha256Fingerprint(cert: X509Certificate): String {
        val digest = MessageDigest.getInstance("SHA-256").digest(cert.encoded)
        return digest.joinToString("") { b -> "%02x".format(b) }
    }

}

internal fun normalizeCertificateEndpointKey(endpoint: String): String {
    val trimmed = endpoint.trim()
    return trimmed.toHttpUrlOrNull()?.toString() ?: trimmed
}

private fun normalizeCertificateEndpoints(
    endpoints: Map<String, List<StoredCert>>,
): Map<String, List<StoredCert>> {
    val normalized = LinkedHashMap<String, LinkedHashMap<String, StoredCert>>()
    endpoints.forEach { (endpoint, certs) ->
        val merged = normalized.getOrPut(normalizeCertificateEndpointKey(endpoint)) { LinkedHashMap() }
        certs.forEach { cert -> merged.putIfAbsent(cert.id, cert) }
    }
    return normalized.mapValues { (_, certs) -> certs.values.toList() }
}

@Serializable
data class CertificateState(
    val endpoints: Map<String, List<StoredCert>> = emptyMap(),
)

@Serializable
data class StoredCert(
    val id: String,
    val pem: String,
    val subject: String,
    val issuer: String,
    val fingerprint: String,
    val addedAt: String,
)

@Serializable
data class TrustedCert(
    val id: String,
    val subject: String,
    val issuer: String,
    val fingerprint: String,
    val addedAt: String,
)

fun StoredCert.toTrusted(): TrustedCert {
    return TrustedCert(
        id = id,
        subject = subject,
        issuer = issuer,
        fingerprint = fingerprint,
        addedAt = addedAt,
    )
}
