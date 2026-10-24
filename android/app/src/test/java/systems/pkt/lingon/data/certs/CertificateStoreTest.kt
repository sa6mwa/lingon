package systems.pkt.lingon.data.certs

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import java.nio.file.Files
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import systems.pkt.lingon.data.LingonJson

class CertificateStoreTest {
    @Test
    fun listFindsLegacyRawEndpointKeyThroughNormalizedLookup() = runTest {
        val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())
        try {
            val dataStore = PreferenceDataStoreFactory.create(
                scope = scope,
                produceFile = { Files.createTempFile("lingon-certs", ".preferences_pb").toFile() },
            )
            val certificate = StoredCert(
                id = "cert-1",
                pem = "pem",
                subject = "CN=example.test",
                issuer = "CN=test-ca",
                fingerprint = "fingerprint",
                addedAt = "2026-07-13T00:00:00Z",
            )
            dataStore.edit { prefs ->
                prefs[stringPreferencesKey("trusted_certs_json")] = LingonJson.encodeToString(
                    CertificateState.serializer(),
                    CertificateState(mapOf("https://example.test" to listOf(certificate))),
                )
            }

            val store = CertificateStore(dataStore)

            assertEquals(listOf(certificate), store.list("https://example.test/"))
        } finally {
            scope.cancel()
        }
    }
}
