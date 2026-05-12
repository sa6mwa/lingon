package systems.pkt.lingon.data

import kotlinx.coroutines.flow.Flow
import systems.pkt.lingon.data.relay.RelayWallEventsPage
import systems.pkt.lingon.data.relay.RelaySession
import systems.pkt.lingon.data.certs.TrustedCert

interface LingonClient {
    val endpointFlow: Flow<String>
    val fontSizeFlow: Flow<Int>
    val resizeHostFlow: Flow<Boolean>
    val backgroundWallEnabledFlow: Flow<Boolean>
    val followOnReadEnabledFlow: Flow<Boolean>
    val appLockTimeoutMinutesFlow: Flow<Int>
    val savedEndpointsFlow: Flow<List<String>>
    val certificatesFlow: Flow<Map<String, List<TrustedCert>>>

    fun setEndpoint(value: String)
    fun setFontSize(value: Int)
    fun saveSessionZoom(endpoint: String, sessionId: String, value: Float)
    fun setResizeHostEnabled(value: Boolean)
    fun setBackgroundWallEnabled(value: Boolean)
    fun setFollowOnReadEnabled(value: Boolean)
    fun setAppLockTimeoutMinutes(value: Int)
    fun saveLastActiveSessionId(endpoint: String, sessionId: String)
    fun clearLastActiveSession()

    suspend fun login(username: String, password: String, totp: String)
    suspend fun logout()
    suspend fun clearAuth()
    suspend fun refreshAuth(): Boolean
    suspend fun loadSessionZoom(endpoint: String, sessionId: String): Float
    suspend fun loadLastActiveSessionId(endpoint: String): String?
    suspend fun listSessions(): List<RelaySession>
    suspend fun listWallEvents(sinceId: Long, limit: Int): RelayWallEventsPage
    fun streamSessions(): Flow<List<RelaySession>>

    suspend fun listTrustedCertificates(endpoint: String): List<TrustedCert>
    suspend fun addTrustedCertificates(endpoint: String, pem: String): List<TrustedCert>
    suspend fun removeTrustedCertificate(endpoint: String, certId: String)
}
