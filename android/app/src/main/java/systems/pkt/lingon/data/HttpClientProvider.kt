package systems.pkt.lingon.data

import java.security.KeyStore
import java.security.SecureRandom
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import java.time.Duration
import javax.net.ssl.SSLContext
import javax.net.ssl.TrustManager
import javax.net.ssl.TrustManagerFactory
import javax.net.ssl.X509TrustManager
import okhttp3.CookieJar
import okhttp3.OkHttpClient
import okhttp3.HttpUrl
import systems.pkt.lingon.data.certs.CertificateStore

class HttpClientProvider(
    private val certificateStore: CertificateStore,
    private val cookieJar: CookieJar,
) {
    private val cache = mutableMapOf<String, CachedClient>()
    private val refreshLocks = mutableMapOf<String, Any>()
    private val lock = Any()

    fun clientFor(endpoint: HttpUrl): OkHttpClient {
        val key = endpoint.toString()
        val certs = certificateStore.listBlocking(key)
        val fingerprintKey = certs.joinToString("|") { it.fingerprint }
        synchronized(lock) {
            val cached = cache[key]
            if (cached != null && cached.fingerprintKey == fingerprintKey) {
                return cached.client
            }
        }
        val trustManager = compositeTrustManager(certs.mapNotNull { parseCert(it.pem) })
        val sslContext = SSLContext.getInstance("TLS")
        sslContext.init(null, arrayOf<TrustManager>(trustManager), SecureRandom())
        val refreshClient = OkHttpClient.Builder()
            .sslSocketFactory(sslContext.socketFactory, trustManager)
            .cookieJar(cookieJar)
            .connectTimeout(Duration.ofSeconds(5))
            .readTimeout(Duration.ofSeconds(15))
            .writeTimeout(Duration.ofSeconds(15))
            .callTimeout(Duration.ofSeconds(20))
            .build()
        val client = refreshClient.newBuilder()
            .addInterceptor(
                authRefreshInterceptor(
                    endpoint = endpoint,
                    refreshClient = refreshClient,
                    cookieJar = cookieJar,
                    refreshLock = refreshLockFor(key),
                ),
            )
            .build()
        synchronized(lock) {
            cache[key] = CachedClient(client, fingerprintKey)
        }
        return client
    }

    suspend fun clearCookies() {
        if (cookieJar is PersistentCookieJar) {
            cookieJar.clear()
        }
    }

    private fun compositeTrustManager(certs: List<X509Certificate>): X509TrustManager {
        val managers = mutableListOf<X509TrustManager>()
        managers.add(systemTrustManager())
        if (certs.isNotEmpty()) {
            managers.add(customTrustManager(certs))
        }
        return CompositeTrustManager(managers)
    }

    private fun systemTrustManager(): X509TrustManager {
        val factory = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm())
        factory.init(null as KeyStore?)
        return factory.trustManagers.filterIsInstance<X509TrustManager>().first()
    }

    private fun customTrustManager(certs: List<X509Certificate>): X509TrustManager {
        val keyStore = KeyStore.getInstance(KeyStore.getDefaultType())
        keyStore.load(null)
        certs.forEachIndexed { idx, cert ->
            keyStore.setCertificateEntry("cert-$idx", cert)
        }
        val factory = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm())
        factory.init(keyStore)
        return factory.trustManagers.filterIsInstance<X509TrustManager>().first()
    }

    private fun parseCert(pem: String): X509Certificate? {
        return try {
            val factory = java.security.cert.CertificateFactory.getInstance("X.509")
            val input = java.io.ByteArrayInputStream(pem.toByteArray(Charsets.UTF_8))
            factory.generateCertificate(input) as? X509Certificate
        } catch (err: CertificateException) {
            null
        }
    }

    private fun refreshLockFor(endpoint: String): Any {
        synchronized(lock) {
            return refreshLocks.getOrPut(endpoint) { Any() }
        }
    }

    private data class CachedClient(
        val client: OkHttpClient,
        val fingerprintKey: String,
    )
}

private class CompositeTrustManager(
    private val delegates: List<X509TrustManager>,
) : X509TrustManager {
    override fun checkClientTrusted(chain: Array<out X509Certificate>, authType: String) {
        var lastError: CertificateException? = null
        for (manager in delegates) {
            try {
                manager.checkClientTrusted(chain, authType)
                return
            } catch (err: CertificateException) {
                lastError = err
            }
        }
        throw lastError ?: CertificateException("untrusted client certificate")
    }

    override fun checkServerTrusted(chain: Array<out X509Certificate>, authType: String) {
        var lastError: CertificateException? = null
        for (manager in delegates) {
            try {
                manager.checkServerTrusted(chain, authType)
                return
            } catch (err: CertificateException) {
                lastError = err
            }
        }
        throw lastError ?: CertificateException("untrusted server certificate")
    }

    override fun getAcceptedIssuers(): Array<X509Certificate> {
        return delegates.flatMap { it.acceptedIssuers.toList() }.toTypedArray()
    }
}
