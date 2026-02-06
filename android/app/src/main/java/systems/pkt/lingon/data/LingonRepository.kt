package systems.pkt.lingon.data

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.flow.map
import systems.pkt.lingon.data.relay.RelaySession
import systems.pkt.lingon.data.relay.RelayWallEventsPage
import systems.pkt.lingon.data.relay.RelaySessionsClient
import systems.pkt.lingon.data.certs.CertificateStore
import systems.pkt.lingon.data.certs.TrustedCert
import systems.pkt.lingon.data.certs.toTrusted

class LingonRepository(
    private val authClient: AuthClient,
    private val sessionsClient: RelaySessionsClient,
    private val certificateStore: CertificateStore,
    private val endpointStore: EndpointStore,
    private val fontSizeStore: FontSizeStore,
    private val zoomStore: ZoomStore,
    private val terminalResizeStore: TerminalResizeStore,
    private val appLockStore: AppLockStore,
) : LingonClient {
    override val endpointFlow: Flow<String> = endpointStore.endpointFlow
    override val fontSizeFlow: Flow<Int> = fontSizeStore.fontSizeFlow
    override val zoomFlow: Flow<Float> = zoomStore.zoomFlow
    override val resizeHostFlow: Flow<Boolean> = terminalResizeStore.resizeFlow
    override val appLockTimeoutMinutesFlow: Flow<Int> = appLockStore.timeoutMinutesFlow
    override val savedEndpointsFlow: Flow<List<String>> = endpointStore.savedEndpointsFlow
    override val certificatesFlow: Flow<Map<String, List<TrustedCert>>> = certificateStore.certificatesFlow
        .map { endpoints -> endpoints.mapValues { entry -> entry.value.map { it.toTrusted() } } }

    override fun setEndpoint(value: String) {
        endpointStore.setEndpoint(value)
    }

    override fun setFontSize(value: Int) {
        fontSizeStore.setFontSize(value)
    }

    override fun setZoom(value: Float) {
        zoomStore.setZoom(value)
    }

    override fun setResizeHostEnabled(value: Boolean) {
        terminalResizeStore.setResizeEnabled(value)
    }

    override fun setAppLockTimeoutMinutes(value: Int) {
        appLockStore.setTimeoutMinutes(value)
    }

    override suspend fun login(username: String, password: String, totp: String) {
        authClient.login(username, password, totp)
    }

    override suspend fun logout() {
        authClient.logout()
    }

    override suspend fun clearAuth() {
        authClient.clearAuth()
    }

    override suspend fun refreshAuth(): Boolean {
        return runCatching {
            authClient.refresh()
            true
        }.getOrDefault(false)
    }

    override suspend fun listSessions(): List<RelaySession> {
        return sessionsClient.listSessions()
    }

    override suspend fun listWallEvents(sinceId: Long, limit: Int): RelayWallEventsPage {
        return sessionsClient.listWallEvents(sinceId, limit)
    }

    override fun streamSessions(): Flow<List<RelaySession>> {
        // Sessions updates are delivered over the active WebSocket connection.
        return emptyFlow()
    }

    override suspend fun listTrustedCertificates(endpoint: String): List<TrustedCert> {
        return certificateStore.list(endpoint).map { it.toTrusted() }
    }

    override suspend fun addTrustedCertificates(endpoint: String, pem: String): List<TrustedCert> {
        endpointStore.rememberEndpoint(endpoint)
        return certificateStore.addCertificates(endpoint, pem).map { it.toTrusted() }
    }

    override suspend fun removeTrustedCertificate(endpoint: String, certId: String) {
        certificateStore.removeCertificate(endpoint, certId)
    }
}
