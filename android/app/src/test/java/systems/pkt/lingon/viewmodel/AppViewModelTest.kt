package systems.pkt.lingon.viewmodel

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.resetMain
import okhttp3.CookieJar
import okhttp3.WebSocket
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TestWatcher
import org.junit.runner.Description
import java.nio.file.Files
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import systems.pkt.lingon.data.ApiException
import systems.pkt.lingon.data.HttpClientProvider
import systems.pkt.lingon.data.LingonClient
import systems.pkt.lingon.data.relay.RelaySession
import systems.pkt.lingon.data.relay.RelayWallEventsPage
import systems.pkt.lingon.data.certs.TrustedCert
import systems.pkt.lingon.data.relay.RelayWebSocketClient
import systems.pkt.lingon.data.certs.CertificateStore
import systems.pkt.lingon.protocol.ScrollbackRow
import systems.pkt.lingon.terminal.TerminalSnapshot

@OptIn(ExperimentalCoroutinesApi::class)
class AppViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun handleSharedTokenUpdatesState() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)

        viewModel.handleSharedToken("token", "https://example")

        val state = viewModel.state.value
        assertFalse(state.loggedIn)
        assertEquals("token", state.shareToken)
        assertEquals("shared", state.activeSessionId)
        assertEquals(1, state.sessions.size)
    }

    @Test
    fun selectSessionOnActiveTabKeepsVisibleSnapshot() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)

        viewModel.handleSharedToken("token", "https://example")
        advanceUntilIdle()

        val seededSnapshot = TerminalSnapshot(
            cols = 4,
            rows = 2,
            runes = IntArray(8),
            modes = IntArray(8),
            fg = IntArray(8),
            bg = IntArray(8),
            graphemes = null,
            cursorX = 0,
            cursorY = 0,
            cursorVisible = true,
            mode = 0,
            title = "",
        )
        setActiveSnapshotForTest(viewModel, seededSnapshot)
        val before = viewModel.state.value.activeSnapshot
        assertNotNull(before)

        viewModel.selectSession("shared")

        val after = viewModel.state.value.activeSnapshot
        assertSame(before, after)
    }

    @Test
    fun bootstrapRestoresLastActiveSessionForEndpoint() = runTest {
        val repository = FakeRepository(
            sessions = listOf(
                RelaySession(id = "alpha", name = "Alpha", status = "active"),
                RelaySession(id = "beta", name = "Beta", status = "active"),
            ),
            failListSessions = false,
            initialLastActiveSessionByEndpoint = mapOf("https://localhost:12843/v1" to "beta"),
        )
        val wsClient = FakeWsClient()

        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        assertEquals("beta", viewModel.state.value.activeSessionId)
    }

    @Test
    fun connectActiveSessionFallsBackToFirstSessionWhenActiveMissing() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "stale",
                shareToken = null,
                connectionState = ConnectionState.Disconnected,
            ),
        )
        viewModel.selectSession("stale")
        advanceUntilIdle()

        assertEquals("host-1", viewModel.state.value.activeSessionId)
        assertEquals(1, wsClient.connectCount)
        assertEquals("host-1", wsClient.lastConnectOptions?.sessionId)
    }

    @Test
    fun connectActiveSessionContinuesWhenRefreshAuthReturnsFalse() = runTest {
        val repository = FakeRepository(
            refreshAuthResult = false,
        )
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                shareToken = null,
                connectionState = ConnectionState.Disconnected,
            ),
        )
        viewModel.selectSession("host-1")
        advanceUntilIdle()

        assertEquals(1, repository.refreshAuthCalls)
        assertEquals(1, wsClient.connectCount)
        assertEquals("host-1", viewModel.state.value.activeSessionId)
    }

    @Test
    fun connectActiveSessionContinuesWhenRefreshAuthThrows() = runTest {
        val repository = FakeRepository(
            refreshAuthError = RuntimeException("network down"),
        )
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                shareToken = null,
                connectionState = ConnectionState.Disconnected,
            ),
        )
        viewModel.selectSession("host-1")
        advanceUntilIdle()

        assertEquals(1, repository.refreshAuthCalls)
        assertEquals(1, wsClient.connectCount)
        assertEquals("host-1", viewModel.state.value.activeSessionId)
    }

    @Test
    fun onAppForegroundRequiresUnlockAfterTimeout() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)

        viewModel.login("user", "pass", "")
        advanceUntilIdle()

        viewModel.onAppBackgroundAt(0L)
        viewModel.onAppForegroundAt(31 * 60_000L)
        advanceUntilIdle()

        val state = viewModel.state.value
        assertTrue(state.requiresAppUnlock)
        assertTrue(state.unlockPromptPending)
    }

    @Test
    fun unlockDisabledDoesNotRequireUnlock() = runTest {
        val repository = FakeRepository(appLockMinutes = 0)
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)

        viewModel.login("user", "pass", "")
        advanceUntilIdle()

        viewModel.onAppBackgroundAt(0L)
        viewModel.onAppForegroundAt(120 * 60_000L)
        advanceUntilIdle()

        val state = viewModel.state.value
        assertFalse(state.requiresAppUnlock)
    }

    @Test
    fun shouldEnableWallWorkConnectedForegroundIsFalse() {
        val enabled = AppViewModel.shouldEnableWallWork(
            loggedIn = true,
            shareToken = null,
            requiresUnlock = false,
            appInForeground = true,
            connectionState = ConnectionState.Connected,
            hasSocket = true,
        )
        assertFalse(enabled)
    }

    @Test
    fun shouldEnableWallWorkDisconnectedForegroundIsTrue() {
        val enabled = AppViewModel.shouldEnableWallWork(
            loggedIn = true,
            shareToken = null,
            requiresUnlock = false,
            appInForeground = true,
            connectionState = ConnectionState.Disconnected,
            hasSocket = false,
        )
        assertTrue(enabled)
    }

    @Test
    fun shouldEnableWallWorkBackgroundLoggedInIsTrue() {
        val enabled = AppViewModel.shouldEnableWallWork(
            loggedIn = true,
            shareToken = null,
            requiresUnlock = false,
            appInForeground = false,
            connectionState = ConnectionState.Connected,
            hasSocket = true,
        )
        assertTrue(enabled)
    }

    @Test
    fun shouldEnableWallWorkLoggedOutOrShareTokenIsFalse() {
        val loggedOut = AppViewModel.shouldEnableWallWork(
            loggedIn = false,
            shareToken = null,
            requiresUnlock = false,
            appInForeground = false,
            connectionState = ConnectionState.Disconnected,
            hasSocket = false,
        )
        assertFalse(loggedOut)
        val shareTokenMode = AppViewModel.shouldEnableWallWork(
            loggedIn = true,
            shareToken = "token",
            requiresUnlock = false,
            appInForeground = false,
            connectionState = ConnectionState.Disconnected,
            hasSocket = false,
        )
        assertFalse(shareTokenMode)
    }

    @Test
    fun adjustScrollbackUpdatesExposedOffset() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)

        val liveSnapshot = TerminalSnapshot(
            cols = 4,
            rows = 2,
            runes = IntArray(8),
            modes = IntArray(8),
            fg = IntArray(8),
            bg = IntArray(8),
            graphemes = null,
            cursorX = 0,
            cursorY = 0,
            cursorVisible = true,
            mode = 0,
            title = "",
        )
        setLiveSnapshotForTest(viewModel, liveSnapshot)
        setScrollbackRowsForTest(
            viewModel,
            listOf(
                ScrollbackRow.newBuilder().addRunes('A'.code).addModes(0).addFg(0).addBg(0).build(),
                ScrollbackRow.newBuilder().addRunes('B'.code).addModes(0).addFg(0).addBg(0).build(),
            ),
        )

        viewModel.adjustScrollback(1)
        assertEquals(1, viewModel.state.value.scrollbackOffsetRows)

        viewModel.adjustScrollback(-1)
        assertEquals(0, viewModel.state.value.scrollbackOffsetRows)
    }

    @Test
    fun updateZoomFactorDebouncesPersistence() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        viewModel.updateZoomFactor(1.3f)
        viewModel.updateZoomFactor(1.6f)
        assertEquals(1.6f, viewModel.state.value.zoomFactor, 0.0001f)
        assertEquals(0, repository.setZoomCalls)

        advanceTimeBy(200)
        advanceUntilIdle()
        assertEquals(1, repository.setZoomCalls)
        assertEquals(1.6f, repository.lastZoom, 0.0001f)
    }
}

@OptIn(ExperimentalCoroutinesApi::class)
class MainDispatcherRule : TestWatcher() {
    private val dispatcher = StandardTestDispatcher()

    override fun starting(description: Description) {
        Dispatchers.setMain(dispatcher)
    }

    override fun finished(description: Description) {
        Dispatchers.resetMain()
    }
}

private class FakeRepository(
    appLockMinutes: Int = 30,
    private val sessions: List<RelaySession> = emptyList(),
    private val failListSessions: Boolean = true,
    private val refreshAuthResult: Boolean = true,
    private val refreshAuthError: Throwable? = null,
    initialLastActiveSessionByEndpoint: Map<String, String> = emptyMap(),
) : LingonClient {
    override val endpointFlow: Flow<String> = MutableStateFlow("https://localhost:12843/v1")
    override val fontSizeFlow: Flow<Int> = MutableStateFlow(14)
    override val zoomFlow: Flow<Float> = MutableStateFlow(1.0f)
    override val resizeHostFlow: Flow<Boolean> = MutableStateFlow(false)
    override val appLockTimeoutMinutesFlow: Flow<Int> = MutableStateFlow(appLockMinutes)
    override val savedEndpointsFlow: Flow<List<String>> = MutableStateFlow(listOf("https://localhost:12843/v1"))
    override val certificatesFlow: Flow<Map<String, List<TrustedCert>>> = MutableStateFlow(emptyMap())
    private val lastActiveSessionByEndpoint = initialLastActiveSessionByEndpoint.toMutableMap()
    var refreshAuthCalls: Int = 0
    var setZoomCalls: Int = 0
    var lastZoom: Float = 1.0f

    override fun setEndpoint(value: String) {
        // no-op
    }

    override fun setFontSize(value: Int) {
        // no-op
    }

    override fun setZoom(value: Float) {
        setZoomCalls += 1
        lastZoom = value
    }

    override fun setResizeHostEnabled(value: Boolean) {
        // no-op
    }

    override fun setAppLockTimeoutMinutes(value: Int) {
        // no-op
    }

    override fun saveLastActiveSessionId(endpoint: String, sessionId: String) {
        val cleanedEndpoint = endpoint.trim()
        val cleanedSessionId = sessionId.trim()
        if (cleanedEndpoint.isBlank() || cleanedSessionId.isBlank()) return
        lastActiveSessionByEndpoint[cleanedEndpoint] = cleanedSessionId
    }

    override fun clearLastActiveSession() {
        lastActiveSessionByEndpoint.clear()
    }

    override suspend fun login(username: String, password: String, totp: String) {
        // no-op
    }

    override suspend fun logout() {
        // no-op
    }

    override suspend fun clearAuth() {
        // no-op
    }

    override suspend fun refreshAuth(): Boolean {
        refreshAuthCalls += 1
        refreshAuthError?.let { throw it }
        return refreshAuthResult
    }

    override suspend fun loadLastActiveSessionId(endpoint: String): String? {
        val cleanedEndpoint = endpoint.trim()
        if (cleanedEndpoint.isBlank()) return null
        return lastActiveSessionByEndpoint[cleanedEndpoint]
    }

    override suspend fun listSessions(): List<RelaySession> {
        if (failListSessions) {
            throw ApiException("invalid session", 401)
        }
        return sessions
    }

    override suspend fun listWallEvents(sinceId: Long, limit: Int): RelayWallEventsPage {
        return RelayWallEventsPage()
    }

    override fun streamSessions(): Flow<List<RelaySession>> = emptyFlow()

    override suspend fun listTrustedCertificates(endpoint: String): List<TrustedCert> = emptyList()

    override suspend fun addTrustedCertificates(endpoint: String, pem: String): List<TrustedCert> = emptyList()

    override suspend fun removeTrustedCertificate(endpoint: String, certId: String) {
        // no-op
    }
}

private class FakeWsClient : RelayWebSocketClient(
    testHttpClientProvider(),
) {
    var connectCount: Int = 0
    var lastConnectOptions: ConnectOptions? = null

    override fun connect(options: ConnectOptions, listener: Listener): WebSocket {
        connectCount += 1
        lastConnectOptions = options
        return object : WebSocket {
            override fun request(): okhttp3.Request = okhttp3.Request.Builder().url(options.baseUrl).build()
            override fun queueSize(): Long = 0
            override fun send(text: String): Boolean = true
            override fun send(bytes: okio.ByteString): Boolean = true
            override fun close(code: Int, reason: String?): Boolean = true
            override fun cancel() {}
        }
    }

    override fun sendInput(webSocket: WebSocket, data: ByteArray) {
        // no-op
    }

    override fun sendInput(webSocket: WebSocket, text: String) {
        // no-op
    }

    override fun sendResize(webSocket: WebSocket, cols: Int, rows: Int) {
        // no-op
    }
}

private fun setActiveSnapshotForTest(viewModel: AppViewModel, snapshot: TerminalSnapshot) {
    val stateField = AppViewModel::class.java.getDeclaredField("_state")
    stateField.isAccessible = true
    @Suppress("UNCHECKED_CAST")
    val stateFlow = stateField.get(viewModel) as MutableStateFlow<UiState>
    stateFlow.value = stateFlow.value.copy(activeSnapshot = snapshot)
}

private fun setLiveSnapshotForTest(viewModel: AppViewModel, snapshot: TerminalSnapshot) {
    val field = AppViewModel::class.java.getDeclaredField("liveSnapshot")
    field.isAccessible = true
    field.set(viewModel, snapshot)
}

private fun setScrollbackRowsForTest(viewModel: AppViewModel, rows: List<ScrollbackRow>) {
    val field = AppViewModel::class.java.getDeclaredField("scrollbackRows")
    field.isAccessible = true
    @Suppress("UNCHECKED_CAST")
    val value = field.get(viewModel) as ArrayList<ScrollbackRow>
    value.clear()
    value.addAll(rows)
}

private fun setUiStateForTest(viewModel: AppViewModel, state: UiState) {
    val stateField = AppViewModel::class.java.getDeclaredField("_state")
    stateField.isAccessible = true
    @Suppress("UNCHECKED_CAST")
    val stateFlow = stateField.get(viewModel) as MutableStateFlow<UiState>
    stateFlow.value = state
}

private fun testHttpClientProvider(): HttpClientProvider {
    val dataStore = PreferenceDataStoreFactory.create(
        scope = CoroutineScope(Dispatchers.IO + SupervisorJob()),
        produceFile = { Files.createTempFile("lingon", ".preferences_pb").toFile() },
    )
    val certStore = CertificateStore(dataStore)
    return HttpClientProvider(certStore, CookieJar.NO_COOKIES)
}
