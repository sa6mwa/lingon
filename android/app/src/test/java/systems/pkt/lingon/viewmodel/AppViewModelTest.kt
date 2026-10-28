package systems.pkt.lingon.viewmodel

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.asCoroutineDispatcher
import kotlinx.coroutines.joinAll
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.flow.emitAll
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.resetMain
import okhttp3.CookieJar
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
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
import systems.pkt.lingon.data.relay.RelayShareSession
import systems.pkt.lingon.data.relay.AuthenticatedShareSession
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
import systems.pkt.lingon.protocol.Sessions
import systems.pkt.lingon.protocol.SessionInfo
import systems.pkt.lingon.protocol.SessionClosed
import systems.pkt.lingon.protocol.Welcome
import systems.pkt.lingon.protocol.WallInactivityStatus
import systems.pkt.lingon.protocol.ScrollbackRow
import systems.pkt.lingon.DefaultTerminalZoom
import systems.pkt.lingon.MaxTerminalZoom
import systems.pkt.lingon.terminal.TerminalSnapshot
import systems.pkt.lingon.ui.SNAPSHOT_MODE_APP_CURSOR
import systems.pkt.lingon.work.BackgroundWallServiceController
import systems.pkt.lingon.work.WallWorkScheduler

@OptIn(ExperimentalCoroutinesApi::class)
class AppViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun activeSessionClosureSelectsSurvivorAfterSuspendedPreferenceRead() = runTest {
        val sessions = listOf(
            RelaySession(id = "first", name = "First", status = "active"),
            RelaySession(id = "second", name = "Second", status = "active"),
        )
        val repository = FakeRepository(sessions = sessions, failListSessions = false)
        val wsClient = FakeWsClient { options, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
            listener.onFrame(socket, snapshotFrame(1, options.sessionId.orEmpty()))
        }
        val model = AppViewModel(repository, wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))
        runCurrent()
        setUiStateForTest(model, model.state.value.copy(loggedIn = true, sessions = sessions, activeSessionId = "first", endpoint = "https://localhost:12843/v1"))
        model.selectSession("first")
        runCurrent()
        wsClient.fireConnect()
        runCurrent()
        val entered = kotlinx.coroutines.CompletableDeferred<Unit>()
        val release = kotlinx.coroutines.CompletableDeferred<Unit>()
        repository.onLoadLastActiveSession = {
            entered.complete(Unit)
            release.await()
            "second"
        }

        wsClient.fireFrame(Frame.newBuilder().setSessionId("first")
            .setSessionClosed(SessionClosed.newBuilder().setReason("closed")).build())
        runCurrent()
        assertTrue("socket closure cancelled survivor preference loading", entered.isCompleted)
        assertNull(model.state.value.activeSessionId)
        release.complete(Unit)
        runCurrent()
        assertEquals("second", model.state.value.activeSessionId)
        assertEquals("second", wsClient.lastConnectOptions?.sessionId)
    }

    @Test
    fun incomingTerminalBurstDoesNotCreateOneMainJobPerFrame() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
            listener.onFrame(socket, snapshotFrame(1, "initial"))
        }
        val model = AppViewModel(repository, wsClient)
        runCurrent()
        setUiStateForTest(model, model.state.value.copy(loggedIn = true, activeSessionId = "host-1", endpoint = "https://localhost:12843/v1"))
        model.selectSession("host-1")
        runCurrent()
        wsClient.fireConnect()
        runCurrent()
        val parent = requireNotNull(model.viewModelScope.coroutineContext[kotlinx.coroutines.Job])
        val before = parent.children.count { it.isActive }

        // Main is deliberately paused while the callback accepts this burst.
        // The bound is on retained scheduled work, not on elapsed wall time.
        for (i in 1..8) wsClient.fireFrame(diffFrame(i + 1L, "burst-$i"))
        val retained = parent.children.count { it.isActive } - before
        assertTrue("terminal burst retained $retained separate main jobs", retained <= 2)
        runCurrent()
        assertEquals(9L, model.state.value.lastFrameSeq)
        assertEquals("burst-8", model.state.value.activeSnapshot?.title)
    }

    @Test
    fun sessionManagementRequiresConnectedOwnerButDoesNotRequireControl() = runTest {
        val repository = FakeRepository()
        val viewModel = AppViewModel(repository, FakeWsClient(), ioDispatcher = StandardTestDispatcher(testScheduler))
        runCurrent()
        val owner = viewModel.state.value.copy(loggedIn = true, activeSessionId = "host", connectionState = ConnectionState.Connected, hasControl = false)
        for (state in listOf(owner.copy(shareToken = "shared", hasControl = true), owner.copy(requiresAppUnlock = true), owner.copy(connectionState = ConnectionState.Disconnected))) {
            setUiStateForTest(viewModel, state)
            viewModel.createTerminal()
            runCurrent()
        }
        assertEquals(0, repository.managementRequests.size)
        setUiStateForTest(viewModel, owner)
        val release = kotlinx.coroutines.CompletableDeferred<Unit>()
        repository.onManage = { release.await() }
        viewModel.renameSession("build 🦊")
        runCurrent()
        assertTrue(viewModel.state.value.sessionManagementBusy)
        viewModel.createTerminal()
        runCurrent()
        assertEquals(1, repository.managementRequests.size)
        assertEquals("rename", repository.managementRequests.single().kind)
        assertEquals("build 🦊", repository.managementRequests.single().name)
        assertEquals("host", repository.managementRequests.single().sessionId)
        release.complete(Unit)
        runCurrent()
        assertFalse(viewModel.state.value.sessionManagementBusy)
    }

    @Test
    fun obsoleteTransientShareAuthenticationDoesNotDropNewSocket() = runTest {
        assertObsoleteShareAuthenticationIgnored(java.io.IOException("obsolete network failure"))
    }

    @Test
    fun obsoletePermanentShareAuthenticationDoesNotClearNewPermission() = runTest {
        assertObsoleteShareAuthenticationIgnored(ApiException("obsolete denial", 403))
    }

    @Test
    fun obsoleteSuccessfulShareAuthenticationDoesNotReplaceNewMetadata() = runTest {
        assertObsoleteShareAuthenticationIgnored(null)
    }

    private suspend fun kotlinx.coroutines.test.TestScope.assertObsoleteShareAuthenticationIgnored(oldFailure: Exception?) {
        val dispatcher = java.util.concurrent.Executors.newFixedThreadPool(2).asCoroutineDispatcher()
        val entered = kotlinx.coroutines.CompletableDeferred<Unit>()
        val release = kotlinx.coroutines.CompletableDeferred<Unit>()
        val attempts = java.util.concurrent.atomic.AtomicInteger()
        val wsClient = FakeWsClient { _, listener, socket -> listener.onOpen(socket) }
        wsClient.onAuthenticateShareSession = { _, _ ->
            if (attempts.incrementAndGet() == 1) {
                entered.complete(Unit)
                kotlinx.coroutines.runBlocking { release.await() }
                if (oldFailure != null) throw oldFailure
                RelayShareSession(sessionId = "shared-real", name = "Obsolete metadata", scope = "view")
            } else {
                RelayShareSession(sessionId = "shared-real", name = "Current metadata", scope = "control")
            }
        }
        val viewModel = AppViewModel(FakeRepository(), wsClient, ioDispatcher = dispatcher)
        try {
            runCurrent()
            val originalJobs = viewModel.viewModelScope.coroutineContext[kotlinx.coroutines.Job]!!.children.toSet()
            viewModel.handleSharedToken("token", "https://example")
            runCurrent()
            entered.await()
            val beforeRecoveryJobs = viewModel.viewModelScope.coroutineContext[kotlinx.coroutines.Job]!!.children.toSet()
            val oldJobs = beforeRecoveryJobs.filter { it !in originalJobs }
            viewModel.onAppForegroundAt(5_000L)
            runCurrent()
            viewModel.viewModelScope.coroutineContext[kotlinx.coroutines.Job]!!.children.filter { it !in beforeRecoveryJobs }.toList().joinAll()
            wsClient.fireConnect()
            wsClient.fireFrame(welcomeFrame())
            runCurrent()
            assertEquals(ConnectionState.Connected, viewModel.state.value.connectionState)
            val epoch = viewModel.state.value.terminalConnectionEpoch
            release.complete(Unit)
            oldJobs.joinAll()
            runCurrent()
            assertEquals(ConnectionState.Connected, viewModel.state.value.connectionState)
            assertEquals(epoch, viewModel.state.value.terminalConnectionEpoch)
            assertNull(viewModel.state.value.shareTokenError)
            assertTrue(viewModel.state.value.shareCanControl)
            assertEquals("Current metadata", viewModel.state.value.sessions.single().name)
            wsClient.fireFrame(Frame.newBuilder().setSeq(9).setSnapshot(Snapshot.newBuilder().setCols(1).setRows(1).addRunes('X'.code)).build())
            runCurrent()
            assertEquals('X'.code, viewModel.state.value.activeSnapshot?.runes?.first())
            advanceTimeBy(10_000)
            runCurrent()
            assertEquals(2, attempts.get())
            assertEquals(1, wsClient.connectCount)
            assertEquals(0, wsClient.closeCount)
        } finally {
            release.complete(Unit)
            invokeOnClearedForTest(viewModel)
            dispatcher.close()
        }
    }

    @Test
    fun shareAuthenticationRetriesNetworkAndServerFailuresWithBackoff() = runTest {
        val wsClient = FakeWsClient()
        var attempts = 0
        wsClient.onAuthenticateShareSession = { _, _ ->
            attempts += 1
            when (attempts) {
                1 -> throw java.io.IOException("offline")
                2 -> throw ApiException("relay restarting", statusCode = 503)
                else -> wsClient.shareSession
            }
        }
        val viewModel = AppViewModel(FakeRepository(), wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))
        viewModel.handleSharedToken("token", "https://example")
        runCurrent()
        assertEquals(1, attempts)
        assertTrue(viewModel.state.value.sessionSyncing)
        assertNull(viewModel.state.value.shareTokenError)
        advanceTimeBy(999)
        runCurrent()
        assertEquals(1, attempts)
        advanceTimeBy(1)
        runCurrent()
        assertEquals(2, attempts)
        advanceTimeBy(1999)
        runCurrent()
        assertEquals(2, attempts)
        advanceTimeBy(1)
        runCurrent()
        assertEquals(3, attempts)
        assertEquals(1, wsClient.connectCount)
        assertEquals("shared-real", wsClient.lastConnectOptions?.sessionId)
        assertNull(viewModel.state.value.shareTokenError)
    }

    @Test
    fun newShareRetriesTransientAuthenticationAfterLastSharedSessionCloses() = runTest {
        for (failure in listOf(
            java.io.IOException("offline"),
            ApiException("rate limited", statusCode = 429),
            ApiException("relay restarting", statusCode = 503),
        )) {
            val wsClient = FakeWsClient()
            wsClient.shareSession = RelayShareSession(sessionId = "old-share", name = "Old", scope = "view")
            val viewModel = AppViewModel(FakeRepository(), wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))
            viewModel.handleSharedToken("old-token", "https://example")
            runCurrent()
            wsClient.fireFrame(Frame.newBuilder().setSessionId("old-share")
                .setSessionClosed(SessionClosed.newBuilder().setReason("closed")).build())
            runCurrent()
            assertNull(viewModel.state.value.activeSessionId)
            assertTrue(viewModel.state.value.sessions.isEmpty())

            var attempts = 0
            wsClient.onAuthenticateShareSession = { _, _ ->
                attempts += 1
                if (attempts == 1) throw failure
                RelayShareSession(sessionId = "new-share", name = "New", scope = "view")
            }
            viewModel.handleSharedToken("new-token", "https://example")
            runCurrent()
            assertEquals(1, attempts)
            advanceTimeBy(999)
            runCurrent()
            assertEquals(1, attempts)
            advanceTimeBy(1)
            runCurrent()
            assertEquals("retry suppressed after ${failure.message}", 2, attempts)
            assertEquals("new-share", viewModel.state.value.activeSessionId)
            assertEquals(2, wsClient.connectCount)
            assertNull(viewModel.state.value.shareTokenError)
            viewModel.logout()
            runCurrent()
        }
    }

    @Test
    fun reconnectSessionFetchIsIgnoredAfterEndpointChange() = runTest {
        assertSupersededReconnectFetch(logout = false)
    }

    @Test
    fun reconnectSessionFetchIsIgnoredAfterLogout() = runTest {
        assertSupersededReconnectFetch(logout = true)
    }

    @Test
    fun staleReconnectAuthenticationFailureDoesNotClearNewAccount() = runTest {
        assertSupersededReconnectFetch(logout = false, failure = ApiException("expired old owner", 401))
    }

    @Test
    fun staleReconnectNetworkFailureDoesNotSetNewContextError() = runTest {
        assertSupersededReconnectFetch(logout = false, failure = java.io.IOException("obsolete network failure"))
    }

    private suspend fun kotlinx.coroutines.test.TestScope.assertSupersededReconnectFetch(logout: Boolean, failure: Exception? = null) {
        val oldSessions = listOf(RelaySession(id = "old-host", name = "Old account", status = "active"))
        val repository = FakeRepository(sessions = oldSessions, failListSessions = false, refreshAuthResult = false)
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
            listener.onFrame(socket, snapshotFrame(7, "old snapshot"))
        }
        val viewModel = AppViewModel(repository, wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))
        runCurrent()
        setUiStateForTest(viewModel, viewModel.state.value.copy(
            loggedIn = true, username = "old-owner", activeSessionId = "old-host", sessions = oldSessions,
            endpoint = "https://localhost:12843/v1", connectionState = ConnectionState.Disconnected,
        ))
        viewModel.selectSession("old-host")
        runCurrent()
        wsClient.fireConnect()
        runCurrent()
        assertEquals(ConnectionState.Connected, viewModel.state.value.connectionState)
        val fetched = kotlinx.coroutines.CompletableDeferred<Unit>()
        val release = kotlinx.coroutines.CompletableDeferred<Unit>()
        var fetches = 0
        repository.onListSessions = {
            fetches += 1
            if (fetches > 1) throw ApiException("new endpoint unauthenticated", 401)
            fetched.complete(Unit)
            release.await()
            if (failure != null) throw failure
            oldSessions
        }
        wsClient.fakeSocket.close(1000, "trigger reconnect")
        runCurrent()
        advanceTimeBy(2000)
        runCurrent()
        assertTrue("reconnect must be awaiting its old session response", fetched.isCompleted)
        if (logout) viewModel.logout() else viewModel.updateEndpoint("https://new.example/v1")
        runCurrent()
        if (failure != null) {
            setUiStateForTest(viewModel, viewModel.state.value.copy(
                loggedIn = true, username = "new-owner", activeSessionId = "new-host",
                sessions = listOf(RelaySession(id = "new-host", name = "New account", status = "active")),
                status = StatusMessage("new context"), connectionState = ConnectionState.Connected,
            ))
        }
        val expected = viewModel.state.value
        val connects = wsClient.connectCount
        val refreshes = repository.refreshAuthCalls
        release.complete(Unit)
        runCurrent()
        assertEquals(expected.sessions, viewModel.state.value.sessions)
        assertEquals(expected.activeSessionId, viewModel.state.value.activeSessionId)
        assertEquals(expected.loggedIn, viewModel.state.value.loggedIn)
        assertEquals(expected.username, viewModel.state.value.username)
        assertEquals(expected.status, viewModel.state.value.status)
        assertEquals(refreshes, repository.refreshAuthCalls)
        assertEquals(connects, wsClient.connectCount)
        advanceTimeBy(10_000)
        runCurrent()
        assertEquals(connects, wsClient.connectCount)
        assertEquals(expected.sessions, viewModel.state.value.sessions)
    }

    @Test
    fun managementSessionFetchIsIgnoredAfterEndpointChange() = runTest {
        assertSupersededManagementFetch(logout = false)
    }

    @Test
    fun managementSessionFetchIsIgnoredAfterLogout() = runTest {
        assertSupersededManagementFetch(logout = true)
    }

    @Test
    fun bootstrapRestoresSessionsAfterDelayedInitialEndpoint() = runTest {
        val sessions = listOf(RelaySession(id = "restored", name = "Restored terminal", status = "active"))
        val repository = FakeRepository(
            sessions = sessions,
            failListSessions = false,
            initialLastActiveSessionByEndpoint = mapOf("https://localhost:12843/v1" to "restored"),
        )
        val endpointReady = kotlinx.coroutines.CompletableDeferred<Unit>()
        lateinit var model: AppViewModel
        val delayedRepository = object : LingonClient by repository {
            override val endpointFlow = flow {
                endpointReady.await()
                emitAll(repository.endpointFlow)
            }
            override suspend fun listSessions(): List<RelaySession> {
                endpointReady.await()
                model.state.first { it.endpoint.isNotBlank() }
                return repository.listSessions()
            }
        }
        val wsClient = FakeWsClient()
        model = AppViewModel(delayedRepository, wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))
        runCurrent()
        assertEquals("", model.state.value.endpoint)
        assertFalse(model.state.value.loggedIn)

        endpointReady.complete(Unit)
        runCurrent()

        assertTrue("persisted authentication was discarded after endpoint initialization", model.state.value.loggedIn)
        assertEquals(sessions, model.state.value.sessions)
        assertEquals("restored", model.state.value.activeSessionId)
        assertEquals("restored", wsClient.lastConnectOptions?.sessionId)
    }

    @Test
    fun bootstrapDoesNotRestoreAuthenticationWhenLoggedOutDuringEndpointInitialization() = runTest {
        val repository = FakeRepository(failListSessions = false)
        val endpointReady = kotlinx.coroutines.CompletableDeferred<Unit>()
        val delayedRepository = object : LingonClient by repository {
            override val endpointFlow = flow {
                endpointReady.await()
                emitAll(repository.endpointFlow)
            }
        }
        val model = AppViewModel(delayedRepository, FakeWsClient(), ioDispatcher = StandardTestDispatcher(testScheduler))
        runCurrent()
        model.logout()
        runCurrent()
        assertEquals(1, repository.clearAuthCalls)

        endpointReady.complete(Unit)
        runCurrent()

        assertFalse(model.state.value.loggedIn)
        assertTrue(model.state.value.sessions.isEmpty())
        assertNull(model.state.value.activeSessionId)
    }

    @Test
    fun bootstrapSessionFetchIsIgnoredAfterEndpointChange() = runTest {
        assertSupersededBootstrapFetch(logout = false)
    }

    @Test
    fun bootstrapSessionFetchIsIgnoredAfterLogout() = runTest {
        assertSupersededBootstrapFetch(logout = true)
    }

    @Test
    fun staleBootstrapAuthenticationFailureDoesNotClearNewAccount() = runTest {
        val repository = FakeRepository(refreshAuthResult = false)
        val release = kotlinx.coroutines.CompletableDeferred<Unit>()
        var fetches = 0
        repository.onListSessions = {
            fetches += 1
            if (fetches == 1) release.await()
            throw ApiException("expired old account", 401)
        }
        val viewModel = AppViewModel(repository, FakeWsClient(), ioDispatcher = StandardTestDispatcher(testScheduler))
        runCurrent()
        viewModel.updateEndpoint("https://new.example/v1")
        runCurrent()
        val newSessions = listOf(RelaySession(id = "new-host", name = "New account", status = "active"))
        setUiStateForTest(viewModel, viewModel.state.value.copy(
            loggedIn = true, username = "new-owner", activeSessionId = "new-host", sessions = newSessions,
            status = StatusMessage("new context"), connectionState = ConnectionState.Connected,
        ))
        release.complete(Unit)
        runCurrent()
        assertTrue(viewModel.state.value.loggedIn)
        assertEquals("new-owner", viewModel.state.value.username)
        assertEquals(newSessions, viewModel.state.value.sessions)
        assertEquals("new-host", viewModel.state.value.activeSessionId)
        assertEquals("new context", viewModel.state.value.status?.message)
    }

    @Test
    fun pendingSessionPreferenceCannotRestoreOldActiveSessionAfterEndpointChange() = runTest {
        val repository = FakeRepository()
        val waiting = kotlinx.coroutines.CompletableDeferred<Unit>()
        val release = kotlinx.coroutines.CompletableDeferred<Unit>()
        var fetches = 0
        repository.onListSessions = {
            fetches += 1
            if (fetches > 1) throw ApiException("new context is unauthenticated", 401)
            listOf(RelaySession(id = "old-host", name = "Old account", status = "active"))
        }
        repository.onLoadLastActiveSession = {
            waiting.complete(Unit)
            release.await()
            "old-host"
        }
        val viewModel = AppViewModel(repository, FakeWsClient(), ioDispatcher = StandardTestDispatcher(testScheduler))
        runCurrent()
        assertTrue(waiting.isCompleted)
        viewModel.updateEndpoint("https://new.example/v1")
        runCurrent()
        release.complete(Unit)
        runCurrent()
        assertTrue(viewModel.state.value.sessions.isEmpty())
        assertNull(viewModel.state.value.activeSessionId)
        assertFalse(viewModel.state.value.loggedIn)
    }

    private fun kotlinx.coroutines.test.TestScope.assertSupersededBootstrapFetch(logout: Boolean) {
        val repository = FakeRepository()
        val release = kotlinx.coroutines.CompletableDeferred<Unit>()
        var fetches = 0
        repository.onListSessions = {
            fetches += 1
            if (fetches > 1) throw ApiException("new context is unauthenticated", 401)
            release.await()
            listOf(RelaySession(id = "old-account-host", name = "Old account", status = "active"))
        }
        val viewModel = AppViewModel(repository, FakeWsClient(), ioDispatcher = StandardTestDispatcher(testScheduler))
        runCurrent()
        assertEquals(1, fetches)
        if (logout) viewModel.logout() else viewModel.updateEndpoint("https://new.example/v1")
        runCurrent()
        release.complete(Unit)
        runCurrent()
        assertTrue(viewModel.state.value.sessions.isEmpty())
        assertNull(viewModel.state.value.activeSessionId)
        assertFalse(viewModel.state.value.loggedIn)
    }

    private fun kotlinx.coroutines.test.TestScope.assertSupersededManagementFetch(logout: Boolean) {
        for (kind in listOf("create", "rename")) {
            val repository = FakeRepository()
            val viewModel = AppViewModel(repository, FakeWsClient(), ioDispatcher = StandardTestDispatcher(testScheduler))
            runCurrent()
            val fetched = kotlinx.coroutines.CompletableDeferred<Unit>()
            val release = kotlinx.coroutines.CompletableDeferred<Unit>()
            var fetches = 0
            repository.onListSessions = {
                fetches += 1
                if (fetches > 1) throw ApiException("new context is unauthenticated", 401)
                fetched.complete(Unit)
                release.await()
                listOf(RelaySession(id = "old-host", name = "Old renamed host", status = "active"))
            }
            setUiStateForTest(viewModel, viewModel.state.value.copy(
                loggedIn = true, activeSessionId = "old-host", connectionState = ConnectionState.Connected,
            ))
            if (kind == "create") viewModel.createTerminal() else viewModel.renameSession("Old renamed host")
            runCurrent()
            assertTrue(fetched.isCompleted)
            if (logout) viewModel.logout() else viewModel.updateEndpoint("https://new.example/v1")
            runCurrent()
            release.complete(Unit)
            runCurrent()
            assertTrue(viewModel.state.value.sessions.isEmpty())
            assertNull(viewModel.state.value.activeSessionId)
            assertFalse(viewModel.state.value.loggedIn)
            assertFalse(viewModel.state.value.sessionManagementBusy)
            assertFalse(viewModel.state.value.status?.message in listOf("Session renamed", "Terminal created"))
        }
    }

    @Test
    fun shareAuthenticationHonorsRetryAfterAndDoesNotRetryInvalidTokens() = runTest {
        val wsClient = FakeWsClient()
        var attempts = 0
        wsClient.onAuthenticateShareSession = { _, _ ->
            attempts += 1
            if (attempts == 1) throw ApiException("limited", statusCode = 429, retryAfterSeconds = 5)
            throw ApiException("invalid share token", statusCode = 401)
        }
        val viewModel = AppViewModel(FakeRepository(), wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))
        viewModel.handleSharedToken("token", "https://example")
        runCurrent()
        advanceTimeBy(4999)
        runCurrent()
        assertEquals(1, attempts)
        advanceTimeBy(1)
        runCurrent()
        assertEquals(2, attempts)
        assertEquals("invalid share token", viewModel.state.value.shareTokenError)
        assertFalse(viewModel.state.value.sessionSyncing)
        advanceTimeBy(30000)
        runCurrent()
        assertEquals(2, attempts)
        assertEquals(0, wsClient.connectCount)
    }

    @Test
    fun changingShareTokenCancelsRetryOfTheOldToken() = runTest {
        val wsClient = FakeWsClient()
        val tokens = mutableListOf<String>()
        wsClient.onAuthenticateShareSession = { _, token ->
            tokens += token
            if (token == "old") throw java.io.IOException("offline")
            wsClient.shareSession
        }
        val viewModel = AppViewModel(FakeRepository(), wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))
        viewModel.handleSharedToken("old", "https://example")
        runCurrent()
        viewModel.handleSharedToken("new", "https://example")
        runCurrent()
        advanceTimeBy(30000)
        runCurrent()
        assertEquals(listOf("old", "new"), tokens)
        assertEquals("new", wsClient.lastConnectOptions?.shareToken)
        assertEquals(1, wsClient.connectCount)
    }

    @Test
    fun prolongedShareOutageKeepsBackoffBoundedWithoutOverflow() = runTest {
        val wsClient = FakeWsClient()
        var attempts = 0
        wsClient.onAuthenticateShareSession = { _, _ ->
            attempts += 1
            if (attempts < 40) throw java.io.IOException("offline")
            wsClient.shareSession
        }
        val viewModel = AppViewModel(FakeRepository(), wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))
        viewModel.handleSharedToken("token", "https://example")
        runCurrent()
        for (attempt in 1 until 40) {
            val delayMs = minOf(15000, 1000 * (1 shl (minOf(attempt, 5) - 1)))
            advanceTimeBy(delayMs.toLong() - 1)
            runCurrent()
            assertEquals(attempt, attempts)
            advanceTimeBy(1)
            runCurrent()
            assertEquals(attempt + 1, attempts)
        }
        assertEquals(1, wsClient.connectCount)
    }

    @Test
    fun sharedAuthenticationRestoresAndReopensPersistedSessionZoom() = runTest {
        val key = "https://example|shared-real"
        val repository = FakeRepository(initialSessionZooms = mapOf(key to 1.7f))
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))
        viewModel.handleSharedToken("token", "https://example")
        runCurrent()
        assertEquals("shared-real", viewModel.state.value.activeSessionId)
        assertEquals(1.7f, viewModel.state.value.zoomFactor, 0.0001f)

        viewModel.updateZoomFactor(1.9f)
        advanceTimeBy(150)
        runCurrent()
        assertEquals(1.9f, repository.savedZooms[key]!!, 0.0001f)
        viewModel.handleSharedToken("token", "https://example")
        runCurrent()
        assertEquals("shared-real", viewModel.state.value.activeSessionId)
        assertEquals(1.9f, viewModel.state.value.zoomFactor, 0.0001f)
    }

    @Test
    fun handleSharedTokenUpdatesState() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))

        viewModel.handleSharedToken("token", "https://example")

        val state = viewModel.state.value
        assertFalse(state.loggedIn)
        assertEquals("token", state.shareToken)
        assertEquals("shared", state.activeSessionId)
        assertEquals(1, state.sessions.size)
    }

    @Test
    fun certificateFlowUsesNormalizedSelectedEndpointKey() = runTest {
        val certificate = TrustedCert(
            id = "cert-1",
            subject = "CN=relay.example",
            issuer = "CN=test-ca",
            fingerprint = "fingerprint",
            addedAt = "2026-07-13T00:00:00Z",
        )
        val repository = FakeRepository(
            savedEndpoints = listOf("https://relay.example"),
            initialCertificates = mapOf("https://relay.example/" to listOf(certificate)),
        )
        val viewModel = AppViewModel(repository, FakeWsClient())

        advanceUntilIdle()

        assertEquals("https://relay.example", viewModel.state.value.selectedCertEndpoint)
        assertEquals(listOf(certificate), viewModel.state.value.trustedCerts)
    }

    @Test
    fun selectSessionOnActiveTabKeepsVisibleSnapshot() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))

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
    fun updateTerminalSize_neverSendsResizeFrames() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                activeSessionId = "host-1",
                hasControl = true,
                resizeHostEnabled = true,
                connectionState = ConnectionState.Connected,
            ),
        )
        setWebSocketForTest(viewModel, wsClient.fakeSocket)

        viewModel.setResizeHostEnabledForTesting(true)
        viewModel.updateTerminalSize(132, 41)
        advanceUntilIdle()

        assertEquals(0, wsClient.resizeCount)
        assertFalse(viewModel.state.value.resizeHostEnabled)
    }

    @Test
    fun sendHeadlessResizeNow_sendsSingleResizeForActiveHeadlessSession() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(
                    RelaySession(id = "headless-1", name = "Headless 1", status = "active", headless = true),
                ),
                activeSessionId = "headless-1",
                terminalCols = 132,
                terminalRows = 41,
                connectionState = ConnectionState.Connected,
            ),
        )
        setWebSocketForTest(viewModel, wsClient.fakeSocket)

        viewModel.sendHeadlessResizeNow()

        assertEquals(1, wsClient.resizeCount)
    }

    @Test
    fun connectActiveSession_doesNotAdvertiseViewportResizeInHello() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "headless-1", name = "Headless 1", status = "active", headless = true)),
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
                sessions = listOf(RelaySession(id = "headless-1", name = "Headless 1", status = "active", headless = true)),
                activeSessionId = "headless-1",
                terminalCols = 132,
                terminalRows = 41,
                connectionState = ConnectionState.Disconnected,
            ),
        )

        viewModel.selectSession("headless-1")
        advanceUntilIdle()

        assertEquals(0, wsClient.lastConnectOptions?.cols)
        assertEquals(0, wsClient.lastConnectOptions?.rows)
    }

    @Test
    fun sharedSessionClosedUsesAuthenticatedSessionId() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
        }
        wsClient.shareSession = RelayShareSession(sessionId = "real-session", name = "Shared real", scope = "view")
        val viewModel = AppViewModel(repository, wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))

        viewModel.handleSharedToken("token", "https://example")
        advanceUntilIdle()

        assertEquals("real-session", viewModel.state.value.activeSessionId)
        assertEquals("real-session", wsClient.lastConnectOptions?.sessionId)

        wsClient.fireFrame(
            Frame.newBuilder()
                .setSessionId("real-session")
                .setSessionClosed(SessionClosed.newBuilder().setReason("closed").build())
                .build(),
        )
        advanceUntilIdle()

        assertNull(viewModel.state.value.activeSessionId)
        assertTrue(viewModel.state.value.sessions.isEmpty())
    }

    @Test
    fun shareForegroundRecoveryReusesAuthenticatedSessionConnection() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
        }
        wsClient.shareSession = RelayShareSession(sessionId = "real-session", name = "Shared real", scope = "view")
        val viewModel = AppViewModel(repository, wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))

        viewModel.handleSharedToken("token", "https://example")
        advanceUntilIdle()
        wsClient.fireConnect()
        advanceUntilIdle()

        assertEquals("real-session", viewModel.state.value.activeSessionId)
        assertEquals("real-session", wsClient.lastConnectOptions?.sessionId)
        assertEquals(1, wsClient.connectCount)

        viewModel.onAppBackgroundAt(0L)
        viewModel.onAppForegroundAt(5_000L)
        advanceUntilIdle()

        assertEquals(1, wsClient.connectCount)
        assertEquals(0, wsClient.closeCount)
        assertEquals("real-session", wsClient.lastConnectOptions?.sessionId)
    }

    @Test
    fun staleShareAuthenticationFailureDoesNotReplaceCurrentTokenState() = runTest {
        val repository = FakeRepository()
        lateinit var viewModel: AppViewModel
        val wsClient = FakeWsClient()
        wsClient.onAuthenticateShareSession = { _, shareToken ->
            if (shareToken == "old-token") {
                viewModel.handleSharedToken("new-token", "https://example")
                throw IllegalStateException("old token failed")
            }
            RelayShareSession(sessionId = "new-session", name = "New shared", scope = "view")
        }
        viewModel = AppViewModel(repository, wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))

        viewModel.handleSharedToken("old-token", "https://example")
        advanceUntilIdle()

        val state = viewModel.state.value
        assertEquals("new-token", state.shareToken)
        assertEquals("new-session", state.activeSessionId)
        assertNull(state.shareTokenError)
        assertNotEquals(ConnectionState.Disconnected, state.connectionState)
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
    fun sessionsAreSortedByNameAcrossBootstrapRefreshAndWebsocketUpdates() = runTest {
        var currentSessions = listOf(
            RelaySession(id = "session-c", name = "Alpha", status = "active"),
            RelaySession(id = "session-a", name = "Charlie", status = "active"),
            RelaySession(id = "session-b", name = "Bravo", status = "active"),
        )
        val repository = FakeRepository(
            failListSessions = false,
            sessionProvider = { currentSessions },
        )
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = emptyList(),
                activeSessionId = null,
                shareToken = null,
            ),
        )

        viewModel.manualRefresh()
        advanceUntilIdle()
        assertEquals(listOf("session-c", "session-b", "session-a"), viewModel.state.value.sessions.map { it.id })
        assertEquals("session-c", viewModel.state.value.activeSessionId)

        currentSessions = listOf(
            RelaySession(id = "session-d", name = "Delta", status = "active"),
            RelaySession(id = "session-b", name = "Bravo", status = "active"),
        )
        viewModel.manualRefresh()
        advanceUntilIdle()
        assertEquals(listOf("session-c", "session-b", "session-d"), viewModel.state.value.sessions.map { it.id })

        wsClient.fireFrame(
            Frame.newBuilder()
                .setSeq(77L)
                .setSessions(
                    Sessions.newBuilder()
                        .addSessions(SessionInfo.newBuilder().setId("session-z").setName("Zulu").setStatus("active"))
                        .addSessions(SessionInfo.newBuilder().setId("session-a").setName("Alpha").setStatus("active"))
                        .addSessions(SessionInfo.newBuilder().setId("session-m").setName("Mike").setStatus("active")),
                )
                .build(),
        )
        advanceUntilIdle()
        assertEquals(listOf("session-a", "session-c", "session-m", "session-z"), viewModel.state.value.sessions.map { it.id })
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
    fun connectActiveSessionSkipsStaleOpenAfterRefreshRace() = runTest {
        val repository = FakeRepository()
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(repository, wsClient)
        advanceUntilIdle()
        repository.onRefreshAuth = {
            setUiStateForTest(
                viewModel,
                viewModel.state.value.copy(
                    endpoint = "https://other.example/v1",
                    sessions = listOf(RelaySession(id = "host-2", name = "Host 2", status = "active")),
                    activeSessionId = "host-2",
                ),
            )
        }

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
        assertEquals(0, wsClient.connectCount)
        assertEquals("host-2", viewModel.state.value.activeSessionId)
    }

    @Test
    fun websocketUnauthorizedRefreshSuccessPreservesSessionsAndRecovers() = runTest {
        val repository = FakeRepository(
            sessions = listOf(
                RelaySession(id = "host-1", name = "Host 1", status = "active"),
                RelaySession(id = "host-2", name = "Host 2", status = "active"),
            ),
            failListSessions = false,
            refreshAuthResult = true,
            backgroundWallEnabled = true,
        )
        val wsClient = FakeWsClient()
        val wallScheduler = RecordingWallWorkScheduler()
        val viewModel = AppViewModel(
            repository,
            wsClient,
            wallWorkScheduler = wallScheduler,
        )
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(
                    RelaySession(id = "host-1", name = "Host 1", status = "active"),
                    RelaySession(id = "host-2", name = "Host 2", status = "active"),
                ),
                activeSessionId = "host-1",
                shareToken = null,
                connectionState = ConnectionState.Connected,
                hasControl = true,
            ),
        )
        viewModel.selectSession("host-1")
        advanceUntilIdle()
        val initialConnectCount = wsClient.connectCount
        val initialRefreshCalls = repository.refreshAuthCalls

        wsClient.fireFailure(401)
        advanceUntilIdle()

        val state = viewModel.state.value
        assertTrue(state.loggedIn)
        assertEquals("host-1", state.activeSessionId)
        assertEquals(listOf("host-1", "host-2"), state.sessions.map { it.id })
        assertTrue(repository.refreshAuthCalls >= initialRefreshCalls + 1)
        assertTrue(wsClient.connectCount > initialConnectCount)
        assertFalse(wallScheduler.resetCursorCalled)
    }

    @Test
    fun websocketUnauthorizedRefreshFailureExpiresSession() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
            refreshAuthResult = false,
        )
        val wsClient = FakeWsClient()
        val wallScheduler = RecordingWallWorkScheduler()
        val viewModel = AppViewModel(
            repository,
            wsClient,
            wallWorkScheduler = wallScheduler,
        )
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                shareToken = null,
                connectionState = ConnectionState.Connected,
                hasControl = true,
            ),
        )
        viewModel.selectSession("host-1")
        advanceUntilIdle()
        val initialRefreshCalls = repository.refreshAuthCalls

        wsClient.fireFailure(401)
        advanceUntilIdle()

        val state = viewModel.state.value
        assertTrue(state.loggedIn)
        assertNull(state.activeSessionId)
        assertTrue(state.sessions.isEmpty())
        assertEquals("session expired", state.status?.message)
        assertEquals(initialRefreshCalls + 1, repository.refreshAuthCalls)
        assertTrue(wallScheduler.resetCursorCalled)
    }

    @Test
    fun websocketAuthorizationFrameRefreshSuccessPreservesSessionsAndRecovers() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
            refreshAuthResult = true,
        )
        val wsClient = FakeWsClient()
        val wallScheduler = RecordingWallWorkScheduler()
        val viewModel = AppViewModel(
            repository,
            wsClient,
            wallWorkScheduler = wallScheduler,
        )
        advanceUntilIdle()

        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(
                loggedIn = true,
                endpoint = "https://localhost:12843/v1",
                sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
                activeSessionId = "host-1",
                shareToken = null,
                connectionState = ConnectionState.Connected,
                hasControl = true,
            ),
        )
        viewModel.selectSession("host-1")
        advanceUntilIdle()
        val initialRefreshCalls = repository.refreshAuthCalls

        wsClient.fireFrame(errorFrame("authorization failed"))
        advanceUntilIdle()

        val state = viewModel.state.value
        assertTrue(state.loggedIn)
        assertEquals("host-1", state.activeSessionId)
        assertEquals(listOf("host-1"), state.sessions.map { it.id })
        assertTrue(repository.refreshAuthCalls >= initialRefreshCalls + 1)
        assertFalse(wallScheduler.resetCursorCalled)
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
    fun shouldEnableBackgroundWallServiceWhenLoggedInUnlockedAndEnabled() {
        assertTrue(
            AppViewModel.shouldEnableBackgroundWallService(
                loggedIn = true,
                shareToken = null,
                requiresUnlock = false,
                backgroundWallEnabled = true,
            ),
        )
        assertFalse(
            AppViewModel.shouldEnableBackgroundWallService(
                loggedIn = false,
                shareToken = null,
                requiresUnlock = false,
                backgroundWallEnabled = true,
            ),
        )
        assertFalse(
            AppViewModel.shouldEnableBackgroundWallService(
                loggedIn = true,
                shareToken = null,
                requiresUnlock = true,
                backgroundWallEnabled = true,
            ),
        )
        assertFalse(
            AppViewModel.shouldEnableBackgroundWallService(
                loggedIn = true,
                shareToken = "token",
                requiresUnlock = false,
                backgroundWallEnabled = true,
            ),
        )
    }

    @Test
    fun setBackgroundWallEnabledStartsBackgroundWallServiceWhileForegrounded() = runTest {
        val repository = FakeRepository(backgroundWallEnabled = false)
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

        viewModel.setBackgroundWallEnabled(true)
        advanceUntilIdle()

        assertTrue(controller.enabledValues.contains(true))
        assertEquals(true, controller.enabledValues.lastOrNull())
    }

    @Test
    fun onAppForegroundKeepsBackgroundWallServiceRunningAfterBackgroundEnable() = runTest {
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
        assertEquals(true, controller.enabledValues.lastOrNull())
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
                loggedIn = true,
                hasControl = true,
            ),
        )
        setWebSocketForTest(viewModel, wsClient.fakeSocket)

        viewModel.sendRawBytes("\u001b[B".encodeToByteArray())

        assertArrayEquals("\u001bOB".encodeToByteArray(), wsClient.lastSentBytes)
    }

    @Test
    fun ownerInputReclaimsControlWithoutDroppingOrRepeatingFirstPayload() = runTest {
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(FakeRepository(), wsClient)
        advanceUntilIdle()
        setUiStateForTest(
            viewModel,
            viewModel.state.value.copy(loggedIn = true, connectionState = ConnectionState.Connected, hasControl = false),
        )
        setWebSocketForTest(viewModel, wsClient.fakeSocket)

        viewModel.sendRawInput("z")
        assertArrayEquals("z".encodeToByteArray(), wsClient.lastSentBytes)
        assertEquals(1L, viewModel.state.value.localInputNonce)
        viewModel.sendRawBytes(byteArrayOf(0x1b, 0x0d))
        assertArrayEquals(byteArrayOf(0x1b, 0x0d), wsClient.lastSentBytes)
        assertEquals(2, wsClient.sentInputs.size)
        assertEquals(2L, viewModel.state.value.localInputNonce)
        assertFalse(viewModel.state.value.hasControl)
        assertEquals(0, wsClient.connectCount)
    }

    @Test
    fun authenticatedShareScopeGatesTypingIndependentlyOfLeaseOwnership() = runTest {
        val wsClient = FakeWsClient { _, listener, socket -> listener.onOpen(socket) }
        val viewModel = AppViewModel(FakeRepository(), wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))
        for (scope in listOf("control", "view", "", "unknown")) {
            wsClient.shareSession = RelayShareSession(sessionId = "real-session", scope = scope)
            viewModel.handleSharedToken("token-$scope", "https://example")
            advanceUntilIdle()
            wsClient.fireConnect()
            advanceUntilIdle()
            setUiStateForTest(viewModel, viewModel.state.value.copy(
                connectionState = ConnectionState.Connected, hasControl = scope != "control",
            ))
            val sentBefore = wsClient.sentInputs.size
            val nonceBefore = viewModel.state.value.localInputNonce
            viewModel.sendRawInput("q")
            viewModel.sendRawBytes(byteArrayOf(0x1b, 0x0d))
            if (scope == "control") {
                assertEquals(sentBefore + 2, wsClient.sentInputs.size)
                assertArrayEquals("q".encodeToByteArray(), wsClient.sentInputs[sentBefore])
                assertArrayEquals(byteArrayOf(0x1b, 0x0d), wsClient.sentInputs[sentBefore + 1])
                assertEquals(nonceBefore + 2, viewModel.state.value.localInputNonce)
                assertFalse(viewModel.state.value.hasControl)
            } else {
                assertEquals(sentBefore, wsClient.sentInputs.size)
                assertEquals(nonceBefore, viewModel.state.value.localInputNonce)
                assertEquals("view-only session", viewModel.state.value.status?.message)
            }
        }
    }

    @Test
    fun inputRemainsBlockedWhileLockedDisconnectedOrUnauthenticated() = runTest {
        val wsClient = FakeWsClient()
        val viewModel = AppViewModel(FakeRepository(), wsClient)
        advanceUntilIdle()
        setWebSocketForTest(viewModel, wsClient.fakeSocket)
        val owner = viewModel.state.value.copy(loggedIn = true, connectionState = ConnectionState.Connected, hasControl = false)
        for (state in listOf(owner.copy(requiresAppUnlock = true), owner.copy(connectionState = ConnectionState.Disconnected), owner.copy(loggedIn = false, hasControl = true))) {
            setUiStateForTest(viewModel, state)
            viewModel.sendRawInput("z")
            viewModel.sendRawBytes(byteArrayOf(0x0d))
            assertTrue(wsClient.sentInputs.isEmpty())
            assertEquals(0L, viewModel.state.value.localInputNonce)
        }
    }

    @Test
    fun sharePermissionIsClearedWhenTokenChangesBeforeAuthentication() = runTest {
        val wsClient = FakeWsClient { _, listener, socket -> listener.onOpen(socket) }
        wsClient.shareSession = RelayShareSession(sessionId = "real-session", scope = "control")
        val viewModel = AppViewModel(FakeRepository(), wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))
        viewModel.handleSharedToken("control", "https://example")
        advanceUntilIdle()
        wsClient.fireConnect()
        advanceUntilIdle()
        assertTrue(viewModel.state.value.canSendInput)
        viewModel.handleSharedToken("view", "https://example")
        assertFalse(viewModel.state.value.canSendInput)
        assertFalse(viewModel.state.value.shareCanControl)
        advanceUntilIdle()
        viewModel.showShareToken(false)
        assertFalse(viewModel.state.value.canSendInput)
        assertFalse(viewModel.state.value.shareCanControl)
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
        // Bootstrap establishes the initial connection from persisted auth.
        val viewModel = AppViewModel(repository, wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))
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
    fun reconnectPreservesReplayCursorAfterUnsequencedSessionsFrame() = runTest {
        val connects = mutableListOf<Long>()
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { options, listener, socket ->
            connects.add(options.lastSeq)
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
            if (options.lastSeq == 0L) {
                listener.onFrame(socket, snapshotFrame(7, "initial"))
                listener.onFrame(socket, sessionsFrame("host-1"))
            } else {
                listener.onFrame(socket, diffFrame(8, "replayed"))
            }
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

        assertEquals("initial", viewModel.state.value.activeSnapshot?.title)
        assertEquals(0L, viewModel.state.value.lastFrameSeq)

        wsClient.fakeSocket.close(1000, "test reconnect")
        advanceTimeBy(1000)
        advanceUntilIdle()
        wsClient.fireConnect()
        advanceUntilIdle()

        assertEquals(listOf(0L, 7L), connects)
        assertEquals(7L, wsClient.lastConnectOptions?.lastSeq)
        assertEquals("replayed", viewModel.state.value.activeSnapshot?.title)
    }

    @Test
    fun clearingViewModelClosesTerminalWebSocketWithoutReconnect() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
        }
        val viewModel = AppViewModel(repository, wsClient, ioDispatcher = StandardTestDispatcher(testScheduler))
        advanceUntilIdle()
        wsClient.fireConnect()
        advanceUntilIdle()

        assertEquals(1, wsClient.connectCount)
        assertEquals(0, wsClient.closeCount)

        invokeOnClearedForTest(viewModel)
        advanceTimeBy(5_000)
        advanceUntilIdle()

        assertEquals(1, wsClient.closeCount)
        assertEquals(1, wsClient.connectCount)
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
    fun foregroundLiveWallFrameShowsInAppBannerAdvancesCursorAndSuppressesReplay() = runTest {
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
        }
        val recordingNotifier = RecordingWallNotifier()
        val wallStore = testWallWorkStateStore(backgroundScope)
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

        viewModel.onAppForegroundAt(1_000L)
        advanceUntilIdle()

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
        runCurrent()
        wsClient.fireFrame(frame)
        runCurrent()

        assertEquals(emptyList<Pair<String, String>>(), recordingNotifier.deliveries)
        assertEquals("relay: bash inactive", viewModel.state.value.transientStatus?.message)
        assertEquals(42L, wallStore.loadCursor("https://localhost:12843/v1"))
    }

    @Test
    fun foregroundWallSurvivesFollowingSnapshot() = assertForegroundWallDelivery(
        terminalUpdate = snapshotFrame(4, "after wall"),
    )

    @Test
    fun foregroundWallSurvivesFollowingDiff() = assertForegroundWallDelivery(
        terminalUpdate = diffFrame(4, "after wall"),
    )

    @Test
    fun foregroundWallRecoversAfterRelayCounterRestarts() = assertForegroundWallDelivery(
        priorCursor = 100L,
        wallId = 1L,
    )

    private fun assertForegroundWallDelivery(
        terminalUpdate: Frame? = null,
        priorCursor: Long = 0L,
        wallId: Long = 42L,
    ) = runTest {
        val endpoint = "https://localhost:12843/v1"
        val repository = FakeRepository(
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            failListSessions = false,
        )
        repository.onListWallEvents = { _, _ -> RelayWallEventsPage(nextId = wallId) }
        val wsClient = FakeWsClient { _, listener, socket ->
            listener.onOpen(socket)
            listener.onFrame(socket, welcomeFrame())
        }
        val notifier = RecordingWallNotifier()
        val store = testWallWorkStateStore(backgroundScope)
        store.advanceCursor(endpoint, priorCursor)
        val viewModel = AppViewModel(
            repository = repository,
            wsClient = wsClient,
            wallDeliveryCoordinator = MonotonicWallDeliveryCoordinator(store, notifier),
        )
        setUiStateForTest(viewModel, viewModel.state.value.copy(
            loggedIn = true, endpoint = endpoint,
            sessions = listOf(RelaySession(id = "host-1", name = "Host 1", status = "active")),
            activeSessionId = "host-1", connectionState = ConnectionState.Disconnected,
        ))
        viewModel.selectSession("host-1")
        advanceUntilIdle()
        wsClient.fireConnect()
        wsClient.fireFrame(snapshotFrame(2, "before wall"))
        viewModel.state.first { it.lastFrameSeq == 2L && it.lastFrameType == "snapshot" }
        val wall = Frame.newBuilder().setSeq(3).setWall(
            systems.pkt.lingon.protocol.Wall.newBuilder()
                .setId(wallId).setSender("relay").setMessage("regression wall"),
        ).build()
        wsClient.fireFrame(wall)
        viewModel.state.first { it.lastFrameType == "wall" }
        assertEquals("relay: regression wall", viewModel.state.value.bannerStatus?.message)
        if (terminalUpdate != null) {
            wsClient.fireFrame(terminalUpdate)
            viewModel.state.first { it.lastFrameSeq == 4L }
            assertEquals("relay: regression wall", viewModel.state.value.bannerStatus?.message)
        }
        assertEquals(wallId, store.loadCursor(endpoint))
        assertEquals(emptyList<Pair<String, String>>(), notifier.deliveries)
        advanceTimeBy(4_999L)
        runCurrent()
        assertEquals("relay: regression wall", viewModel.state.value.bannerStatus?.message)
        advanceTimeBy(1L)
        runCurrent()
        assertNull(viewModel.state.value.transientStatus)
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

        advanceTimeBy(4999)
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

        advanceTimeBy(4999)
        runCurrent()
        assertEquals("wall 5m", viewModel.state.value.transientStatus?.message)

        advanceTimeBy(1)
        runCurrent()
        assertNull(viewModel.state.value.transientStatus)
    }

    @Test
    fun logoutCompletesLocallyWhenRelayIsUnreachable() = runTest {
        val repository = FakeRepository(failListSessions = false)
        repository.onLogout = { throw java.net.ConnectException("offline") }
        val model = AppViewModel(repository, FakeWsClient())
        setUiStateForTest(model, model.state.value.copy(loggedIn = true, username = "owner", shareToken = "shared"))

        model.logout()
        advanceUntilIdle()

        assertFalse(model.state.value.loggedIn)
        assertNull(model.state.value.username)
        assertNull(model.state.value.shareToken)
        assertNull(model.state.value.activeSessionId)
        assertTrue(model.state.value.sessions.isEmpty())
        assertEquals(1, repository.clearAuthCalls)
        assertFalse(model.state.value.isBusy)
        assertEquals("offline", model.state.value.status?.message)
    }

    @Test
    fun logoutCancellationIsNotReportedAsNetworkFailure() = runTest {
        val repository = FakeRepository(failListSessions = false)
        repository.onLogout = { throw kotlinx.coroutines.CancellationException("cancelled") }
        val model = AppViewModel(repository, FakeWsClient())

        model.logout()
        advanceUntilIdle()

        assertTrue(model.state.value.status?.level != StatusLevel.Error)
        assertEquals(1, repository.clearAuthCalls)
        assertFalse(model.state.value.isBusy)
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
    followOnReadEnabled: Boolean = false,
    initialSessionZooms: Map<String, Float> = emptyMap(),
    initialLastActiveSessionByEndpoint: Map<String, String> = emptyMap(),
    savedEndpoints: List<String> = listOf("https://localhost:12843/v1"),
    initialCertificates: Map<String, List<TrustedCert>> = emptyMap(),
) : LingonClient {
    private val backgroundWallEnabledState = MutableStateFlow(backgroundWallEnabled)
    private val followOnReadEnabledState = MutableStateFlow(followOnReadEnabled)
    private val endpointState = MutableStateFlow("https://localhost:12843/v1")
    override val endpointFlow: Flow<String> = endpointState
    override val fontSizeFlow: Flow<Int> = MutableStateFlow(14)
    override val resizeHostFlow: Flow<Boolean> = MutableStateFlow(false)
    override val backgroundWallEnabledFlow: Flow<Boolean> =
        backgroundWallEnabledFlowOverride ?: backgroundWallEnabledState
    override val followOnReadEnabledFlow: Flow<Boolean> = followOnReadEnabledState
    override val appLockTimeoutMinutesFlow: Flow<Int> = MutableStateFlow(appLockMinutes)
    override val savedEndpointsFlow: Flow<List<String>> = MutableStateFlow(savedEndpoints)
    override val certificatesFlow: Flow<Map<String, List<TrustedCert>>> = MutableStateFlow(initialCertificates)
    private val lastActiveSessionByEndpoint = initialLastActiveSessionByEndpoint.toMutableMap()
    val managementRequests = mutableListOf<systems.pkt.lingon.data.relay.SessionManagementRequest>()
    var onManage: (suspend () -> Unit)? = null
    var onListSessions: (suspend () -> List<RelaySession>)? = null
    var onListWallEvents: (suspend (Long, Int) -> RelayWallEventsPage)? = null
    var onLoadLastActiveSession: (suspend () -> String?)? = null
    var refreshAuthCalls: Int = 0
    var saveZoomCalls: Int = 0
    var onSetBackgroundWallEnabled: ((Boolean) -> Unit)? = null
    var onRefreshAuth: (suspend () -> Unit)? = null
    var onLogout: (suspend () -> Unit)? = null
    var clearAuthCalls: Int = 0
    val savedZooms = initialSessionZooms.toMutableMap()

    override fun setEndpoint(value: String) {
        endpointState.value = value
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

    override fun setFollowOnReadEnabled(value: Boolean) {
        followOnReadEnabledState.value = value
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
        onLogout?.invoke()
    }

    override suspend fun clearAuth() {
        clearAuthCalls += 1
    }

    override suspend fun refreshAuth(): Boolean {
        refreshAuthCalls += 1
        onRefreshAuth?.invoke()
        refreshAuthError?.let { throw it }
        return refreshAuthResult
    }

    override suspend fun loadSessionZoom(endpoint: String, sessionId: String): Float {
        return savedZooms["${endpoint.trim()}|${sessionId.trim()}"] ?: DefaultTerminalZoom
    }

    override suspend fun loadLastActiveSessionId(endpoint: String): String? {
        onLoadLastActiveSession?.let { return it() }
        val cleanedEndpoint = endpoint.trim()
        if (cleanedEndpoint.isBlank()) return null
        return lastActiveSessionByEndpoint[cleanedEndpoint]
    }

    override suspend fun manageSession(operation: systems.pkt.lingon.data.relay.SessionManagementRequest): systems.pkt.lingon.data.relay.SessionManagementResult {
        managementRequests += operation
        onManage?.invoke()
        return systems.pkt.lingon.data.relay.SessionManagementResult(sessionId = operation.sessionId, name = operation.name)
    }

    override suspend fun listSessions(): List<RelaySession> {
        onListSessions?.let { return it() }
        if (failListSessions) {
            throw ApiException("invalid session", 401)
        }
        return sessionProvider?.invoke() ?: sessions
    }

    override suspend fun listWallEvents(sinceId: Long, limit: Int): RelayWallEventsPage {
        onListWallEvents?.let { return it(sinceId, limit) }
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
    private val provider: HttpClientProvider = testHttpClientProvider(),
    var onConnect: (ConnectOptions, Listener, WebSocket) -> Unit = { _, _, _ -> },
) : RelayWebSocketClient(
    provider,
) {
    var connectCount: Int = 0
    var lastConnectOptions: ConnectOptions? = null
    var lastSentBytes: ByteArray? = null
    val sentInputs = mutableListOf<ByteArray>()
    var lastSentCommand: CommandKind? = null
    var resizeCount: Int = 0
    var closeCount: Int = 0
    var shareSession: RelayShareSession = RelayShareSession(sessionId = "shared-real", name = "Shared session", scope = "view")
    var onAuthenticateShareSession: ((okhttp3.HttpUrl, String) -> RelayShareSession)? = null
    private var pendingConnect: (() -> Unit)? = null
    private val listeners = mutableMapOf<WebSocket, Listener>()
    var fakeSocket: WebSocket = newSocket()
        private set

    private fun newSocket(): WebSocket = object : WebSocket {
        override fun request(): okhttp3.Request = okhttp3.Request.Builder().url("https://localhost/").build()
        override fun queueSize(): Long = 0
        override fun send(text: String): Boolean = true
        override fun send(bytes: okio.ByteString): Boolean = true
        override fun close(code: Int, reason: String?): Boolean {
            closeCount += 1
            listeners[this]?.onClosed(this, code, reason)
            return true
        }
        override fun cancel() {}
    }
    private var lastListener: RelayWebSocketClient.Listener? = null

    override fun connect(options: ConnectOptions, listener: Listener): WebSocket {
        if (connectCount > 0) fakeSocket = newSocket()
        connectCount += 1
        lastConnectOptions = options
        lastListener = listener
        val socket = fakeSocket
        listeners[socket] = listener
        pendingConnect = { onConnect(options, listener, socket) }
        return fakeSocket
    }

    override fun authenticateShareSession(baseUrl: okhttp3.HttpUrl, shareToken: String): AuthenticatedShareSession {
        val session = onAuthenticateShareSession?.invoke(baseUrl, shareToken) ?: shareSession
        return AuthenticatedShareSession(session, baseUrl, shareToken, provider.clientFor(baseUrl))
    }

    fun fireConnect() {
        val callback = pendingConnect ?: return
        pendingConnect = null
        callback()
    }

    fun fireFrame(frame: Frame) {
        lastListener?.onFrame(fakeSocket, frame)
    }

    fun fireFailure(responseCode: Int) {
        val response = Response.Builder()
            .request(Request.Builder().url("https://localhost/ws").build())
            .protocol(Protocol.HTTP_1_1)
            .code(responseCode)
            .message("status $responseCode")
            .build()
        lastListener?.onFailure(fakeSocket, RuntimeException("websocket failed"), response)
    }

    override fun sendInput(webSocket: WebSocket, data: ByteArray) {
        lastSentBytes = data.copyOf()
        sentInputs.add(data.copyOf())
    }

    override fun sendInput(webSocket: WebSocket, text: String) {
        lastSentBytes = text.toByteArray()
    }

    override fun sendResize(webSocket: WebSocket, cols: Int, rows: Int) {
        resizeCount += 1
    }

    override fun sendCommand(webSocket: WebSocket, kind: CommandKind) {
        lastSentCommand = kind
    }
}

private class RecordingWallWorkScheduler : WallWorkScheduler {
    var resetCursorCalled = false
    val enabledValues = mutableListOf<Boolean>()

    override fun setEnabled(enabled: Boolean) {
        enabledValues += enabled
    }

    override fun resetCursor() {
        resetCursorCalled = true
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

private fun invokeOnClearedForTest(viewModel: AppViewModel) {
    val method = AppViewModel::class.java.getDeclaredMethod("onCleared")
    method.isAccessible = true
    method.invoke(viewModel)
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

private fun sessionsFrame(sessionId: String): Frame {
    return Frame.newBuilder()
        .setSessions(
            Sessions.newBuilder()
                .addSessions(
                    SessionInfo.newBuilder()
                        .setId(sessionId)
                        .setName(sessionId)
                        .setStatus("active")
                        .build(),
                )
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

private fun errorFrame(message: String): Frame {
    return Frame.newBuilder()
        .setError(
            systems.pkt.lingon.protocol.Error.newBuilder()
                .setMessage(message)
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

private fun testWallWorkStateStore(scope: CoroutineScope): WallWorkStateStore {
    val dataStore = PreferenceDataStoreFactory.create(
        scope = scope,
        produceFile = { Files.createTempFile("lingon-wall", ".preferences_pb").toFile() },
    )
    return WallWorkStateStore(dataStore)
}

private class RecordingWallNotifier : WallNotifier {
    val deliveries = mutableListOf<Pair<String, String>>()

    override fun notifyWall(notification: WallNotification): Boolean {
        deliveries += "${notification.endpoint.trim()}#${notification.eventId}" to
            "${notification.sender.trim()}\n${notification.message.trim()}"
        return true
    }
}

private class RecordingBackgroundWallServiceController : BackgroundWallServiceController {
    val enabledValues = mutableListOf<Boolean>()

    override fun setEnabled(enabled: Boolean) {
        enabledValues += enabled
    }
}
