package systems.pkt.lingon.viewmodel

import systems.pkt.lingon.DefaultTerminalFontSizeSp
import systems.pkt.lingon.DefaultTerminalZoom
import systems.pkt.lingon.data.relay.RelaySession
import systems.pkt.lingon.terminal.TerminalSnapshot

enum class StatusLevel {
    Info,
    Warn,
    Error,
}

data class StatusMessage(
    val message: String,
    val level: StatusLevel = StatusLevel.Info,
)

enum class ConnectionState {
    Idle,
    Connecting,
    Connected,
    Disconnected,
    Waiting,
}

data class UiState(
    val endpoint: String = "",
    val username: String? = null,
    val loggedIn: Boolean = false,
    val loginError: String? = null,
    val sessions: List<RelaySession> = emptyList(),
    val activeSessionId: String? = null,
    val activeSnapshot: TerminalSnapshot? = null,
    val status: StatusMessage? = null,
    val transientStatus: StatusMessage? = null,
    val theme: String? = null,
    val isBusy: Boolean = false,
    val showSettings: Boolean = false,
    val showThemePicker: Boolean = false,
    val showAppLockTimeoutDialog: Boolean = false,
    val showShareToken: Boolean = false,
    val showCertificates: Boolean = false,
    val fontSizeSp: Int = DefaultTerminalFontSizeSp,
    val zoomFactor: Float = DefaultTerminalZoom,
    val resizeHostEnabled: Boolean = false,
    val backgroundWallEnabled: Boolean = false,
    val wallInactivityEnabled: Boolean = false,
    val wallInactivityLabel: String? = null,
    val shareToken: String? = null,
    val shareTokenError: String? = null,
    val certificateError: String? = null,
    val savedEndpoints: List<String> = emptyList(),
    val selectedCertEndpoint: String? = null,
    val trustedCerts: List<systems.pkt.lingon.data.certs.TrustedCert> = emptyList(),
    val connectionState: ConnectionState = ConnectionState.Idle,
    val hasControl: Boolean = false,
    val terminalCols: Int = 80,
    val terminalRows: Int = 24,
    val scrollbackOffsetRows: Int = 0,
    val lastFrameSeq: Long = 0,
    val lastFrameType: String? = null,
    val lastFrameAtMs: Long = 0,
    val lastFrameError: String? = null,
    val terminalConnectionEpoch: Long = 0,
    val panResetNonce: Int = 0,
    val sessionSyncing: Boolean = false,
    val isRefreshing: Boolean = false,
    val lastManualRefreshAtMs: Long = 0,
    val appLockTimeoutMinutes: Int = 30,
    val requiresAppUnlock: Boolean = false,
    val unlockPromptPending: Boolean = false,
    val restoreTerminalImeOnLifecycleStart: Boolean? = null,
) {
    val bannerStatus: StatusMessage?
        get() = transientStatus ?: status

    val canAttach: Boolean
        get() = loggedIn || !shareToken.isNullOrBlank()

    val showsSyncingIndicator: Boolean
        get() = sessionSyncing ||
            connectionState == ConnectionState.Connecting ||
            connectionState == ConnectionState.Waiting ||
            connectionState == ConnectionState.Disconnected

    val activeSessionHeadless: Boolean
        get() = activeSessionId?.let { id -> sessions.firstOrNull { it.id == id }?.headless == true } ?: false
}
