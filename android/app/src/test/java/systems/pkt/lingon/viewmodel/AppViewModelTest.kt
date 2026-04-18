package systems.pkt.lingon.viewmodel

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
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
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TestWatcher
import org.junit.runner.Description
import java.nio.file.Files
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import systems.pkt.lingon.data.ApiException
import systems.pkt.lingon.data.HttpClientProvider
import systems.pkt.lingon.data.LingonClient
import systems.pkt.lingon.data.WallWorkStateStore
import systems.pkt.lingon.data.relay.RelaySession
import systems.pkt.lingon.data.relay.RelayWallEventsPage
import systems.pkt.lingon.data.certs.TrustedCert
import systems.pkt.lingon.data.relay.RelayWebSocketClient
import systems.pkt.lingon.data.certs.CertificateStore
import systems.pkt.lingon.notifications.MonotonicWallDeliveryCoordinator
import systems.pkt.lingon.protocol.Diff
import systems.pkt.lingon.protocol.Frame
import systems.pkt.lingon.protocol.CommandKind
import systems.pkt.lingon.protocol.Scrollback
import systems.pkt.lingon.protocol.Snapshot
import systems.pkt.lingon.protocol.Welcome
import systems.pkt.lingon.protocol.WallInactivityStatus
import systems.pkt.lingon.protocol.ScrollbackRow
import systems.pkt.lingon.DefaultTerminalZoom
import systems.pkt.lingon.MaxTerminalZoom
import systems.pkt.lingon.terminal.TerminalSnapshot
import systems.pkt.lingon.ui.SNAPSHOT_MODE_APP_CURSOR
import systems.pkt.lingon.work.BackgroundWallServiceController

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
        setWebSocketForTest(viewModel, wsClient.fakeSocket)
        setActiveConnectionForTest(viewModel, null, "token")
        val before = viewModel.state.value.activeSnapshot
        assertNotNull(before)

        viewModel.selectSession("shared")

        val after = viewModel.state.value.activeSnapshot
        assertSame(before, after)
    }

    @Test
    fun selectSessionOnActiveTabRehydratesLiveCacheAfterStaleRender() = runTest {
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
            title = "current",
        )
        val staleSnapshot = TerminalSnapshot(
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
            title = "stale",
        )
        setLiveSnapshotForTest(viewModel, liveSnapshot)
        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(
                    RelaySession(id = "shared", name = "Shared", status = "active"),
                ),
                activeSessionId = "shared",
                activeSnapshot = staleSnapshot,
                scrollbackOffsetRows = 4,
                connectionState = ConnectionState.Connected,
                shareToken = null,
            ),
        )
        setWebSocketForTest(viewModel, wsClient.fakeSocket)
        setActiveConnectionForTest(viewModel, "shared", null)

        viewModel.selectSession("shared")

        assertEquals("current", viewModel.state.value.activeSnapshot?.title)
        assertEquals(0, viewModel.state.value.scrollbackOffsetRows)
        assertEquals(0, wsClient.connectCount)
    }

    @Test
    fun selectSessionDoesNotInheritViewportResetTokenWhenSwitchingTabs() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(
                    RelaySession(id = "alpha", name = "Alpha", status = "active"),
                    RelaySession(id = "beta", name = "Beta", status = "active"),
                ),
                activeSessionId = "alpha",
                panResetNonce = 7,
                connectionState = ConnectionState.Connected,
                shareToken = null,
            ),
        )

        viewModel.selectSession("beta")

        assertEquals("beta", viewModel.state.value.activeSessionId)
        assertEquals(0, viewModel.state.value.panResetNonce)
    }

    @Test
    fun selectSessionRestoresPerSessionCacheAndLastSeq() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient { options, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
            when (options.sessionId) {
                "alpha" -> {
                    if (options.lastSeq == 0L) {
                        listener.onFrame(socket, snapshotFrame(1, "alpha-1"))
                        listener.onFrame(socket, diffFrame(2, "alpha-2"))
                    }
                }
                "beta" -> {
                    listener.onFrame(socket, snapshotFrame(1, "beta-1"))
                }
            }
        }
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(
                    RelaySession(id = "alpha", name = "Alpha", status = "active"),
                    RelaySession(id = "beta", name = "Beta", status = "active"),
                ),
                activeSessionId = "alpha",
                connectionState = ConnectionState.Disconnected,
                shareToken = null,
            ),
        )

        viewModel.selectSession("alpha")
        advanceUntilIdle()
        assertEquals(1, wsClient.connectCount)
        wsClient.fireConnect()
        advanceUntilIdle()

        assertEquals("alpha-2", viewModel.state.value.activeSnapshot?.title)
        assertEquals(2L, viewModel.state.value.lastFrameSeq)

        viewModel.selectSession("beta")
        advanceUntilIdle()
        assertEquals(2, wsClient.connectCount)
        assertNull(viewModel.state.value.activeSnapshot)
        wsClient.fireConnect()
        advanceUntilIdle()

        assertEquals("beta-1", viewModel.state.value.activeSnapshot?.title)
        assertEquals(1L, viewModel.state.value.lastFrameSeq)

        viewModel.selectSession("alpha")
        advanceUntilIdle()
        assertEquals(3, wsClient.connectCount)
        wsClient.fireConnect()
        advanceUntilIdle()

        assertEquals("alpha", wsClient.lastConnectOptions?.sessionId)
        assertEquals(2L, wsClient.lastConnectOptions?.lastSeq)
        assertEquals("alpha-2", viewModel.state.value.activeSnapshot?.title)
    }

    @Test
    fun selectSessionWithCacheRestoresImmediatelyBeforeReconnect() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient { options, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
            when (options.sessionId) {
                "alpha" -> {
                    if (options.lastSeq == 0L) {
                        listener.onFrame(socket, snapshotFrame(1, "alpha-1"))
                        listener.onFrame(socket, diffFrame(2, "alpha-2"))
                    }
                }
            }
        }
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(
                    RelaySession(id = "alpha", name = "Alpha", status = "active"),
                    RelaySession(id = "beta", name = "Beta", status = "active"),
                ),
                activeSessionId = "alpha",
                connectionState = ConnectionState.Disconnected,
                shareToken = null,
            ),
        )

        viewModel.selectSession("alpha")
        advanceUntilIdle()
        wsClient.fireConnect()
        advanceUntilIdle()

        assertEquals("alpha-2", viewModel.state.value.activeSnapshot?.title)

        setScrollbackRowsForTest(
            viewModel,
            listOf(
                ScrollbackRow.newBuilder().addRunes('A'.code).addModes(0).addFg(0).addBg(0).build(),
                ScrollbackRow.newBuilder().addRunes('B'.code).addModes(0).addFg(0).addBg(0).build(),
            ),
        )
        viewModel.adjustScrollback(1)
        assertEquals(1, viewModel.state.value.scrollbackOffsetRows)

        viewModel.selectSession("beta")
        advanceUntilIdle()
        assertNull(viewModel.state.value.activeSnapshot)

        viewModel.selectSession("alpha")
        advanceUntilIdle()
        assertEquals("alpha-2", viewModel.state.value.activeSnapshot?.title)
        assertEquals(2L, wsClient.lastConnectOptions?.lastSeq)
        assertEquals(1, viewModel.state.value.scrollbackOffsetRows)
        assertEquals(2L, viewModel.state.value.lastFrameSeq)
    }

    @Test
    fun selectSessionRestoresLastSeqInCacheWithoutFullReplay() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient { options, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
            if (options.lastSeq == 0L) {
                listener.onFrame(socket, snapshotFrame(1, "alpha-1"))
                listener.onFrame(socket, diffFrame(2, "alpha-2"))
            }
        }
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(
                    RelaySession(id = "alpha", name = "Alpha", status = "active"),
                    RelaySession(id = "beta", name = "Beta", status = "active"),
                ),
                activeSessionId = "alpha",
                connectionState = ConnectionState.Disconnected,
                shareToken = null,
            ),
        )

        viewModel.selectSession("alpha")
        advanceUntilIdle()
        wsClient.fireConnect()
        advanceUntilIdle()

        assertEquals(1, wsClient.connectCount)
        assertEquals("alpha", wsClient.lastConnectOptions?.sessionId)
        assertEquals(0L, wsClient.lastConnectOptions?.lastSeq)
        assertEquals("alpha-2", viewModel.state.value.activeSnapshot?.title)
        assertEquals(2L, viewModel.state.value.lastFrameSeq)

        viewModel.selectSession("beta")
        advanceUntilIdle()
        assertEquals(2, wsClient.connectCount)
        wsClient.fireConnect()
        advanceUntilIdle()

        assertEquals("beta", wsClient.lastConnectOptions?.sessionId)
        assertEquals(0L, wsClient.lastConnectOptions?.lastSeq)

        viewModel.selectSession("alpha")
        advanceUntilIdle()
        assertEquals("alpha", wsClient.lastConnectOptions?.sessionId)
        assertEquals(2L, wsClient.lastConnectOptions?.lastSeq)
        assertEquals(3, wsClient.connectCount)
        wsClient.fireConnect()
        advanceUntilIdle()
        assertEquals("alpha-2", viewModel.state.value.activeSnapshot?.title)
    }

    @Test
    fun transientlyMissingActiveSessionStaysVisibleDuringGracePeriod() = runTest {
        var nowMs = 1_000L
        var currentSessions = listOf(
            RelaySession(id = "alpha", name = "Alpha", status = "active"),
            RelaySession(id = "beta", name = "Beta", status = "active"),
        )
        val repository = FakeRepository(
            failListSessions = false,
            sessionProvider = { currentSessions },
        )
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)
        viewModel.nowProvider = { nowMs }
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = currentSessions,
                activeSessionId = "beta",
                connectionState = ConnectionState.Connected,
                shareToken = null,
            ),
        )

        currentSessions = listOf(
            RelaySession(id = "alpha", name = "Alpha", status = "active"),
        )
        viewModel.manualRefresh()
        advanceUntilIdle()

        assertEquals("beta", viewModel.state.value.activeSessionId)
        assertEquals(listOf("alpha", "beta"), viewModel.state.value.sessions.map { it.id })

        nowMs += 6_000L
        viewModel.manualRefresh()
        advanceUntilIdle()

        assertEquals("alpha", viewModel.state.value.activeSessionId)
        assertEquals(listOf("alpha"), viewModel.state.value.sessions.map { it.id })
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
            backgroundWallEnabled = false,
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
            backgroundWallEnabled = false,
            appInForeground = true,
            connectionState = ConnectionState.Disconnected,
            hasSocket = false,
        )
        assertTrue(enabled)
    }

    @Test
    fun shouldEnableWallWorkBackgroundLoggedInIsFalse() {
        val enabled = AppViewModel.shouldEnableWallWork(
            loggedIn = true,
            shareToken = null,
            requiresUnlock = false,
            backgroundWallEnabled = false,
            appInForeground = false,
            connectionState = ConnectionState.Connected,
            hasSocket = true,
        )
        assertFalse(enabled)
    }

    @Test
    fun shouldEnableWallWorkLoggedOutOrShareTokenIsFalse() {
        val loggedOut = AppViewModel.shouldEnableWallWork(
            loggedIn = false,
            shareToken = null,
            requiresUnlock = false,
            backgroundWallEnabled = false,
            appInForeground = false,
            connectionState = ConnectionState.Disconnected,
            hasSocket = false,
        )
        assertFalse(loggedOut)
        val shareTokenMode = AppViewModel.shouldEnableWallWork(
            loggedIn = true,
            shareToken = "token",
            requiresUnlock = false,
            backgroundWallEnabled = false,
            appInForeground = false,
            connectionState = ConnectionState.Disconnected,
            hasSocket = false,
        )
        assertFalse(shareTokenMode)
    }

    @Test
    fun shouldEnableWallWorkBackgroundLiveModeDisablesWorker() {
        val enabled = AppViewModel.shouldEnableWallWork(
            loggedIn = true,
            shareToken = null,
            requiresUnlock = false,
            backgroundWallEnabled = true,
            appInForeground = false,
            connectionState = ConnectionState.Disconnected,
            hasSocket = false,
        )
        assertFalse(enabled)
    }

    @Test
    fun shouldEnableWallWorkBackgroundDisabledSettingKeepsWorkerOff() {
        val enabled = AppViewModel.shouldEnableWallWork(
            loggedIn = true,
            shareToken = null,
            requiresUnlock = false,
            backgroundWallEnabled = false,
            appInForeground = false,
            connectionState = ConnectionState.Disconnected,
            hasSocket = false,
        )
        assertFalse(enabled)
    }

    @Test
    fun shouldEnableBackgroundWallServiceOnlyWhenLoggedInBackgroundAndEnabled() {
        assertTrue(
            AppViewModel.shouldEnableBackgroundWallService(
                loggedIn = true,
                shareToken = null,
                backgroundWallEnabled = true,
                appInForeground = false,
            ),
        )
        assertFalse(
            AppViewModel.shouldEnableBackgroundWallService(
                loggedIn = true,
                shareToken = null,
                backgroundWallEnabled = true,
                appInForeground = true,
            ),
        )
        assertFalse(
            AppViewModel.shouldEnableBackgroundWallService(
                loggedIn = false,
                shareToken = null,
                backgroundWallEnabled = true,
                appInForeground = false,
            ),
        )
        assertFalse(
            AppViewModel.shouldEnableBackgroundWallService(
                loggedIn = true,
                shareToken = "token",
                backgroundWallEnabled = true,
                appInForeground = false,
            ),
        )
    }

    @Test
    fun onAppBackgroundEnablesBackgroundWallServiceWhenConfigured() = runTest {
        val repository = FakeRepository(backgroundWallEnabled = true)
        val wsClient = FakeWsClient()
        val controller = RecordingBackgroundWallServiceController()
        val viewModel = AppViewModel(
            repository,
            wsClient,
            backgroundWallServiceController = controller,
        )
        advanceUntilIdle()
        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                shareToken = null,
            ),
        )

        viewModel.onAppBackgroundAt(nowMs = 1234L)
        advanceUntilIdle()

        assertTrue(controller.enabledValues.contains(true))
        assertEquals(true, controller.enabledValues.lastOrNull())
    }

    @Test
    fun onAppForegroundDisablesBackgroundWallServiceAfterBackgroundEnable() = runTest {
        val repository = FakeRepository(backgroundWallEnabled = true)
        val wsClient = FakeWsClient()
        val controller = RecordingBackgroundWallServiceController()
        val viewModel = AppViewModel(
            repository,
            wsClient,
            backgroundWallServiceController = controller,
        )
        advanceUntilIdle()
        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                shareToken = null,
            ),
        )

        viewModel.onAppBackgroundAt(nowMs = 1234L)
        advanceUntilIdle()
        viewModel.onAppForegroundAt(nowMs = 2234L)
        advanceUntilIdle()

        assertTrue(controller.enabledValues.contains(true))
        assertEquals(false, controller.enabledValues.lastOrNull())
    }

    @Test
    fun onAppBackgroundKeepsBackgroundWallServiceDisabledForShareToken() = runTest {
        val repository = FakeRepository(backgroundWallEnabled = true)
        val wsClient = FakeWsClient()
        val controller = RecordingBackgroundWallServiceController()
        val viewModel = AppViewModel(
            repository,
            wsClient,
            backgroundWallServiceController = controller,
        )
        advanceUntilIdle()
        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = false,
                shareToken = "token",
            ),
        )

        viewModel.onAppBackgroundAt(nowMs = 1234L)
        advanceUntilIdle()

        assertFalse(controller.enabledValues.contains(true))
        assertEquals(false, controller.enabledValues.lastOrNull())
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
    fun updateZoomFactorPersistsPerSessionAndRestoresOnTabSwitch() = runTest {
        val repository = FakeRepository(
            initialSessionZooms = mapOf(
                "https://localhost:12843/v1|host-1" to 1.2f,
                "https://localhost:12843/v1|host-2" to 1.4f,
            ),
        )
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(
                    RelaySession(id = "host-1", name = "Host 1", status = "active"),
                    RelaySession(id = "host-2", name = "Host 2", status = "active"),
                ),
                activeSessionId = "host-1",
            ),
        )

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        assertEquals(1.2f, viewModel.state.value.zoomFactor, 0.0001f)

        viewModel.updateZoomFactor(1.3f)
        viewModel.updateZoomFactor(1.6f)
        assertEquals(1.6f, viewModel.state.value.zoomFactor, 0.0001f)
        assertEquals(0, repository.saveZoomCalls)

        advanceTimeBy(200)
        advanceUntilIdle()
        assertEquals(1, repository.saveZoomCalls)
        assertEquals(1.6f, repository.savedZooms["https://localhost:12843/v1|host-1"]!!, 0.0001f)

        viewModel.selectSession("host-2")
        advanceUntilIdle()
        assertEquals(1.4f, viewModel.state.value.zoomFactor, 0.0001f)

        viewModel.updateZoomFactor(1.8f)
        advanceTimeBy(200)
        advanceUntilIdle()
        assertEquals(2, repository.saveZoomCalls)
        assertEquals(1.8f, repository.savedZooms["https://localhost:12843/v1|host-2"]!!, 0.0001f)

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        assertEquals(1.6f, viewModel.state.value.zoomFactor, 0.0001f)
    }

    @Test
    fun resetZoomAndPanOnlyAffectsActiveSession() = runTest {
        val repository = FakeRepository(
            initialSessionZooms = mapOf(
                "https://localhost:12843/v1|host-1" to 1.7f,
                "https://localhost:12843/v1|host-2" to 1.4f,
            ),
        )
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(
                    RelaySession(id = "host-1", name = "Host 1", status = "active"),
                    RelaySession(id = "host-2", name = "Host 2", status = "active"),
                ),
                activeSessionId = "host-1",
            ),
        )

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        assertEquals(1.7f, viewModel.state.value.zoomFactor, 0.0001f)

        viewModel.resetZoomAndPan()
        advanceUntilIdle()
        assertEquals(DefaultTerminalZoom, viewModel.state.value.zoomFactor, 0.0001f)
        assertEquals(DefaultTerminalZoom, repository.savedZooms["https://localhost:12843/v1|host-1"]!!, 0.0001f)

        viewModel.selectSession("host-2")
        advanceUntilIdle()
        assertEquals(1.4f, viewModel.state.value.zoomFactor, 0.0001f)
        assertEquals(0, viewModel.state.value.panResetNonce)
    }

    @Test
    fun updateZoomFactorClampsAtRaisedMaxZoom() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
            ),
        )

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        viewModel.updateZoomFactor(MaxTerminalZoom + 1.5f)
        advanceTimeBy(200)
        advanceUntilIdle()

        assertEquals(MaxTerminalZoom, viewModel.state.value.zoomFactor, 0.0001f)
        assertEquals(MaxTerminalZoom, repository.savedZooms["https://localhost:12843/v1|host-1"]!!, 0.0001f)
    }

    @Test
    fun setBackgroundWallEnabledIgnoresStaleRepositoryEmission() = runTest {
        val backgroundWallFlow = MutableSharedFlow<Boolean>(extraBufferCapacity = 4)
        val repository = FakeRepository(backgroundWallEnabledFlowOverride = backgroundWallFlow)
        repository.onSetBackgroundWallEnabled = { _ ->
            backgroundWallFlow.tryEmit(false)
        }
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        viewModel.setBackgroundWallEnabled(true)
        advanceUntilIdle()

        assertTrue(viewModel.state.value.backgroundWallEnabled)
    }

    @Test
    fun sendRawBytesTranslatesArrowKeysWhenAppCursorModeActive() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        val liveSnapshot = TerminalSnapshot(
            cols = 80,
            rows = 24,
            runes = IntArray(80 * 24),
            modes = IntArray(80 * 24),
            fg = IntArray(80 * 24),
            bg = IntArray(80 * 24),
            graphemes = null,
            cursorX = 0,
            cursorY = 0,
            cursorVisible = true,
            mode = SNAPSHOT_MODE_APP_CURSOR,
            title = "",
        )
        setLiveSnapshotForTest(viewModel, liveSnapshot)
        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                connectionState = ConnectionState.Connected,
                activeSnapshot = liveSnapshot,
            ),
        )
        setWebSocketForTest(viewModel, wsClient.fakeSocket)

        viewModel.sendRawBytes("\u001b[B".encodeToByteArray())

        assertArrayEquals("\u001bOB".encodeToByteArray(), wsClient.lastSentBytes)
    }

    @Test
    fun manualRefreshForcesReconnectForActiveSession() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
        }
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Connected,
            ),
        )
        setActiveConnectionForTest(viewModel, "host-1", null)
        viewModel.selectSession("host-1")
        advanceUntilIdle()
        wsClient.fireConnect()
        runCurrent()

        assertEquals(1, wsClient.connectCount)
        assertEquals("host-1", wsClient.lastConnectOptions?.sessionId)
        wsClient.connectCount = 0
        wsClient.closeCount = 0

        viewModel.manualRefresh()
        advanceUntilIdle()

        assertEquals(1, wsClient.closeCount)
        assertEquals(1, wsClient.connectCount)
        assertEquals("host-1", wsClient.lastConnectOptions?.sessionId)
    }

    @Test
    fun manualRefreshForcesFullSnapshotReconnect() = runTest {
        val sessionConnects = mutableListOf<Long>()
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { options, listener, socket ->
            sessionConnects.add(options.lastSeq)
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
            if (options.lastSeq == 0L) {
                val title = if (sessionConnects.size == 1) "initial" else "reloaded"
                val seq = if (sessionConnects.size == 1) 1L else 2L
                listener.onFrame(socket, snapshotFrame(seq, title))
            }
        }
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(
                    RelaySession(id = "host-1", name = "Host 1", status = "active"),
                ),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Disconnected,
                shareToken = null,
            ),
        )

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        wsClient.fireConnect()
        runCurrent()

        assertEquals(1, wsClient.connectCount)
        assertEquals("initial", viewModel.state.value.activeSnapshot?.title)

        viewModel.manualRefresh()
        advanceUntilIdle()
        wsClient.fireConnect()
        advanceUntilIdle()

        assertEquals(2, wsClient.connectCount)
        assertEquals(0L, wsClient.lastConnectOptions?.lastSeq)
        assertEquals("reloaded", viewModel.state.value.activeSnapshot?.title)
    }

    @Test
    fun manualRefreshPreservesScrollbackOffset() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
            listener.onFrame(socket, snapshotFrame(1, "initial"))
            listener.onFrame(socket, diffFrame(2, "initial-diff"))
        }
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(
                    RelaySession(id = "host-1", name = "Host 1", status = "active"),
                ),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Disconnected,
                shareToken = null,
            ),
        )

        setScrollbackRowsForTest(
            viewModel,
            listOf(
                ScrollbackRow.newBuilder().addRunes('A'.code).addModes(0).addFg(0).addBg(0).build(),
                ScrollbackRow.newBuilder().addRunes('B'.code).addModes(0).addFg(0).addBg(0).build(),
                ScrollbackRow.newBuilder().addRunes('C'.code).addModes(0).addFg(0).addBg(0).build(),
            ),
        )

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        wsClient.fireConnect()
        advanceUntilIdle()
        assertEquals(0L, wsClient.lastConnectOptions?.lastSeq)
        assertEquals("initial-diff", viewModel.state.value.activeSnapshot?.title)

        viewModel.adjustScrollback(2)
        assertEquals(2, viewModel.state.value.scrollbackOffsetRows)

        wsClient.onConnect = { options, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
            listener.onFrame(socket, snapshotFrame(3, "reloaded"))
        }
        viewModel.manualRefresh()
        advanceUntilIdle()
        wsClient.fireConnect()
        advanceUntilIdle()

        assertEquals("reloaded", viewModel.state.value.activeSnapshot?.title)
        assertEquals(0L, wsClient.lastConnectOptions?.lastSeq)
        assertEquals(2, viewModel.state.value.scrollbackOffsetRows)
    }

    @Test
    fun toggleWallInactivitySendsCycleCommandForActiveSession() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Connected,
                shareToken = null,
            ),
        )
        setWebSocketForTest(viewModel, wsClient.fakeSocket)
        setActiveConnectionForTest(viewModel, "host-1", null)

        viewModel.toggleWallInactivity()

        assertEquals(CommandKind.COMMAND_KIND_CYCLE_WALL_INACTIVITY, wsClient.lastSentCommand)
    }

    @Test
    fun wallInactivityStatusFrameUpdatesActiveSessionState() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
        }
        val viewModel = AppViewModel(repository, wsClient)

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Disconnected,
                shareToken = null,
            ),
        )

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        wsClient.fireConnect()
        runCurrent()
        wsClient.fireFrame(wallInactivityStatusFrame(seq = 2, sessionId = "host-1", enabled = true, label = "5m"))
        runCurrent()

        assertTrue("enabled=${viewModel.state.value.wallInactivityEnabled} label=${viewModel.state.value.wallInactivityLabel}", viewModel.state.value.wallInactivityEnabled)
        assertEquals("5m", viewModel.state.value.wallInactivityLabel)
        assertEquals("wall 5m", viewModel.state.value.transientStatus?.message)
    }

    @Test
    fun liveWallFrameAdvancesCursorAndSuppressesReplay() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
        }
        val recordingNotifier = RecordingWallNotifier()
        val wallStore = testWallWorkStateStore()
        val viewModel = AppViewModel(
            repository = repository,
            wsClient = wsClient,
            wallDeliveryCoordinator = MonotonicWallDeliveryCoordinator(wallStore, recordingNotifier),
        )

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Disconnected,
                shareToken = null,
            ),
        )

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        wsClient.fireConnect()
        runCurrent()

        val frame = Frame.newBuilder()
            .setSeq(2)
            .setWall(
                systems.pkt.lingon.protocol.Wall.newBuilder()
                    .setId(42)
                    .setSender("relay")
                    .setMessage("bash inactive")
                    .build(),
            )
            .build()
        wsClient.fireFrame(frame)
        advanceUntilIdle()
        wsClient.fireFrame(frame)
        advanceUntilIdle()

        assertEquals(listOf("https://localhost:12843/v1#42" to "relay\nbash inactive"), recordingNotifier.deliveries)
        assertEquals(42L, wallStore.loadCursor("https://localhost:12843/v1"))
    }

    @Test
    fun wallInactivityBannerAutoDismisses() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
        }
        val viewModel = AppViewModel(repository, wsClient)

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Disconnected,
                shareToken = null,
            ),
        )

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        wsClient.fireConnect()
        runCurrent()
        wsClient.fireFrame(wallInactivityStatusFrame(seq = 2, sessionId = "host-1", enabled = true, label = "5m"))
        runCurrent()

        assertEquals("wall 5m", viewModel.state.value.transientStatus?.message)

        advanceTimeBy(2999)
        runCurrent()
        assertEquals("wall 5m", viewModel.state.value.transientStatus?.message)

        advanceTimeBy(1)
        runCurrent()
        assertNull(viewModel.state.value.transientStatus)
    }

    @Test
    fun wallInactivityBannerReplacementRearmsDismissTimer() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
        }
        val viewModel = AppViewModel(repository, wsClient)

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Connected,
                shareToken = null,
            ),
        )
        viewModel.selectSession("host-1")
        advanceUntilIdle()
        wsClient.fireConnect()
        runCurrent()

        wsClient.fireFrame(wallInactivityStatusFrame(seq = 2, sessionId = "host-1", enabled = true, label = "2m"))
        runCurrent()
        assertEquals("wall 2m", viewModel.state.value.transientStatus?.message)

        advanceTimeBy(2000)
        wsClient.fireFrame(wallInactivityStatusFrame(seq = 3, sessionId = "host-1", enabled = true, label = "5m"))
        runCurrent()
        assertEquals("wall 5m", viewModel.state.value.transientStatus?.message)

        advanceTimeBy(2999)
        runCurrent()
        assertEquals("wall 5m", viewModel.state.value.transientStatus?.message)

        advanceTimeBy(1)
        runCurrent()
        assertNull(viewModel.state.value.transientStatus)
    }

    @Test
    fun logoutClearsWallInactivityCacheForFreshSession() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
        }
        val viewModel = AppViewModel(repository, wsClient)

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Disconnected,
                shareToken = null,
            ),
        )

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        wsClient.fireConnect()
        runCurrent()
        wsClient.fireFrame(wallInactivityStatusFrame(seq = 2, sessionId = "host-1", enabled = true, label = "5m"))
        runCurrent()
        assertTrue(viewModel.state.value.wallInactivityEnabled)
        assertEquals("5m", viewModel.state.value.wallInactivityLabel)

        viewModel.logout()
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Disconnected,
                shareToken = null,
                status = null,
                transientStatus = null,
            ),
        )

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        assertFalse(viewModel.state.value.wallInactivityEnabled)
        assertNull(viewModel.state.value.wallInactivityLabel)

        wsClient.fireConnect()
        runCurrent()
        wsClient.fireFrame(wallInactivityStatusFrame(seq = 3, sessionId = "host-1", enabled = false, label = ""))
        runCurrent()

        assertFalse(viewModel.state.value.wallInactivityEnabled)
        assertNull(viewModel.state.value.wallInactivityLabel)
        assertNull(viewModel.state.value.transientStatus)
    }

    @Test
    fun scrollbackFrameClearsSessionSyncing() = runTest {
        val repository = FakeRepository(
            sessions = listOf(
                RelaySession(id = "host-1", name = "Host 1", status = "active"),
            ),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
            listener.onFrame(socket, scrollbackFrame(2L))
        }
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Disconnected,
                shareToken = null,
            ),
        )

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        wsClient.fireConnect()
        advanceUntilIdle()

        assertEquals("scrollback", viewModel.state.value.lastFrameType)
        assertFalse(viewModel.state.value.sessionSyncing)
        assertEquals(2L, viewModel.state.value.lastFrameSeq)
    }

    @Test
    fun welcomeFrameClearsSessionSyncing() = runTest {
        val repository = FakeRepository(
            sessions = listOf(
                RelaySession(id = "host-1", name = "Host 1", status = "active"),
            ),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
        }
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Disconnected,
                shareToken = null,
            ),
        )

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        wsClient.fireConnect()
        advanceUntilIdle()

        assertEquals("welcome", viewModel.state.value.lastFrameType)
        assertFalse(viewModel.state.value.sessionSyncing)
        assertEquals(ConnectionState.Connected, viewModel.state.value.connectionState)
    }

    @Test
    fun repeatedSocketCloseKeepsSessionSyncingWhileReconnectIsPending() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
        }
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Disconnected,
                shareToken = null,
            ),
        )

        viewModel.selectSession("host-1")
        advanceUntilIdle()
        wsClient.fireConnect()
        advanceUntilIdle()

        wsClient.fakeSocket.close(1006, "network down")
        runCurrent()
        assertTrue(viewModel.state.value.sessionSyncing)
        assertTrue(viewModel.state.value.showsSyncingIndicator)
        assertEquals(ConnectionState.Disconnected, viewModel.state.value.connectionState)

        wsClient.fakeSocket.close(1006, "network still down")
        runCurrent()
        assertTrue(viewModel.state.value.sessionSyncing)
        assertTrue(viewModel.state.value.showsSyncingIndicator)
        assertEquals(ConnectionState.Disconnected, viewModel.state.value.connectionState)
    }

    @Test
    fun waitingForHostCountsAsSyncingIndicatorState() = runTest {
        val state = UiState(
            loggedIn = true,
            activeSessionId = "host-1",
            connectionState = ConnectionState.Waiting,
            sessionSyncing = false,
        )

        assertTrue(state.showsSyncingIndicator)
    }

    @Test
    fun onAppForegroundHealthyConnectionDoesNotReconnect() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
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
                connectionState = ConnectionState.Connected,
                lastFrameAtMs = 1_000L,
                status = null,
                lastFrameError = null,
            ),
        )
        setWebSocketForTest(viewModel, wsClient.fakeSocket)
        setSocketOpenForTest(viewModel, true)
        setActiveConnectionForTest(viewModel, "host-1", null)
        wsClient.connectCount = 0

        viewModel.onAppBackgroundAt(0L)
        viewModel.onAppForegroundAt(5_000L)
        advanceUntilIdle()

        assertEquals(0, wsClient.closeCount)
        assertEquals(0, wsClient.connectCount)
    }

    @Test
    fun onAppForegroundReconnectsWhenSocketReferenceExistsButIsNotOpen() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
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
                connectionState = ConnectionState.Connected,
                lastFrameAtMs = 1_000L,
                status = null,
                lastFrameError = null,
            ),
        )
        setWebSocketForTest(viewModel, wsClient.fakeSocket)
        setSocketOpenForTest(viewModel, false)
        setActiveConnectionForTest(viewModel, "host-1", null)
        wsClient.connectCount = 0

        viewModel.onAppBackgroundAt(0L)
        viewModel.onAppForegroundAt(31_000L)
        advanceUntilIdle()

        assertTrue(wsClient.closeCount >= 1)
        assertTrue(wsClient.connectCount >= 1)
        assertEquals("host-1", wsClient.lastConnectOptions?.sessionId)
    }

    @Test
    fun onAppForegroundRecoverableFailureReconnects() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
        }
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Disconnected,
                status = StatusMessage("Unable to resolve host \"pkt.systems\"", StatusLevel.Error),
                lastFrameError = "Unable to resolve host \"pkt.systems\"",
            ),
        )
        setWebSocketForTest(viewModel, wsClient.fakeSocket)
        setActiveConnectionForTest(viewModel, "host-1", null)
        wsClient.connectCount = 0

        viewModel.onAppBackgroundAt(0L)
        viewModel.onAppForegroundAt(31_000L)
        advanceUntilIdle()

        assertTrue(wsClient.closeCount >= 1)
        assertTrue(wsClient.connectCount >= 1)
    }

    @Test
    fun shouldRecoverConnectionOnForegroundOnlyWhenBrokenAndNotThrottled() {
        val broken = AppViewModel.shouldRecoverConnectionOnForeground(
            state = UiState(
                loggedIn = true,
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Disconnected,
                status = StatusMessage("Unable to resolve host", StatusLevel.Error),
                lastFrameError = "Unable to resolve host",
            ),
            hasSocket = false,
            nowMs = 60_000L,
            lastForegroundRecoveryAtMs = 0L,
        )
        assertTrue(broken)

        val healthy = AppViewModel.shouldRecoverConnectionOnForeground(
            state = UiState(
                loggedIn = true,
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Connected,
            ),
            hasSocket = true,
            nowMs = 60_000L,
            lastForegroundRecoveryAtMs = 0L,
        )
        assertFalse(healthy)

        val staleConnectedReference = AppViewModel.shouldRecoverConnectionOnForeground(
            state = UiState(
                loggedIn = true,
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Connected,
            ),
            hasSocket = false,
            nowMs = 60_000L,
            lastForegroundRecoveryAtMs = 0L,
        )
        assertTrue(staleConnectedReference)

        val throttled = AppViewModel.shouldRecoverConnectionOnForeground(
            state = UiState(
                loggedIn = true,
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                connectionState = ConnectionState.Disconnected,
                status = StatusMessage("Unable to resolve host", StatusLevel.Error),
                lastFrameError = "Unable to resolve host",
            ),
            hasSocket = false,
            nowMs = 20_000L,
            lastForegroundRecoveryAtMs = 10_000L,
        )
        assertFalse(throttled)
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
    private val sessionProvider: (() -> List<RelaySession>)? = null,
    private val failListSessions: Boolean = true,
    private val refreshAuthResult: Boolean = true,
    private val refreshAuthError: Throwable? = null,
    backgroundWallEnabled: Boolean = false,
    backgroundWallEnabledFlowOverride: Flow<Boolean>? = null,
    initialSessionZooms: Map<String, Float> = emptyMap(),
    initialLastActiveSessionByEndpoint: Map<String, String> = emptyMap(),
) : LingonClient {
    private val backgroundWallEnabledState = MutableStateFlow(backgroundWallEnabled)
    override val endpointFlow: Flow<String> = MutableStateFlow("https://localhost:12843/v1")
    override val fontSizeFlow: Flow<Int> = MutableStateFlow(14)
    override val resizeHostFlow: Flow<Boolean> = MutableStateFlow(false)
    override val backgroundWallEnabledFlow: Flow<Boolean> =
        backgroundWallEnabledFlowOverride ?: backgroundWallEnabledState
    override val appLockTimeoutMinutesFlow: Flow<Int> = MutableStateFlow(appLockMinutes)
    override val savedEndpointsFlow: Flow<List<String>> = MutableStateFlow(listOf("https://localhost:12843/v1"))
    override val certificatesFlow: Flow<Map<String, List<TrustedCert>>> = MutableStateFlow(emptyMap())
    private val lastActiveSessionByEndpoint = initialLastActiveSessionByEndpoint.toMutableMap()
    var refreshAuthCalls: Int = 0
    var saveZoomCalls: Int = 0
    var onSetBackgroundWallEnabled: ((Boolean) -> Unit)? = null
    val savedZooms = initialSessionZooms.toMutableMap()

    override fun setEndpoint(value: String) {
        // no-op
    }

    override fun setFontSize(value: Int) {
        // no-op
    }

    override fun saveSessionZoom(endpoint: String, sessionId: String, value: Float) {
        saveZoomCalls += 1
        savedZooms["${endpoint.trim()}|${sessionId.trim()}"] = value
    }

    override fun setResizeHostEnabled(value: Boolean) {
        // no-op
    }

    override fun setBackgroundWallEnabled(value: Boolean) {
        onSetBackgroundWallEnabled?.invoke(value) ?: run {
            backgroundWallEnabledState.value = value
        }
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

    override suspend fun loadSessionZoom(endpoint: String, sessionId: String): Float {
        return savedZooms["${endpoint.trim()}|${sessionId.trim()}"] ?: DefaultTerminalZoom
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
        return sessionProvider?.invoke() ?: sessions
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

private class FakeWsClient(
    var onConnect: (ConnectOptions, Listener, WebSocket) -> Unit = { _, _, _ -> },
) : RelayWebSocketClient(
    testHttpClientProvider(),
) {
    var connectCount: Int = 0
    var lastConnectOptions: ConnectOptions? = null
    var lastSentBytes: ByteArray? = null
    var lastSentCommand: CommandKind? = null
    var closeCount: Int = 0
    private var pendingConnect: ((WebSocket) -> Unit)? = null
    val fakeSocket: WebSocket = object : WebSocket {
        override fun request(): okhttp3.Request = okhttp3.Request.Builder().url("https://localhost/").build()
        override fun queueSize(): Long = 0
        override fun send(text: String): Boolean = true
        override fun send(bytes: okio.ByteString): Boolean = true
        override fun close(code: Int, reason: String?): Boolean {
            closeCount += 1
            lastListener?.onClosed(this, code, reason)
            return true
        }
        override fun cancel() {}
    }
    private var lastListener: RelayWebSocketClient.Listener? = null

    override fun connect(options: ConnectOptions, listener: Listener): WebSocket {
        connectCount += 1
        lastConnectOptions = options
        lastListener = listener
        pendingConnect = { socket -> onConnect(options, listener, socket) }
        return fakeSocket
    }

    fun fireConnect() {
        val callback = pendingConnect ?: return
        pendingConnect = null
        callback(fakeSocket)
    }

    fun fireFrame(frame: Frame) {
        lastListener?.onFrame(fakeSocket, frame)
    }

    override fun sendInput(webSocket: WebSocket, data: ByteArray) {
        lastSentBytes = data.copyOf()
    }

    override fun sendInput(webSocket: WebSocket, text: String) {
        lastSentBytes = text.toByteArray()
    }

    override fun sendResize(webSocket: WebSocket, cols: Int, rows: Int) {
        // no-op
    }

    override fun sendCommand(webSocket: WebSocket, kind: CommandKind) {
        lastSentCommand = kind
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

private fun setWebSocketForTest(viewModel: AppViewModel, webSocket: WebSocket) {
    val field = AppViewModel::class.java.getDeclaredField("ws")
    field.isAccessible = true
    field.set(viewModel, webSocket)
    setSocketOpenForTest(viewModel, true)
}

private fun setSocketOpenForTest(viewModel: AppViewModel, value: Boolean) {
    val field = AppViewModel::class.java.getDeclaredField("socketOpen")
    field.isAccessible = true
    field.setBoolean(viewModel, value)
}

private fun setActiveConnectionForTest(viewModel: AppViewModel, sessionId: String?, shareToken: String?) {
    val companionClass = Class.forName("systems.pkt.lingon.viewmodel.AppViewModel\$ConnectionKey")
    val ctor = companionClass.getDeclaredConstructor(String::class.java, String::class.java)
    ctor.isAccessible = true
    val key = ctor.newInstance(sessionId, shareToken)
    val field = AppViewModel::class.java.getDeclaredField("activeConnection")
    field.isAccessible = true
    field.set(viewModel, key)
}

private fun welcomeFrame(): Frame {
    return Frame.newBuilder()
        .setWelcome(
            Welcome.newBuilder()
                .setGrantedControl(false)
                .setServerCols(80)
                .setServerRows(24)
                .setHolderClientId("")
                .build(),
        )
        .build()
}

private fun snapshotFrame(seq: Long, title: String): Frame {
    return Frame.newBuilder()
        .setSeq(seq)
        .setSnapshot(
            Snapshot.newBuilder()
                .setCols(80)
                .setRows(24)
                .setCursorVisible(true)
                .setTitle(title)
                .build(),
        )
        .build()
}

private fun diffFrame(seq: Long, title: String): Frame {
    return Frame.newBuilder()
        .setSeq(seq)
        .setDiff(
            Diff.newBuilder()
                .setTitle(title)
                .build(),
        )
        .build()
}

private fun scrollbackFrame(seq: Long): Frame {
    return Frame.newBuilder()
        .setSeq(seq)
        .setScrollback(
            Scrollback.newBuilder()
                .setCols(80)
                .setClear(false)
                .addRows(
                    ScrollbackRow.newBuilder()
                        .addRunes('A'.code)
                        .addModes(0)
                        .addFg(0)
                        .addBg(0)
                        .build(),
                )
                .build(),
        )
        .build()
}

private fun wallInactivityStatusFrame(
    seq: Long,
    sessionId: String = "",
    enabled: Boolean,
    label: String,
    error: String = "",
): Frame {
    val builder = Frame.newBuilder()
        .setSeq(seq)
        .setWallInactivityStatus(
            WallInactivityStatus.newBuilder()
                .setEnabled(enabled)
                .setInactiveAfter(label)
                .setError(error)
                .build(),
        )
    if (sessionId.isNotBlank()) {
        builder.sessionId = sessionId
    }
    return builder.build()
}

private fun testHttpClientProvider(): HttpClientProvider {
    val dataStore = PreferenceDataStoreFactory.create(
        scope = CoroutineScope(Dispatchers.IO + SupervisorJob()),
        produceFile = { Files.createTempFile("lingon", ".preferences_pb").toFile() },
    )
    val certStore = CertificateStore(dataStore)
    return HttpClientProvider(certStore, CookieJar.NO_COOKIES)
}

private fun testWallWorkStateStore(): WallWorkStateStore {
    val dataStore = PreferenceDataStoreFactory.create(
        scope = CoroutineScope(Dispatchers.Main + SupervisorJob()),
        produceFile = { Files.createTempFile("lingon-wall", ".preferences_pb").toFile() },
    )
    return WallWorkStateStore(dataStore)
}

private class RecordingWallNotifier : WallNotifier {
    val deliveries = mutableListOf<Pair<String, String>>()

    override fun notifyWall(notification: WallNotification) {
        deliveries += "${notification.endpoint.trim()}#${notification.eventId}" to
            "${notification.sender.trim()}\n${notification.message.trim()}"
    }
}

private class RecordingBackgroundWallServiceController : BackgroundWallServiceController {
    val enabledValues = mutableListOf<Boolean>()

    override fun setEnabled(enabled: Boolean) {
        enabledValues += enabled
    }
}
