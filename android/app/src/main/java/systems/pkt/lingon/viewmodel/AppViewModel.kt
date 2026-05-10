package systems.pkt.lingon.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.TimeoutCancellationException
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import android.util.Log
import androidx.annotation.VisibleForTesting
import okhttp3.Response
import okhttp3.WebSocket
import systems.pkt.lingon.data.ApiException
import systems.pkt.lingon.data.LingonClient
import systems.pkt.lingon.data.relay.RelaySession
import systems.pkt.lingon.data.relay.RelayWebSocketClient
import systems.pkt.lingon.notifications.NoopWallDeliveryCoordinator
import systems.pkt.lingon.notifications.WallDeliveryCoordinator
import systems.pkt.lingon.notifications.formatWallSource
import systems.pkt.lingon.terminal.TerminalSnapshot
import systems.pkt.lingon.terminal.buildScrollbackSnapshot
import systems.pkt.lingon.DefaultScrollbackLines
import systems.pkt.lingon.DefaultTerminalZoom
import systems.pkt.lingon.MaxTerminalZoom
import systems.pkt.lingon.MinTerminalZoom
import systems.pkt.lingon.protocol.CommandKind
import systems.pkt.lingon.protocol.ScrollbackRow
import systems.pkt.lingon.ui.SNAPSHOT_MODE_APP_CURSOR
import systems.pkt.lingon.ui.translateAppCursorKeys
import systems.pkt.lingon.work.NoopWallWorkScheduler
import systems.pkt.lingon.work.BackgroundWallServiceController
import systems.pkt.lingon.work.NoopBackgroundWallServiceController
import systems.pkt.lingon.work.WallWorkScheduler
import java.util.LinkedHashMap
import java.util.UUID
import kotlin.math.abs
import kotlin.math.max
import kotlin.math.min

class AppViewModel(
    private val repository: LingonClient,
    private val wsClient: RelayWebSocketClient,
    private val wallDeliveryCoordinator: WallDeliveryCoordinator = NoopWallDeliveryCoordinator,
    private val wallWorkScheduler: WallWorkScheduler = NoopWallWorkScheduler,
    private val backgroundWallServiceController: BackgroundWallServiceController = NoopBackgroundWallServiceController,
) : ViewModel() {
    private val _state = MutableStateFlow(UiState())
    val state: StateFlow<UiState> = _state.asStateFlow()

    private var ws: WebSocket? = null
    private var socketOpen = false
    private var reconnectJob: Job? = null
    private var reconnectAttempt = 0
    private var suppressReconnect = false
    private var sessionPollJob: Job? = null
    private var sessionPollAttempt = 0
    private var refreshJob: Job? = null
    private val clientId = "android-${UUID.randomUUID()}"
    private var lastSeq = 0L
    private var activeConnection: ConnectionKey? = null
    private var liveSnapshot: TerminalSnapshot? = null
    private val scrollbackRows = ArrayList<ScrollbackRow>()
    private var scrollbackCols = 0
    private var scrollbackOffset = 0
    private var forceFullSnapshotOnNextConnect = false
    private val scrollbackLimit = DefaultScrollbackLines
    private val sessionCaches = LinkedHashMap<String, SessionCache>()
    private val sessionListCache = LinkedHashMap<String, RelaySession>()
    private val missingSessionSinceMs = LinkedHashMap<String, Long>()
    @VisibleForTesting
    internal var resizeHostOverride: Boolean? = null
    @VisibleForTesting
    internal var nowProvider: () -> Long = System::currentTimeMillis
    private var lastBackgroundAtMs: Long? = null
    private var appInForeground = true
    private val sessionViewStates = LinkedHashMap<SessionViewStateKey, SessionViewState>()
    private val sessionWallInactivityStates = LinkedHashMap<SessionViewStateKey, SessionWallInactivityState>()
    private val zoomLoadJobs = LinkedHashMap<SessionViewStateKey, Job>()
    private val zoomPersistJobs = LinkedHashMap<SessionViewStateKey, Job>()
    private var nextPanResetToken = 0
    private var lastForegroundRecoveryAtMs: Long = 0
    private var pendingWallInactivitySessionId: String? = null
    private var pendingBackgroundWallEnabled: Boolean? = null
    private var transientStatusJob: Job? = null

    init {
        viewModelScope.launch {
            repository.endpointFlow.collectLatest { endpoint ->
                _state.update { it.copy(endpoint = endpoint) }
            }
        }
        viewModelScope.launch {
            repository.fontSizeFlow.collectLatest { size ->
                _state.update { it.copy(fontSizeSp = size) }
            }
        }
        viewModelScope.launch {
            repository.resizeHostFlow.collectLatest { _ ->
                if (resizeHostOverride != null) return@collectLatest
                _state.update { it.copy(resizeHostEnabled = false) }
            }
        }
        viewModelScope.launch {
            repository.backgroundWallEnabledFlow.collectLatest { enabled ->
                val pending = pendingBackgroundWallEnabled
                if (pending != null && enabled != pending) {
                    return@collectLatest
                }
                if (pending != null && enabled == pending) {
                    pendingBackgroundWallEnabled = null
                }
                _state.update { it.copy(backgroundWallEnabled = enabled) }
                syncWallPollingSchedule()
            }
        }
        viewModelScope.launch {
            repository.appLockTimeoutMinutesFlow.collectLatest { minutes ->
                _state.update { state ->
                    if (minutes == 0 && state.requiresAppUnlock) {
                        state.copy(
                            appLockTimeoutMinutes = minutes,
                            requiresAppUnlock = false,
                            unlockPromptPending = false,
                        )
                    } else {
                        state.copy(appLockTimeoutMinutes = minutes)
                    }
                }
            }
        }
        viewModelScope.launch {
            repository.savedEndpointsFlow.collectLatest { endpoints ->
                _state.update { state ->
                    val selected = state.selectedCertEndpoint
                    val nextSelected = when {
                        !selected.isNullOrBlank() && endpoints.contains(selected) -> selected
                        endpoints.isNotEmpty() -> endpoints.first()
                        else -> state.endpoint
                    }
                    state.copy(savedEndpoints = endpoints, selectedCertEndpoint = nextSelected)
                }
                val target = _state.value.selectedCertEndpoint
                if (!target.isNullOrBlank()) {
                    loadTrustedCertificates(target)
                }
            }
        }
        viewModelScope.launch {
            repository.certificatesFlow.collectLatest { allCerts ->
                val endpoint = _state.value.selectedCertEndpoint
                if (endpoint.isNullOrBlank()) return@collectLatest
                _state.update { it.copy(trustedCerts = allCerts[endpoint].orEmpty()) }
            }
        }
        viewModelScope.launch {
            bootstrapSessions()
        }
    }

    fun showSettings(show: Boolean) {
        _state.update { it.copy(showSettings = show) }
    }

    fun showThemePicker(show: Boolean) {
        _state.update { it.copy(showThemePicker = show) }
    }

    fun showAppLockTimeoutDialog(show: Boolean) {
        _state.update { it.copy(showAppLockTimeoutDialog = show) }
    }

    fun setAppLockTimeoutMinutes(minutes: Int) {
        repository.setAppLockTimeoutMinutes(minutes)
        if (minutes <= 0) {
            _state.update { it.copy(requiresAppUnlock = false, unlockPromptPending = false) }
            lastBackgroundAtMs = null
        }
    }

    fun showShareToken(show: Boolean, token: String? = null) {
        _state.update { it.copy(showShareToken = show, shareToken = token, shareTokenError = null) }
    }

    fun setResizeHostEnabled(enabled: Boolean) {
        repository.setResizeHostEnabled(false)
        _state.update { it.copy(resizeHostEnabled = false) }
    }

    fun setBackgroundWallEnabled(enabled: Boolean) {
        pendingBackgroundWallEnabled = enabled
        repository.setBackgroundWallEnabled(enabled)
        _state.update { it.copy(backgroundWallEnabled = enabled) }
        syncWallPollingSchedule()
    }

    fun showCertificates(show: Boolean) {
        _state.update { state ->
            val selected = state.selectedCertEndpoint ?: state.endpoint
            state.copy(showCertificates = show, selectedCertEndpoint = selected, certificateError = null)
        }
        if (show) {
            _state.value.selectedCertEndpoint?.let { endpoint ->
                viewModelScope.launch { loadTrustedCertificates(endpoint) }
            }
        }
    }

    fun manualRefresh() {
        if (_state.value.requiresAppUnlock) {
            setStatus("unlock required", StatusLevel.Warn)
            return
        }
        if (refreshJob?.isActive == true) return
        refreshJob = viewModelScope.launch {
            val startedAt = System.currentTimeMillis()
            _state.update { it.copy(isRefreshing = true, lastManualRefreshAtMs = startedAt) }
            forceFullSnapshotOnNextConnect = true
            val previousSuppressReconnect = suppressReconnect
            suppressReconnect = true
            try {
                forceRecoverActiveConnection(refreshSessionsFirst = true)
            } finally {
                suppressReconnect = previousSuppressReconnect
            }
            val elapsed = System.currentTimeMillis() - startedAt
            val minRefreshMs = 750L
            if (elapsed < minRefreshMs) {
                delay(minRefreshMs - elapsed)
            }
            _state.update { it.copy(isRefreshing = false) }
        }
    }

    fun toggleWallInactivity() {
        val state = _state.value
        val sessionId = state.activeSessionId?.trim().orEmpty()
        if (sessionId.isBlank()) {
            setStatus("no active session", StatusLevel.Warn)
            return
        }
        if (state.requiresAppUnlock) {
            setStatus("unlock required", StatusLevel.Warn)
            return
        }
        if (state.connectionState != ConnectionState.Connected) {
            setStatus("not connected", StatusLevel.Warn)
            return
        }
        if (state.shareToken != null && !state.hasControl) {
            setStatus("view-only session", StatusLevel.Warn)
            return
        }
        val webSocket = ws
        if (!socketOpen || webSocket == null) {
            setStatus("not connected", StatusLevel.Warn)
            return
        }
        pendingWallInactivitySessionId = sessionId
        wsClient.sendCommand(webSocket, CommandKind.COMMAND_KIND_CYCLE_WALL_INACTIVITY)
    }

    fun selectCertEndpoint(endpoint: String) {
        _state.update { it.copy(selectedCertEndpoint = endpoint, certificateError = null) }
        viewModelScope.launch { loadTrustedCertificates(endpoint) }
    }

    fun setShareTokenError(message: String?) {
        _state.update { it.copy(shareTokenError = message) }
    }

    fun requestAppUnlockPrompt() {
        if (!_state.value.requiresAppUnlock) return
        _state.update { it.copy(unlockPromptPending = true) }
    }

    fun onUnlockPromptLaunched() {
        _state.update { it.copy(unlockPromptPending = false) }
    }

    fun onAppUnlockSucceeded() {
        lastBackgroundAtMs = null
        _state.update { it.copy(requiresAppUnlock = false, unlockPromptPending = false) }
        onAppForeground()
    }

    fun onAppUnlockCancelled() {
        _state.update { it.copy(unlockPromptPending = false) }
    }

    @VisibleForTesting
    internal fun setHasControlForTesting(value: Boolean) {
        _state.update { it.copy(hasControl = value) }
    }

    @VisibleForTesting
    internal fun setResizeHostEnabledForTesting(value: Boolean) {
        resizeHostOverride = value
        _state.update { it.copy(resizeHostEnabled = false) }
    }

    fun setCertificateError(message: String?) {
        _state.update { it.copy(certificateError = message) }
    }

    fun addTrustedCertificates(endpoint: String, pem: String) {
        viewModelScope.launch {
            try {
                repository.addTrustedCertificates(endpoint, pem)
                loadTrustedCertificates(endpoint)
                setCertificateError(null)
            } catch (err: Exception) {
                setCertificateError(err.message ?: "failed to add certificate")
            }
        }
    }

    fun removeTrustedCertificate(endpoint: String, certId: String) {
        viewModelScope.launch {
            try {
                repository.removeTrustedCertificate(endpoint, certId)
                loadTrustedCertificates(endpoint)
            } catch (err: Exception) {
                setCertificateError(err.message ?: "failed to remove certificate")
            }
        }
    }

    fun updateEndpoint(value: String) {
        val normalized = normalizeEndpoint(value)
        if (normalized == null) {
            setStatus("endpoint must start with https://", StatusLevel.Error)
            return
        }
        repository.setEndpoint(normalized)
        clearSessionCaches()
        resetCurrentSessionState()
        lastBackgroundAtMs = null
        stopReconnect()
        stopWallPoll(resetCursor = true)
        closeWebSocket("endpoint updated")
        _state.update {
            it.copy(
                loggedIn = false,
                username = null,
                activeSnapshot = null,
                sessions = emptyList(),
                activeSessionId = null,
                shareToken = null,
                shareTokenError = null,
                wallInactivityEnabled = false,
                wallInactivityLabel = null,
                scrollbackOffsetRows = 0,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                hasControl = false,
                lastFrameSeq = 0,
                lastFrameType = null,
                lastFrameAtMs = 0,
                lastFrameError = null,
                requiresAppUnlock = false,
                unlockPromptPending = false,
                sessionSyncing = false,
            )
        }
        syncWallPollingSchedule()
        viewModelScope.launch {
            bootstrapSessions()
            if (_state.value.shareToken != null) {
                connectActiveSession()
            }
        }
    }

    fun updateFontSize(value: Int) {
        repository.setFontSize(value)
    }

    fun updateZoomFactor(value: Float) {
        val normalized = value.coerceIn(MinTerminalZoom, MaxTerminalZoom)
        val key = activeSessionViewStateKey()
        if (key == null) {
            _state.update {
                if (abs(it.zoomFactor - normalized) < 0.0005f) {
                    it
                } else {
                    it.copy(zoomFactor = normalized)
                }
            }
            return
        }
        val currentViewState = sessionViewStates[key] ?: SessionViewState()
        if (abs(currentViewState.zoomFactor - normalized) < 0.0005f) {
            return
        }
        sessionViewStates[key] = currentViewState.copy(zoomFactor = normalized)
        _state.update { it.copy(zoomFactor = normalized) }
        persistSessionZoomDebounced(key, normalized)
    }

    fun recordTerminalImeVisibilityForLifecycle(visible: Boolean) {
        _state.update { it.copy(restoreTerminalImeOnLifecycleStart = visible) }
    }

    fun resetZoomAndPan() {
        val key = activeSessionViewStateKey()
        if (key == null) {
            _state.update { it.copy(zoomFactor = DefaultTerminalZoom, panResetNonce = it.panResetNonce + 1) }
            return
        }
        cancelZoomPersistence(key)
        nextPanResetToken += 1
        val updatedViewState = (sessionViewStates[key] ?: SessionViewState()).copy(
            zoomFactor = DefaultTerminalZoom,
            panResetNonce = nextPanResetToken,
        )
        sessionViewStates[key] = updatedViewState
        repository.saveSessionZoom(key.endpoint, key.sessionId, DefaultTerminalZoom)
        _state.update {
            it.copy(
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = updatedViewState.panResetNonce,
            )
        }
    }

    fun setTheme(value: String) {
        _state.update { it.copy(theme = value) }
    }

    fun showStatus(message: String, level: StatusLevel = StatusLevel.Info) {
        setStatus(message, level)
    }

    fun login(username: String, password: String, totp: String) {
        viewModelScope.launch {
            setBusy(true)
            _state.update { it.copy(loginError = null) }
            try {
                withTimeout(20_000) {
                    repository.login(username, password, totp)
                }
                _state.update {
                    it.copy(
                        loggedIn = true,
                        username = username,
                        loginError = null,
                        shareToken = null,
                        shareTokenError = null,
                        status = null,
                        transientStatus = null,
                        requiresAppUnlock = false,
                        unlockPromptPending = false,
                    )
                }
                lastBackgroundAtMs = null
                bootstrapSessions()
            } catch (err: ApiException) {
                _state.update { it.copy(loginError = err.message ?: "login failed") }
            } catch (err: TimeoutCancellationException) {
                _state.update { it.copy(loginError = "network timeout") }
            } catch (err: Exception) {
                _state.update { it.copy(loginError = err.message ?: "network error") }
            } finally {
                setBusy(false)
            }
        }
    }

    fun logout() {
        viewModelScope.launch {
            setBusy(true)
            suppressReconnect = true
            stopReconnect()
            stopWallPoll(resetCursor = true)
            closeWebSocket("logout")
            try {
                repository.logout()
            } catch (err: ApiException) {
                _state.update {
                    it.copy(
                        status = StatusMessage(err.message ?: "logout failed", StatusLevel.Error),
                        transientStatus = null,
                    )
                }
            } finally {
                try {
                    repository.clearAuth()
                } catch (_: Exception) {
                }
                repository.clearLastActiveSession()
                clearSessionCaches()
                resetCurrentSessionState()
                suppressReconnect = false
                _state.update {
                    it.copy(
                        loggedIn = false,
                        username = null,
                        sessions = emptyList(),
                        activeSessionId = null,
                        activeSnapshot = null,
                        shareToken = null,
                        shareTokenError = null,
                        wallInactivityEnabled = false,
                        wallInactivityLabel = null,
                        showCertificates = false,
                        certificateError = null,
                        scrollbackOffsetRows = 0,
                        zoomFactor = DefaultTerminalZoom,
                        panResetNonce = 0,
                        connectionState = ConnectionState.Idle,
                        hasControl = false,
                        lastFrameSeq = 0,
                        lastFrameType = null,
                        lastFrameAtMs = 0,
                        lastFrameError = null,
                        showAppLockTimeoutDialog = false,
                        requiresAppUnlock = false,
                        unlockPromptPending = false,
                        sessionSyncing = false,
                    )
                }
                lastBackgroundAtMs = null
                syncWallPollingSchedule()
                setBusy(false)
            }
        }
    }

    fun handleSharedToken(token: String, endpointOverride: String?) {
        stopReconnect()
        stopWallPoll(resetCursor = true)
        closeWebSocket("share token")
        clearSessionCaches()
        resetCurrentSessionState()
        lastBackgroundAtMs = null
        _state.update {
            it.copy(
                loggedIn = false,
                username = null,
                shareToken = token,
                shareTokenError = null,
                sessions = listOf(sharedSession()),
                activeSessionId = sharedSessionId,
                activeSnapshot = null,
                scrollbackOffsetRows = 0,
                hasControl = false,
                requiresAppUnlock = false,
                unlockPromptPending = false,
                sessionSyncing = false,
            )
        }
        activateSessionViewState()
        syncWallPollingSchedule()
        viewModelScope.launch {
            val endpointValue = if (!endpointOverride.isNullOrBlank()) {
                val normalized = normalizeEndpoint(endpointOverride)
                if (normalized == null) {
                    setStatus("share token endpoint must start with https://", StatusLevel.Error)
                    return@launch
                }
                repository.setEndpoint(normalized)
                normalized
            } else {
                repository.endpointFlow.first().trim()
            }
            if (endpointValue.isBlank()) {
                setStatus("invalid endpoint", StatusLevel.Error)
                return@launch
            }
            _state.update { it.copy(endpoint = endpointValue) }
            activateSessionViewState()
            connectActiveSession()
        }
    }

    fun selectSession(sessionId: String) {
        selectSession(sessionId, persistSelection = true)
    }

    @VisibleForTesting
    fun selectSessionForTesting(sessionId: String) {
        selectSession(sessionId, persistSelection = false)
    }

    private fun selectSession(sessionId: String, persistSelection: Boolean) {
        val current = _state.value
        val wallState = currentWallInactivityState(sessionId)
        if (sessionId == current.activeSessionId) {
            syncCurrentSessionCache()
            activateSessionViewState()
            if (sessionCaches[sessionId]?.liveSnapshot != null) {
                applySessionCache(sessionId)
            }
            _state.update {
                it.copy(
                    wallInactivityEnabled = wallState.enabled,
                    wallInactivityLabel = wallState.label,
                )
            }
            if (ws == null || current.connectionState != ConnectionState.Connected) {
                connectActiveSession()
            }
            return
        }
        syncCurrentSessionCache()
        _state.update {
            it.copy(
                activeSessionId = sessionId,
                sessionSyncing = true,
                wallInactivityEnabled = wallState.enabled,
                wallInactivityLabel = wallState.label,
            )
        }
        activateSessionViewState()
        if (persistSelection && current.shareToken.isNullOrBlank()) {
            persistActiveSession(current.endpoint, sessionId)
        }
        connectActiveSession()
    }

    fun updateTerminalSize(cols: Int, rows: Int) {
        if (cols <= 0 || rows <= 0) return
        val current = _state.value
        if (current.terminalCols == cols && current.terminalRows == rows) return
        _state.update { it.copy(terminalCols = cols, terminalRows = rows) }
    }

    fun sendHeadlessResizeNow() {
        val state = _state.value
        val activeId = state.activeSessionId?.trim().orEmpty()
        if (activeId.isBlank()) return
        val activeSession = state.sessions.firstOrNull { it.id == activeId } ?: return
        if (!activeSession.headless) return
        val socket = ws ?: return
        if (state.terminalCols <= 0 || state.terminalRows <= 0) return
        wsClient.sendResize(socket, state.terminalCols, state.terminalRows)
    }

    fun adjustScrollback(deltaRows: Int) {
        if (deltaRows == 0) return
        val live = liveSnapshot ?: return
        val maxOffset = maxScrollbackOffset(live)
        if (maxOffset <= 0) return
        val next = (scrollbackOffset + deltaRows).coerceIn(0, maxOffset)
        if (next == scrollbackOffset) return
        scrollbackOffset = next
        val display = if (scrollbackOffset > 0) {
            buildScrollbackSnapshot(live, scrollbackRows, scrollbackOffset)
        } else {
            live
        }
        _state.update { it.copy(activeSnapshot = display, scrollbackOffsetRows = scrollbackOffset) }
        syncCurrentSessionCache()
    }

    private fun resetScrollbackView() {
        if (scrollbackOffset == 0) return
        scrollbackOffset = 0
        _state.update { it.copy(activeSnapshot = liveSnapshot, scrollbackOffsetRows = 0) }
        syncCurrentSessionCache()
    }

    private fun resetScrollbackBuffer() {
        scrollbackRows.clear()
        scrollbackCols = 0
        scrollbackOffset = 0
    }

    private fun resetCurrentSessionState() {
        liveSnapshot = null
        resetScrollbackBuffer()
        lastSeq = 0
    }

    private fun clearSessionCaches() {
        sessionCaches.clear()
        sessionListCache.clear()
        missingSessionSinceMs.clear()
        zoomLoadJobs.values.forEach { it.cancel() }
        zoomPersistJobs.values.forEach { it.cancel() }
        zoomLoadJobs.clear()
        zoomPersistJobs.clear()
        sessionViewStates.clear()
        sessionWallInactivityStates.clear()
        pendingWallInactivitySessionId = null
        nextPanResetToken = 0
    }

    private data class SessionViewStateKey(
        val endpoint: String,
        val sessionId: String,
    )

    private data class SessionViewState(
        val zoomFactor: Float = DefaultTerminalZoom,
        val panResetNonce: Int = 0,
    )

    private fun currentSessionCacheKey(): String? {
        return _state.value.activeSessionId?.trim()?.takeIf { it.isNotEmpty() }
    }

    private fun activeSessionViewStateKey(state: UiState = _state.value): SessionViewStateKey? {
        val endpoint = state.endpoint.trim()
        val sessionId = state.activeSessionId?.trim().orEmpty()
        if (endpoint.isBlank() || sessionId.isBlank()) {
            return null
        }
        return SessionViewStateKey(endpoint = endpoint, sessionId = sessionId)
    }

    private fun activateSessionViewState(state: UiState = _state.value) {
        val key = activeSessionViewStateKey(state)
        if (key == null) {
            _state.update { it.copy(zoomFactor = DefaultTerminalZoom, panResetNonce = 0) }
            return
        }
        val cached = sessionViewStates[key]
        if (cached != null) {
            _state.update { current ->
                if (current.activeSessionId == key.sessionId && current.endpoint.trim() == key.endpoint) {
                    current.copy(
                        zoomFactor = cached.zoomFactor,
                        panResetNonce = cached.panResetNonce,
                    )
                } else {
                    current
                }
            }
            return
        }
        _state.update { current ->
            if (current.activeSessionId == key.sessionId && current.endpoint.trim() == key.endpoint) {
                current.copy(zoomFactor = DefaultTerminalZoom, panResetNonce = 0)
            } else {
                current
            }
        }
        if (zoomLoadJobs[key]?.isActive == true) {
            return
        }
        zoomLoadJobs[key] = viewModelScope.launch {
            try {
                val zoom = repository.loadSessionZoom(key.endpoint, key.sessionId)
                val previous = sessionViewStates[key] ?: SessionViewState()
                val loadedState = previous.copy(zoomFactor = zoom)
                sessionViewStates[key] = loadedState
                _state.update { current ->
                    if (current.activeSessionId == key.sessionId && current.endpoint.trim() == key.endpoint) {
                        current.copy(
                            zoomFactor = loadedState.zoomFactor,
                            panResetNonce = loadedState.panResetNonce,
                        )
                    } else {
                        current
                    }
                }
            } finally {
                zoomLoadJobs.remove(key)
            }
        }
    }

    private fun prefetchSessionViewStates(endpoint: String, sessions: Collection<RelaySession>) {
        val cleanedEndpoint = endpoint.trim()
        if (cleanedEndpoint.isBlank()) return
        for (session in sessions) {
            val cleanedSessionId = session.id.trim()
            if (cleanedSessionId.isBlank()) continue
            val key = SessionViewStateKey(cleanedEndpoint, cleanedSessionId)
            if (sessionViewStates.containsKey(key) || zoomLoadJobs[key]?.isActive == true) {
                continue
            }
            zoomLoadJobs[key] = viewModelScope.launch {
                try {
                    val zoom = repository.loadSessionZoom(key.endpoint, key.sessionId)
                    val existing = sessionViewStates[key] ?: SessionViewState()
                    sessionViewStates[key] = existing.copy(zoomFactor = zoom)
                } finally {
                    zoomLoadJobs.remove(key)
                }
            }
        }
    }

    private fun persistSessionZoomDebounced(key: SessionViewStateKey, value: Float) {
        cancelZoomPersistence(key)
        zoomPersistJobs[key] = viewModelScope.launch {
            delay(150)
            repository.saveSessionZoom(key.endpoint, key.sessionId, value)
            zoomPersistJobs.remove(key)
        }
    }

    private fun cancelZoomPersistence(key: SessionViewStateKey) {
        zoomPersistJobs.remove(key)?.cancel()
    }

    private fun syncCurrentSessionCache() {
        val key = currentSessionCacheKey() ?: return
        val cache = sessionCaches.getOrPut(key) { SessionCache() }
        cache.lastSeq = lastSeq
        cache.liveSnapshot = liveSnapshot
        cache.scrollbackCols = scrollbackCols
        cache.scrollbackOffset = scrollbackOffset
        cache.scrollbackRows.clear()
        cache.scrollbackRows.addAll(scrollbackRows)
    }

    private fun applySessionCache(sessionId: String?) {
        val key = (sessionId ?: _state.value.activeSessionId)?.trim()?.takeIf { it.isNotEmpty() }
        val cache = key?.let { sessionCaches[it] }
        if (cache == null) {
            lastSeq = 0
            liveSnapshot = null
            scrollbackRows.clear()
            scrollbackCols = 0
            scrollbackOffset = 0
            _state.update {
                it.copy(
                    activeSnapshot = null,
                    scrollbackOffsetRows = 0,
                    lastFrameSeq = 0,
                    lastFrameType = null,
                    lastFrameAtMs = 0,
                    lastFrameError = null,
                )
            }
            return
        }
        lastSeq = cache.lastSeq
        liveSnapshot = cache.liveSnapshot
        scrollbackRows.clear()
        scrollbackRows.addAll(cache.scrollbackRows)
        scrollbackCols = cache.scrollbackCols
        scrollbackOffset = cache.scrollbackOffset
        if (scrollbackOffset < 0) {
            scrollbackOffset = 0
        }
        val maxOffset = maxScrollbackOffset(liveSnapshot)
        if (scrollbackOffset > maxOffset) {
            scrollbackOffset = maxOffset
        }
        val display = if (scrollbackOffset > 0) {
            liveSnapshot?.let { buildScrollbackSnapshot(it, scrollbackRows, scrollbackOffset) }
        } else {
            liveSnapshot
        }
        _state.update {
            it.copy(
                activeSnapshot = display,
                scrollbackOffsetRows = scrollbackOffset,
                lastFrameSeq = cache.lastSeq,
                lastFrameType = null,
                lastFrameAtMs = 0,
                lastFrameError = null,
            )
        }
    }

    private data class SessionCache(
        var lastSeq: Long = 0L,
        var liveSnapshot: TerminalSnapshot? = null,
        val scrollbackRows: ArrayList<ScrollbackRow> = ArrayList(),
        var scrollbackCols: Int = 0,
        var scrollbackOffset: Int = 0,
    )

    private data class SessionWallInactivityState(
        val enabled: Boolean = false,
        val label: String? = null,
    )

    private fun currentWallInactivityState(sessionId: String?, endpoint: String = _state.value.endpoint): SessionWallInactivityState {
        val cleanedSession = sessionId?.trim().orEmpty()
        val cleanedEndpoint = endpoint.trim()
        if (cleanedSession.isBlank() || cleanedEndpoint.isBlank()) {
            return SessionWallInactivityState()
        }
        return sessionWallInactivityStates[SessionViewStateKey(cleanedEndpoint, cleanedSession)] ?: SessionWallInactivityState()
    }

    private fun wallInactivityBanner(state: SessionWallInactivityState): String {
        if (!state.enabled) {
            return "wall off"
        }
        val label = state.label?.trim().orEmpty()
        return if (label.isNotBlank()) {
            "wall $label"
        } else {
            "wall on"
        }
    }

    private fun wallBanner(notification: WallNotification): String {
        val source = formatWallSource(notification.sender, notification.sourceSessionName)
        val body = notification.message.trim()
        return when {
            source.isNotBlank() && body.isNotBlank() -> "$source: $body"
            body.isNotBlank() -> body
            source.isNotBlank() -> source
            else -> "wall"
        }
    }

    private fun maxScrollbackOffset(snapshot: TerminalSnapshot?): Int {
        val live = snapshot ?: return 0
        val totalRows = scrollbackRows.size + live.rows
        return (totalRows - live.rows).coerceAtLeast(0)
    }

    private fun isLoggable(tag: String, level: Int): Boolean {
        return runCatching { Log.isLoggable(tag, level) }.getOrDefault(false)
    }

    fun sendRawInput(data: String) {
        if (data.isEmpty()) return
        if (_state.value.requiresAppUnlock) {
            setStatus("unlock required", StatusLevel.Warn)
            return
        }
        resetScrollbackView()
        val state = _state.value
        if (state.connectionState != ConnectionState.Connected) {
            setStatus("not connected", StatusLevel.Warn)
            return
        }
        if (state.shareToken != null && !state.hasControl) {
            setStatus("view-only session", StatusLevel.Warn)
            return
        }
        sendProcessedInput(data.toByteArray())
    }

    fun sendRawBytes(bytes: ByteArray) {
        if (bytes.isEmpty()) return
        if (_state.value.requiresAppUnlock) {
            setStatus("unlock required", StatusLevel.Warn)
            return
        }
        resetScrollbackView()
        val state = _state.value
        if (state.connectionState != ConnectionState.Connected) {
            setStatus("not connected", StatusLevel.Warn)
            return
        }
        if (state.shareToken != null && !state.hasControl) {
            setStatus("view-only session", StatusLevel.Warn)
            return
        }
        sendProcessedInput(bytes)
    }

    private fun sendProcessedInput(bytes: ByteArray) {
        val appCursorActive = (liveSnapshot?.mode ?: _state.value.activeSnapshot?.mode ?: 0) and SNAPSHOT_MODE_APP_CURSOR != 0
        val payload = translateAppCursorKeys(bytes, appCursorActive)
        val webSocket = ws
        if (!socketOpen || webSocket == null) {
            setStatus("not connected", StatusLevel.Warn)
            return
        }
        wsClient.sendInput(webSocket, payload)
    }

    private suspend fun bootstrapSessions() {
        if (_state.value.requiresAppUnlock) {
            return
        }
        if (_state.value.shareToken != null) {
            return
        }
        try {
            val sessions = listSessionsWithRecovery()
            _state.update { it.copy(loggedIn = true) }
            updateSessions(sessions)
            syncWallPollingSchedule()
            if (sessions.isEmpty()) {
                scheduleSessionPoll()
            }
            // Sessions updates are delivered via the active WebSocket connection.
        } catch (err: ApiException) {
            if (err.statusCode == 401) {
                handleUnauthorizedResponse()
                return
            }
            _state.update {
                it.copy(
                    status = StatusMessage(err.message ?: "failed to load sessions", StatusLevel.Error),
                    transientStatus = null,
                )
            }
        } catch (err: CancellationException) {
            throw err
        } catch (err: Exception) {
            _state.update {
                it.copy(
                    status = StatusMessage(err.message ?: "failed to load sessions", StatusLevel.Error),
                    transientStatus = null,
                )
            }
        }
    }

    private suspend fun updateSessions(sessions: List<RelaySession>): Boolean {
        if (_state.value.shareToken != null) return false
        val now = nowProvider()
        val currentActive = _state.value.activeSessionId?.trim().orEmpty()
        val merged = LinkedHashMap<String, RelaySession>(sessions.size + 1)
        for (session in sessions) {
            merged[session.id] = session
            sessionListCache[session.id] = session
            missingSessionSinceMs.remove(session.id)
        }
        if (currentActive.isNotBlank() && !merged.containsKey(currentActive)) {
            val missingSince = missingSessionSinceMs.getOrPut(currentActive) { now }
            if (now - missingSince <= MissingSessionGraceMs) {
                val cached = sessionListCache[currentActive]
                merged[currentActive] = cached ?: RelaySession(
                    id = currentActive,
                    name = currentActive,
                    status = "reconnecting",
                )
            } else {
                missingSessionSinceMs.remove(currentActive)
                sessionListCache.remove(currentActive)
            }
        }
        _state.update { it.copy(sessions = merged.values.toList()) }
        prefetchSessionViewStates(_state.value.endpoint, merged.values)
        syncWallPollingSchedule()
        val connectionHandled = ensureActiveSession(merged.values.toList())
        if (merged.isNotEmpty()) {
            stopSessionPoll()
        }
        return connectionHandled
    }

    private suspend fun handleExplicitSessionClosed(sessionId: String) {
        if (sessionId.isBlank()) return
        val before = _state.value
        val remaining = before.sessions.filterNot { it.id == sessionId }
        sessionListCache.remove(sessionId)
        missingSessionSinceMs.remove(sessionId)
        val wasActive = before.activeSessionId == sessionId || activeConnection?.sessionId == sessionId
        if (wasActive) {
            stopReconnect()
            stopSessionPoll()
            suppressReconnect = true
            closeWebSocket("session closed")
            forceFullSnapshotOnNextConnect = false
            resetCurrentSessionState()
        }
        _state.update {
            it.copy(
                sessions = remaining,
                activeSessionId = if (wasActive) null else it.activeSessionId,
                activeSnapshot = if (wasActive) null else it.activeSnapshot,
                wallInactivityEnabled = if (wasActive) false else it.wallInactivityEnabled,
                wallInactivityLabel = if (wasActive) null else it.wallInactivityLabel,
                scrollbackOffsetRows = if (wasActive) 0 else it.scrollbackOffsetRows,
                connectionState = if (wasActive) ConnectionState.Idle else it.connectionState,
                hasControl = if (wasActive) false else it.hasControl,
                sessionSyncing = false,
            )
        }
        syncWallPollingSchedule()
        ensureActiveSession(remaining)
    }

    private suspend fun ensureActiveSession(sessions: List<RelaySession>): Boolean {
        val current = _state.value.activeSessionId
        val hasCurrent = current != null && sessions.any { it.id == current }
        if (hasCurrent) {
            if (ws == null || _state.value.connectionState != ConnectionState.Connected) {
                connectActiveSession()
                return true
            }
            return false
        }
        val endpoint = resolveEndpointForPersistence()
        val preferredSessionId = withTimeoutOrNull(1500) {
            repository.loadLastActiveSessionId(endpoint)
        }
        val next = when {
            !preferredSessionId.isNullOrBlank() && sessions.any { it.id == preferredSessionId } -> preferredSessionId
            else -> sessions.firstOrNull()?.id
        }
        val wallState = currentWallInactivityState(next)
        _state.update {
            it.copy(
                activeSessionId = next,
                wallInactivityEnabled = wallState.enabled,
                wallInactivityLabel = wallState.label,
            )
        }
        if (!next.isNullOrBlank()) {
            persistActiveSession(endpoint, next)
        }
        activateSessionViewState()
        if (next != null && next != current) {
            connectActiveSession()
            return true
        }
        return false
    }

    private suspend fun resolveEndpointForPersistence(): String {
        val current = _state.value.endpoint.trim()
        if (current.isNotBlank()) {
            return current
        }
        return repository.endpointFlow.first().trim()
    }

    private fun persistActiveSession(endpoint: String, sessionId: String) {
        val cleanedEndpoint = endpoint.trim()
        val cleanedSessionId = sessionId.trim()
        if (cleanedEndpoint.isBlank() || cleanedSessionId.isBlank()) return
        repository.saveLastActiveSessionId(cleanedEndpoint, cleanedSessionId)
    }

    private fun connectActiveSession() {
        val state = _state.value
        if (state.requiresAppUnlock) {
            syncWallPollingSchedule()
            return
        }
        val shareToken = state.shareToken
        var sessionId = if (!shareToken.isNullOrBlank()) {
            null
        } else {
            state.activeSessionId?.takeIf { candidate ->
                state.sessions.any { session -> session.id == candidate }
            }
        }
        if (shareToken.isNullOrBlank() && sessionId.isNullOrBlank()) {
            val fallback = state.sessions.firstOrNull()?.id
            if (!fallback.isNullOrBlank()) {
                sessionId = fallback
                forceFullSnapshotOnNextConnect = true
                _state.update {
                    it.copy(
                        activeSessionId = fallback,
                        activeSnapshot = null,
                        scrollbackOffsetRows = 0,
                        panResetNonce = 0,
                        sessionSyncing = true,
                        wallInactivityEnabled = currentWallInactivityState(fallback).enabled,
                        wallInactivityLabel = currentWallInactivityState(fallback).label,
                    )
                }
                activateSessionViewState()
                persistActiveSession(state.endpoint, fallback)
            }
        }
        if (sessionId.isNullOrBlank() && shareToken.isNullOrBlank()) {
            if (state.loggedIn) {
                scheduleSessionPoll()
            }
            syncWallPollingSchedule()
            return
        }
        val key = ConnectionKey(sessionId = sessionId, shareToken = shareToken)
        if (activeConnection == key && socketOpen && ws != null) {
            if (sessionId != null && sessionCaches[sessionId]?.liveSnapshot != null) {
                applySessionCache(sessionId)
            }
            return
        }
        applySessionCache(sessionId)
        val baseUrl = state.endpoint.trim().toHttpUrlOrNull()
        if (baseUrl == null) {
            setStatus("invalid endpoint", StatusLevel.Error)
            return
        }
        if (shareToken.isNullOrBlank()) {
            viewModelScope.launch {
                // Refreshing auth can fail transiently on mobile networks; attempt WS anyway and
                // rely on explicit 401 handling from the WS connection path.
                runCatching { repository.refreshAuth() }
                openWebSocket(baseUrl, sessionId, shareToken, state)
            }
            return
        }
        openWebSocket(baseUrl, sessionId, shareToken, state)
    }

    private fun openWebSocket(baseUrl: HttpUrl, sessionId: String?, shareToken: String?, state: UiState) {
        val key = ConnectionKey(sessionId = sessionId, shareToken = shareToken)
        if (activeConnection == key && socketOpen && ws != null) {
            return
        }
        stopReconnect()
        closeWebSocket("switch")
        socketOpen = false
        _state.update {
            it.copy(
                connectionState = ConnectionState.Connecting,
                hasControl = false,
                lastFrameError = null,
                sessionSyncing = true,
            )
        }
        syncWallPollingSchedule()
        activeConnection = key
        suppressReconnect = false
        val options = RelayWebSocketClient.ConnectOptions(
            baseUrl = baseUrl,
            sessionId = sessionId,
            shareToken = shareToken,
            clientId = clientId,
            cols = 0,
            rows = 0,
            wantsControl = true,
            lastSeq = if (forceFullSnapshotOnNextConnect) 0L else lastSeq,
        )
        ws = wsClient.connect(options, object : RelayWebSocketClient.Listener {
            private fun isStale(webSocket: WebSocket): Boolean {
                return ws !== webSocket
            }

            override fun onOpen(webSocket: WebSocket) {
                if (isStale(webSocket)) return
                socketOpen = true
                reconnectAttempt = 0
                stopReconnect()
                clearStatus()
                syncWallPollingSchedule()
            }

            override fun onFrame(webSocket: WebSocket, frame: systems.pkt.lingon.protocol.Frame) {
                if (isStale(webSocket)) return
                if (frame.seq != 0L) {
                    lastSeq = frame.seq
                }
                when {
                    frame.hasSessions() -> {
                        val updated = frame.sessions.sessionsList.map { session ->
                            RelaySession(
                                id = session.id,
                                name = session.name,
                                status = session.status,
                                headless = session.headless,
                            )
                        }
                        viewModelScope.launch {
                            updateSessions(updated)
                        }
                        _state.update {
                            it.copy(
                                lastFrameSeq = frame.seq,
                                lastFrameType = "sessions",
                                lastFrameAtMs = System.currentTimeMillis(),
                                lastFrameError = null,
                            )
                        }
                    }
                    frame.hasSessionClosed() -> {
                        val closedSessionId = frame.sessionId.takeIf { it.isNotBlank() }
                            ?: _state.value.activeSessionId
                            ?: activeConnection?.sessionId
                        if (!closedSessionId.isNullOrBlank()) {
                            viewModelScope.launch {
                                handleExplicitSessionClosed(closedSessionId.trim())
                            }
                        }
                        _state.update {
                            it.copy(
                                lastFrameSeq = frame.seq,
                                lastFrameType = "session_closed",
                                lastFrameAtMs = System.currentTimeMillis(),
                                lastFrameError = null,
                                sessionSyncing = false,
                            )
                        }
                    }
                    frame.hasWelcome() -> {
                        val holder = frame.welcome.holderClientId
                        val hasControl = holder.isNotBlank() && holder == clientId
                        if (isLoggable("lingon-term", Log.DEBUG)) {
                            Log.d(
                                "lingon-term",
                                "apply welcome seq=${frame.seq} control=${hasControl} cols=${frame.welcome.serverCols} rows=${frame.welcome.serverRows}",
                            )
                        }
                        _state.update {
                            it.copy(
                                connectionState = ConnectionState.Connected,
                                hasControl = hasControl,
                                lastFrameSeq = frame.seq,
                                lastFrameType = "welcome",
                                lastFrameAtMs = System.currentTimeMillis(),
                                lastFrameError = null,
                                sessionSyncing = false,
                            )
                        }
                        clearStatus()
                        syncWallPollingSchedule()
                    }
                    frame.hasSnapshot() -> {
                        forceFullSnapshotOnNextConnect = false
                        val snapshot = TerminalSnapshot.fromProto(frame.snapshot)
                        liveSnapshot = snapshot
                        val display = if (scrollbackOffset > 0) {
                            buildScrollbackSnapshot(snapshot, scrollbackRows, scrollbackOffset)
                        } else {
                            snapshot
                        }
                        if (isLoggable("lingon-term", Log.DEBUG)) {
                            Log.d(
                                "lingon-term",
                                "apply snapshot seq=${frame.seq} cols=${snapshot.cols} rows=${snapshot.rows}",
                            )
                        }
                        _state.update {
                            it.copy(
                                activeSnapshot = display,
                                scrollbackOffsetRows = scrollbackOffset,
                                connectionState = ConnectionState.Connected,
                                lastFrameSeq = frame.seq,
                                lastFrameType = "snapshot",
                                lastFrameAtMs = System.currentTimeMillis(),
                                lastFrameError = null,
                                sessionSyncing = false,
                            )
                        }
                        clearStatus()
                    }
                    frame.hasDiff() -> {
                        val nextLive = TerminalSnapshot.applyDiff(liveSnapshot, frame.diff)
                        liveSnapshot = nextLive
                        val display = if (scrollbackOffset > 0) {
                            buildScrollbackSnapshot(nextLive, scrollbackRows, scrollbackOffset)
                        } else {
                            nextLive
                        }
                        if (isLoggable("lingon-term", Log.DEBUG)) {
                            Log.d(
                                "lingon-term",
                                "apply diff seq=${frame.seq} rows=${frame.diff.diffRowsCount} cols=${frame.diff.cols} rowsTotal=${frame.diff.rows}",
                            )
                        }
                        _state.update {
                            it.copy(
                                activeSnapshot = display,
                                scrollbackOffsetRows = scrollbackOffset,
                                connectionState = ConnectionState.Connected,
                                lastFrameSeq = frame.seq,
                                lastFrameType = "diff",
                                lastFrameAtMs = System.currentTimeMillis(),
                                lastFrameError = null,
                                sessionSyncing = false,
                            )
                        }
                        clearStatus()
                    }
                    frame.hasControl() -> {
                        val holder = frame.control.holderClientId
                        val hasControl = holder.isNotBlank() && holder == clientId
                        if (isLoggable("lingon-term", Log.DEBUG)) {
                            Log.d(
                                "lingon-term",
                                "apply control seq=${frame.seq} holder=${holder} control=${hasControl}",
                            )
                        }
                        _state.update {
                            it.copy(
                                hasControl = hasControl,
                                lastFrameSeq = frame.seq,
                                lastFrameType = "control",
                                lastFrameAtMs = System.currentTimeMillis(),
                                lastFrameError = null,
                            )
                        }
                    }
                    frame.hasOut() -> {
                        if (isLoggable("lingon-term", Log.DEBUG)) {
                            Log.w(
                                "lingon-term",
                                "received out frame seq=${frame.seq} len=${frame.out.data.size()} (no emulator)",
                            )
                        }
                        _state.update {
                            it.copy(
                                lastFrameSeq = frame.seq,
                                lastFrameType = "out",
                                lastFrameAtMs = System.currentTimeMillis(),
                                lastFrameError = null,
                            )
                        }
                    }
                    frame.hasScrollback() -> {
                        val scrollback = frame.scrollback
                        if (scrollback.clear) {
                            scrollbackRows.clear()
                            scrollbackOffset = 0
                        }
                        if (scrollback.cols > 0) {
                            if (scrollbackCols != 0 && scrollbackCols != scrollback.cols) {
                                scrollbackRows.clear()
                                scrollbackOffset = 0
                            }
                            scrollbackCols = scrollback.cols
                        }
                        if (scrollback.rowsCount > 0) {
                            scrollbackRows.addAll(scrollback.rowsList)
                        }
                        if (scrollbackLimit > 0 && scrollbackRows.size > scrollbackLimit) {
                            val extra = scrollbackRows.size - scrollbackLimit
                            scrollbackRows.subList(0, extra).clear()
                        }
                        val live = liveSnapshot
                        if (live != null) {
                            val maxOffset = maxScrollbackOffset(live)
                            if (scrollbackOffset > maxOffset) {
                                scrollbackOffset = maxOffset
                            }
                            val display = if (scrollbackOffset > 0) {
                                buildScrollbackSnapshot(live, scrollbackRows, scrollbackOffset)
                            } else {
                                live
                            }
                            _state.update {
                                it.copy(
                                    activeSnapshot = display,
                                    scrollbackOffsetRows = scrollbackOffset,
                                    sessionSyncing = false,
                                    lastFrameSeq = frame.seq,
                                    lastFrameType = "scrollback",
                                    lastFrameAtMs = System.currentTimeMillis(),
                                    lastFrameError = null,
                                )
                            }
                        } else {
                            _state.update {
                                it.copy(
                                    scrollbackOffsetRows = scrollbackOffset,
                                    sessionSyncing = false,
                                    lastFrameSeq = frame.seq,
                                    lastFrameType = "scrollback",
                                    lastFrameAtMs = System.currentTimeMillis(),
                                    lastFrameError = null,
                                )
                            }
                        }
                    }
                    frame.hasWall() -> {
                        val wall = frame.wall
                        val notification = WallNotification(
                            endpoint = _state.value.endpoint.trim(),
                            eventId = wall.id,
                            sender = wall.sender,
                            sourceSessionName = wall.sourceSessionName,
                            message = wall.message,
                        )
                        viewModelScope.launch {
                            if (appInForeground) {
                                if (wallDeliveryCoordinator.consumeInApp(notification)) {
                                    showTransientStatus(wallBanner(notification), StatusLevel.Info)
                                }
                            } else {
                                wallDeliveryCoordinator.deliver(notification)
                            }
                        }
                        _state.update {
                            it.copy(
                                lastFrameSeq = frame.seq,
                                lastFrameType = "wall",
                                lastFrameAtMs = System.currentTimeMillis(),
                                lastFrameError = null,
                            )
                        }
                    }
                    frame.hasWallInactivityStatus() -> {
                        val sessionIdForStatus = frame.sessionId.takeIf { it.isNotBlank() }
                            ?: _state.value.activeSessionId
                            ?: activeConnection?.sessionId
                        val cleanedSessionId = sessionIdForStatus.orEmpty().trim()
                        val endpoint = _state.value.endpoint.trim()
                        val nextState = SessionWallInactivityState(
                            enabled = frame.wallInactivityStatus.enabled,
                            label = frame.wallInactivityStatus.inactiveAfter.takeIf { it.isNotBlank() },
                        )
                        val previousState = currentWallInactivityState(cleanedSessionId, endpoint)
                        if (cleanedSessionId.isNotBlank() && endpoint.isNotBlank()) {
                            sessionWallInactivityStates[SessionViewStateKey(endpoint, cleanedSessionId)] = nextState
                        }
                        _state.update {
                            val updated = it.copy(
                                lastFrameSeq = frame.seq,
                                lastFrameType = "wall_inactivity_status",
                                lastFrameAtMs = System.currentTimeMillis(),
                                lastFrameError = null,
                            )
                            if (updated.activeSessionId == cleanedSessionId) {
                                updated.copy(
                                    wallInactivityEnabled = nextState.enabled,
                                    wallInactivityLabel = nextState.label,
                                )
                            } else {
                                updated
                            }
                        }
                        val errText = frame.wallInactivityStatus.error.trim()
                        if (errText.isNotBlank()) {
                            if (_state.value.activeSessionId == cleanedSessionId) {
                                setStatus(errText, StatusLevel.Error)
                            }
                            if (pendingWallInactivitySessionId == cleanedSessionId) {
                                pendingWallInactivitySessionId = null
                            }
                            return
                        }
                        val shouldShowBanner =
                            pendingWallInactivitySessionId == cleanedSessionId || previousState != nextState
                        if (pendingWallInactivitySessionId == cleanedSessionId) {
                            pendingWallInactivitySessionId = null
                        }
                        if (shouldShowBanner && _state.value.activeSessionId == cleanedSessionId) {
                            showTransientStatus(wallInactivityBanner(nextState), StatusLevel.Info)
                        }
                    }
                    frame.hasError() -> {
                        val msg = frame.error.message.ifBlank { "connection error" }
                        _state.update {
                            it.copy(
                                lastFrameSeq = frame.seq,
                                lastFrameType = "error",
                                lastFrameAtMs = System.currentTimeMillis(),
                                lastFrameError = msg,
                            )
                        }
                        if (msg.contains("no host", ignoreCase = true)) {
                            setStatus("waiting for host", StatusLevel.Warn)
                            scheduleReconnect(
                                "waiting for host",
                                statusPrefix = "waiting for host, retrying in",
                                reconnectState = ConnectionState.Waiting,
                            )
                            syncCurrentSessionCache()
                            return
                        }
                        if (msg.contains("control not permitted", ignoreCase = true)) {
                            setStatus("view-only session", StatusLevel.Warn)
                            syncCurrentSessionCache()
                            return
                        }
                        if (msg.contains("authorization", ignoreCase = true)) {
                            setStatus("session expired", StatusLevel.Error)
                            handleAuthFailureWithoutLogout()
                            syncCurrentSessionCache()
                            return
                        }
                        val retryAfter = frame.error.retryAfterSeconds.takeIf { it > 0 }
                        scheduleReconnect(msg, retryAfter)
                    }
                }
                if (frame.seq != 0L) {
                    syncCurrentSessionCache()
                }
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable?, response: Response?) {
                if (isStale(webSocket)) return
                ws = null
                socketOpen = false
                if (response?.code == 401) {
                    setStatus("session expired", StatusLevel.Error)
                    handleAuthFailureWithoutLogout()
                    return
                }
                _state.update {
                    it.copy(
                        lastFrameType = "failure",
                        lastFrameAtMs = System.currentTimeMillis(),
                        lastFrameError = t?.message,
                        sessionSyncing = false,
                    )
                }
                scheduleReconnect(t?.message)
                syncWallPollingSchedule()
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String?) {
                if (isStale(webSocket)) return
                ws = null
                socketOpen = false
                _state.update {
                    it.copy(
                        lastFrameType = "closed",
                        lastFrameAtMs = System.currentTimeMillis(),
                        lastFrameError = reason,
                        sessionSyncing = false,
                    )
                }
                scheduleReconnect(reason)
                syncWallPollingSchedule()
            }
        })
    }

    private fun disconnectAndReset() {
        stopReconnect()
        stopSessionPoll()
        stopWallPoll(resetCursor = true)
        closeWebSocket("session expired")
        forceFullSnapshotOnNextConnect = false
        repository.clearLastActiveSession()
        lastBackgroundAtMs = null
        clearSessionCaches()
        resetCurrentSessionState()
        _state.update {
            it.copy(
                loggedIn = false,
                username = null,
                sessions = emptyList(),
                activeSessionId = null,
                activeSnapshot = null,
                shareToken = null,
                wallInactivityEnabled = false,
                wallInactivityLabel = null,
                scrollbackOffsetRows = 0,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                showCertificates = false,
                certificateError = null,
                connectionState = ConnectionState.Disconnected,
                hasControl = false,
                lastFrameSeq = 0,
                lastFrameType = null,
                lastFrameAtMs = 0,
                lastFrameError = null,
                requiresAppUnlock = false,
                unlockPromptPending = false,
                sessionSyncing = false,
            )
        }
        syncWallPollingSchedule()
    }

    private fun closeWebSocket(reason: String) {
        val current = ws
        ws = null
        socketOpen = false
        activeConnection = null
        pendingWallInactivitySessionId = null
        syncWallPollingSchedule()
        current?.close(1000, reason)
    }

    private suspend fun forceRecoverActiveConnection(refreshSessionsFirst: Boolean) {
        stopReconnect()
        stopSessionPoll()
        closeWebSocket("manual recovery")
        if (refreshSessionsFirst) {
            refreshSessionsAndReconnect()
        } else {
            connectActiveSession()
        }
    }

    private fun scheduleReconnect(
        reason: String?,
        retryAfterSeconds: Int? = null,
        statusPrefix: String? = null,
        reconnectState: ConnectionState = ConnectionState.Disconnected,
    ) {
        if (suppressReconnect) return
        ws = null
        socketOpen = false
        _state.update {
            it.copy(
                connectionState = reconnectState,
                hasControl = false,
                sessionSyncing = true,
            )
        }
        syncWallPollingSchedule()
        if (reconnectJob?.isActive == true) return
        reconnectAttempt += 1
        val delayMs = nextBackoffMs(reconnectAttempt, retryAfterSeconds)
        val seconds = (delayMs / 1000).coerceAtLeast(1)
        val prefix = statusPrefix ?: "reconnecting in"
        setStatus("${prefix} ${seconds}s", StatusLevel.Warn)
        reconnectJob = viewModelScope.launch {
            var remaining = seconds
            while (remaining > 0) {
                setStatus("${prefix} ${remaining}s", StatusLevel.Warn)
                delay(1000)
                remaining -= 1
            }
            refreshSessionsAndReconnect()
        }
    }

    private fun stopReconnect() {
        reconnectJob?.cancel()
        reconnectJob = null
    }

    private fun scheduleSessionPoll() {
        if (_state.value.shareToken != null) return
        if (!_state.value.loggedIn) return
        if (sessionPollJob?.isActive == true) return
        sessionPollAttempt += 1
        val delayMs = nextBackoffMs(sessionPollAttempt, null)
        val seconds = (delayMs / 1000).coerceAtLeast(1)
        setStatus("waiting for host, retrying in ${seconds}s", StatusLevel.Warn)
        sessionPollJob = viewModelScope.launch {
            var remaining = seconds
            while (remaining > 0) {
                setStatus("waiting for host, retrying in ${remaining}s", StatusLevel.Warn)
                delay(1000)
                remaining -= 1
            }
            refreshSessionsAndReconnect()
            if (_state.value.sessions.isEmpty() && _state.value.loggedIn) {
                sessionPollJob = null
                scheduleSessionPoll()
            }
        }
    }

    private fun stopSessionPoll() {
        sessionPollJob?.cancel()
        sessionPollJob = null
        sessionPollAttempt = 0
    }

    private suspend fun refreshSessionsAndReconnect() {
        if (_state.value.requiresAppUnlock) {
            return
        }
        if (_state.value.shareToken != null) {
            connectActiveSession()
            return
        }
        if (!_state.value.loggedIn) {
            return
        }
        try {
            val sessions = listSessionsWithRecovery()
            val connectionHandled = updateSessions(sessions)
            if (sessions.isEmpty()) {
                scheduleSessionPoll()
                return
            }
            if (connectionHandled) {
                return
            }
        } catch (err: ApiException) {
            if (err.statusCode == 401) {
                handleUnauthorizedResponse()
                return
            }
            _state.update {
                it.copy(
                    status = StatusMessage(err.message ?: "failed to load sessions", StatusLevel.Error),
                    transientStatus = null,
                )
            }
        } catch (err: CancellationException) {
            throw err
        } catch (err: Exception) {
            _state.update {
                it.copy(
                    status = StatusMessage(err.message ?: "failed to load sessions", StatusLevel.Error),
                    transientStatus = null,
                )
            }
        }
        connectActiveSession()
    }

    private suspend fun listSessionsWithRecovery(): List<RelaySession> {
        return try {
            repository.listSessions()
        } catch (err: ApiException) {
            if (err.statusCode != 401 || !_state.value.loggedIn) {
                throw err
            }
            val refreshed = runCatching { repository.refreshAuth() }.getOrDefault(false)
            if (!refreshed) {
                throw err
            }
            repository.listSessions()
        }
    }

    private fun stopWallPoll(resetCursor: Boolean) {
        wallWorkScheduler.setEnabled(false)
        if (resetCursor) {
            wallWorkScheduler.resetCursor()
        }
    }

    private fun syncWallPollingSchedule() {
        val state = _state.value
        val backgroundServiceEnabled = shouldEnableBackgroundWallService(
            loggedIn = state.loggedIn,
            shareToken = state.shareToken,
            requiresUnlock = state.requiresAppUnlock,
            backgroundWallEnabled = state.backgroundWallEnabled,
        )
        backgroundWallServiceController.setEnabled(backgroundServiceEnabled)
        val enabled = shouldEnableWallWork(
            loggedIn = state.loggedIn,
            shareToken = state.shareToken,
            requiresUnlock = state.requiresAppUnlock,
            backgroundWallEnabled = state.backgroundWallEnabled,
            appInForeground = appInForeground,
            connectionState = state.connectionState,
            hasSocket = socketOpen,
        )
        wallWorkScheduler.setEnabled(enabled)
    }

    private fun handleUnauthorizedResponse() {
        if (!_state.value.loggedIn) {
            _state.update {
                it.copy(
                    loggedIn = false,
                    sessions = emptyList(),
                    activeSessionId = null,
                    wallInactivityEnabled = false,
                    wallInactivityLabel = null,
                    sessionSyncing = false,
                )
            }
            syncWallPollingSchedule()
            return
        }
        setStatus("session expired", StatusLevel.Error)
        handleAuthFailureWithoutLogout()
    }

    private fun handleAuthFailureWithoutLogout() {
        if (!_state.value.loggedIn) {
            disconnectAndReset()
            return
        }
        stopReconnect()
        stopSessionPoll()
        stopWallPoll(resetCursor = true)
        closeWebSocket("session expired")
        repository.clearLastActiveSession()
        resetCurrentSessionState()
        _state.update {
            it.copy(
                sessions = emptyList(),
                activeSessionId = null,
                activeSnapshot = null,
                shareToken = null,
                wallInactivityEnabled = false,
                wallInactivityLabel = null,
                scrollbackOffsetRows = 0,
                showCertificates = false,
                certificateError = null,
                connectionState = ConnectionState.Disconnected,
                hasControl = false,
                lastFrameSeq = 0,
                lastFrameType = null,
                lastFrameAtMs = 0,
                lastFrameError = null,
                requiresAppUnlock = false,
                unlockPromptPending = false,
                sessionSyncing = false,
            )
        }
        syncWallPollingSchedule()
    }

    private fun nextBackoffMs(attempt: Int, retryAfterSeconds: Int?): Int {
        val backoffMs = min(15000, 1000 * (1 shl (attempt - 1)))
        return if (retryAfterSeconds != null && retryAfterSeconds > 0) {
            max(backoffMs, retryAfterSeconds * 1000)
        } else {
            backoffMs
        }
    }

    private fun setStatus(message: String, level: StatusLevel = StatusLevel.Info) {
        transientStatusJob?.cancel()
        transientStatusJob = null
        _state.update {
            it.copy(
                status = StatusMessage(message, level),
                transientStatus = null,
            )
        }
    }

    private fun showTransientStatus(
        message: String,
        level: StatusLevel = StatusLevel.Info,
        timeoutMs: Long = transientStatusDurationMs,
    ) {
        transientStatusJob?.cancel()
        _state.update { it.copy(transientStatus = StatusMessage(message, level)) }
        transientStatusJob = viewModelScope.launch {
            delay(timeoutMs)
            _state.update { state ->
                if (state.transientStatus?.message == message && state.transientStatus.level == level) {
                    state.copy(transientStatus = null)
                } else {
                    state
                }
            }
        }
    }

    fun onAppBackground() {
        onAppBackgroundAt(System.currentTimeMillis())
    }

    @VisibleForTesting
    internal fun onAppBackgroundAt(nowMs: Long) {
        appInForeground = false
        if (_state.value.loggedIn) {
            lastBackgroundAtMs = nowMs
        }
        syncWallPollingSchedule()
    }

    fun onAppForeground() {
        onAppForegroundAt(System.currentTimeMillis())
    }

    @VisibleForTesting
    internal fun onAppForegroundAt(nowMs: Long) {
        appInForeground = true
        viewModelScope.launch {
            val state = _state.value
            if (state.requiresAppUnlock) {
                return@launch
            }
            val timedOut = shouldRequireAppUnlock(
                timeoutMinutes = state.appLockTimeoutMinutes,
                wasLoggedIn = state.loggedIn,
                lastBackgroundMs = lastBackgroundAtMs,
                nowMs = nowMs,
            )
            if (timedOut) {
                stopWallPoll(resetCursor = false)
                _state.update {
                    it.copy(
                        requiresAppUnlock = true,
                        unlockPromptPending = true,
                    )
                }
                syncWallPollingSchedule()
                return@launch
            }
            lastBackgroundAtMs = null
            _state.update { it.copy(requiresAppUnlock = false, unlockPromptPending = false) }
            if (_state.value.shareToken != null) {
                connectActiveSession()
                return@launch
            }
            if (!_state.value.loggedIn) {
                syncWallPollingSchedule()
                return@launch
            }
            if (shouldRecoverConnectionOnForeground(
                    state = _state.value,
                    hasSocket = socketOpen,
                    nowMs = nowMs,
                    lastForegroundRecoveryAtMs = lastForegroundRecoveryAtMs,
                )
            ) {
                lastForegroundRecoveryAtMs = nowMs
                forceRecoverActiveConnection(refreshSessionsFirst = true)
                syncWallPollingSchedule()
                return@launch
            }
            bootstrapSessions()
            syncWallPollingSchedule()
        }
    }

    private fun clearStatus() {
        transientStatusJob?.cancel()
        transientStatusJob = null
        _state.update { it.copy(status = null, transientStatus = null) }
    }

    fun dismissStatus() {
        transientStatusJob?.cancel()
        transientStatusJob = null
        _state.update { state ->
            when {
                state.transientStatus != null -> state.copy(transientStatus = null)
                state.status != null -> state.copy(status = null)
                else -> state
            }
        }
    }

    private fun setBusy(value: Boolean) {
        _state.update { it.copy(isBusy = value) }
    }

    private suspend fun loadTrustedCertificates(endpoint: String) {
        val certs = repository.listTrustedCertificates(endpoint)
        _state.update { it.copy(trustedCerts = certs) }
    }

    private fun normalizeEndpoint(raw: String): String? {
        val trimmed = raw.trim()
        if (trimmed.isBlank()) return null
        if (trimmed.startsWith("https://")) return trimmed
        if (trimmed.contains("://")) return null
        return "https://$trimmed"
    }

    override fun onCleared() {
        transientStatusJob?.cancel()
        transientStatusJob = null
        super.onCleared()
    }

    private data class ConnectionKey(
        val sessionId: String?,
        val shareToken: String?,
    )

    companion object {
        private const val sharedSessionId = "shared"
        private const val MissingSessionGraceMs = 5_000L
        private const val foregroundRecoveryMinIntervalMs = 30_000L
        private const val transientStatusDurationMs = 5000L

        @VisibleForTesting
        internal fun shouldRequireAppUnlock(
            timeoutMinutes: Int,
            wasLoggedIn: Boolean,
            lastBackgroundMs: Long?,
            nowMs: Long,
        ): Boolean {
            if (!wasLoggedIn) return false
            if (timeoutMinutes <= 0) return false
            val backgroundAt = lastBackgroundMs ?: return false
            val elapsed = nowMs - backgroundAt
            if (elapsed <= 0) return false
            return elapsed >= timeoutMinutes * 60_000L
        }

        @VisibleForTesting
        internal fun shouldEnableWallWork(
            loggedIn: Boolean,
            shareToken: String?,
            requiresUnlock: Boolean,
            backgroundWallEnabled: Boolean,
            appInForeground: Boolean,
            connectionState: ConnectionState,
            hasSocket: Boolean,
        ): Boolean {
            if (!loggedIn || !shareToken.isNullOrBlank() || requiresUnlock) {
                return false
            }
            if (!appInForeground) {
                return false
            }
            val connected = connectionState == ConnectionState.Connected && hasSocket
            return !connected
        }

        @VisibleForTesting
        internal fun shouldEnableBackgroundWallService(
            loggedIn: Boolean,
            shareToken: String?,
            requiresUnlock: Boolean,
            backgroundWallEnabled: Boolean,
        ): Boolean {
            if (!loggedIn || !shareToken.isNullOrBlank() || requiresUnlock || !backgroundWallEnabled) {
                return false
            }
            return true
        }

        @VisibleForTesting
        internal fun shouldRecoverConnectionOnForeground(
            state: UiState,
            hasSocket: Boolean,
            nowMs: Long,
            lastForegroundRecoveryAtMs: Long,
        ): Boolean {
            if (state.requiresAppUnlock) return false
            if (!state.canAttach) return false
            if (nowMs - lastForegroundRecoveryAtMs < foregroundRecoveryMinIntervalMs) return false
            if (state.isRefreshing) return false

            val hasRecoverableError = !state.lastFrameError.isNullOrBlank() || !state.status?.message.isNullOrBlank()
            val hasStaleConnectionState = state.connectionState == ConnectionState.Disconnected ||
                state.connectionState == ConnectionState.Waiting ||
                (state.connectionState == ConnectionState.Connected && !hasSocket)

            return hasRecoverableError || hasStaleConnectionState
        }

        private fun sharedSession(): RelaySession {
            return RelaySession(id = sharedSessionId, name = "Shared session", status = "active")
        }
    }
}
