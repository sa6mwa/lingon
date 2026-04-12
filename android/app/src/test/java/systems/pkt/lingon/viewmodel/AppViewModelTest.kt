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
import systems.pkt.lingon.data.relay.RelaySession
import systems.pkt.lingon.data.relay.RelayWallEventsPage
import systems.pkt.lingon.data.certs.TrustedCert
import systems.pkt.lingon.data.relay.RelayWebSocketClient
import systems.pkt.lingon.data.certs.CertificateStore
import systems.pkt.lingon.protocol.Diff
import systems.pkt.lingon.protocol.Frame
import systems.pkt.lingon.protocol.Scrollback
import systems.pkt.lingon.protocol.Snapshot
import systems.pkt.lingon.protocol.Welcome
import systems.pkt.lingon.protocol.ScrollbackRow
import systems.pkt.lingon.terminal.TerminalSnapshot
import systems.pkt.lingon.ui.SNAPSHOT_MODE_APP_CURSOR

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
    fun selectSessionResetsViewportNonceWhenSwitchingTabs() = runTest {
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
        assertEquals(8, viewModel.state.value.panResetNonce)
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
        advanceUntilIdle()

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
        advanceUntilIdle()

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
        setActiveConnectionForTest(viewModel, "host-1", null)
        wsClient.connectCount = 0

        viewModel.onAppBackgroundAt(0L)
        viewModel.onAppForegroundAt(5_000L)
        advanceUntilIdle()

        assertEquals(0, wsClient.closeCount)
        assertEquals(0, wsClient.connectCount)
    }

    @Test
    fun onAppForegroundRecoverableFailureReconnectsOnceAndIsThrottled() = runTest {
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

        assertEquals(1, wsClient.closeCount)
        assertEquals(1, wsClient.connectCount)

        viewModel.onAppForegroundAt(40_000L)
        advanceUntilIdle()

        assertEquals(1, wsClient.closeCount)
        assertEquals(1, wsClient.connectCount)
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
    private val onConnect: (ConnectOptions, Listener, WebSocket) -> Unit = { _, _, _ -> },
) : RelayWebSocketClient(
    testHttpClientProvider(),
) {
    var connectCount: Int = 0
    var lastConnectOptions: ConnectOptions? = null
    var lastSentBytes: ByteArray? = null
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

    override fun sendInput(webSocket: WebSocket, data: ByteArray) {
        lastSentBytes = data.copyOf()
    }

    override fun sendInput(webSocket: WebSocket, text: String) {
        lastSentBytes = text.toByteArray()
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

private fun setWebSocketForTest(viewModel: AppViewModel, webSocket: WebSocket) {
    val field = AppViewModel::class.java.getDeclaredField("ws")
    field.isAccessible = true
    field.set(viewModel, webSocket)
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

private fun testHttpClientProvider(): HttpClientProvider {
    val dataStore = PreferenceDataStoreFactory.create(
        scope = CoroutineScope(Dispatchers.IO + SupervisorJob()),
        produceFile = { Files.createTempFile("lingon", ".preferences_pb").toFile() },
    )
    val certStore = CertificateStore(dataStore)
    return HttpClientProvider(certStore, CookieJar.NO_COOKIES)
}
