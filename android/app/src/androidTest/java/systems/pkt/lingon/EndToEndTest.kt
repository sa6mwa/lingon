package systems.pkt.lingon

import android.Manifest
import android.app.Notification
import android.app.NotificationManager
import android.content.ClipData
import android.content.ClipboardManager
import android.service.notification.StatusBarNotification
import android.graphics.Bitmap
import android.graphics.Canvas
import android.os.ParcelFileDescriptor
import android.view.KeyEvent
import androidx.activity.compose.setContent
import androidx.core.app.NotificationManagerCompat
import androidx.core.view.WindowInsetsCompat
import androidx.test.rule.GrantPermissionRule
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextReplacement
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.joinAll
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.UiObject2
import androidx.test.uiautomator.Until
import android.os.SystemClock
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.view.WindowManager
import android.view.inputmethod.InputMethodManager
import android.view.inputmethod.EditorInfo
import android.widget.EditText
import android.widget.FrameLayout
import androidx.compose.ui.graphics.Color
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import java.net.HttpURLConnection
import java.net.URL
import java.util.Base64
import java.util.Locale
import java.util.zip.CRC32
import java.io.File
import java.io.FileInputStream
import java.io.InputStreamReader
import systems.pkt.lingon.viewmodel.ConnectionState
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.WebSocket
import systems.pkt.lingon.data.relay.RelayWebSocketClient
import systems.pkt.lingon.protocol.Frame
import java.io.FileOutputStream
import java.util.UUID
import java.util.concurrent.atomic.AtomicBoolean
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flow
import org.junit.After
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.junit.rules.RuleChain
import systems.pkt.lingon.test.FailureCaptureRule
import systems.pkt.lingon.ui.LingonAppContent
import systems.pkt.lingon.ui.TestTags
import systems.pkt.lingon.viewmodel.AppViewModel
import systems.pkt.lingon.viewmodel.AppViewModelFactory
import systems.pkt.lingon.viewmodel.UiState
import systems.pkt.lingon.terminal.TerminalGridView
import systems.pkt.lingon.terminal.TerminalPalette
import systems.pkt.lingon.terminal.TerminalSnapshot
import systems.pkt.lingon.terminal.TerminalViewportState
import systems.pkt.lingon.terminal.buildScrollbackSnapshot
import systems.pkt.lingon.protocol.ScrollbackRow
import kotlin.math.min
import kotlin.math.max

@RunWith(AndroidJUnit4::class)
class EndToEndTest {
    private val composeRule = createAndroidComposeRule<MainActivity>()
    private val notificationPermissionRule: GrantPermissionRule =
        GrantPermissionRule.grant(Manifest.permission.POST_NOTIFICATIONS)

    @get:Rule
    val ruleChain: RuleChain = RuleChain
        .outerRule(notificationPermissionRule)
        .around(composeRule)
        .around(FailureCaptureRule(composeRule))

    private val testConfig = TestConfig.fromArgs()
    private val screenshotRunId = UUID.randomUUID().toString().take(8)
    private val startedHeadlessSessions = linkedSetOf<String>()
    private val startedHostSessions = linkedSetOf<String>()

    @Before
    fun ensureBackendReachable() {
        restoreNotificationDelivery()
        clearAppNotifications()
        configureEndpointAndCerts()
        assertBackendReachable(testConfig.endpoint, testConfig.caPem)
    }

    @After
    fun restoreDeviceState() {
        runCatching { restoreNotificationDelivery() }
        runCatching { restoreSoftInputAdjustResize() }
        runCatching { resumeActivity() }
        runCatching { detachStartedHeadlessSessions() }
        runCatching { stopHostsViaHarness(startedHostSessions.toList()) }
        runCatching {
            composeRule.runOnUiThread {
                appViewModel().setBackgroundWallEnabled(false)
                appViewModel().setFollowOnReadEnabled(false)
            }
            composeRule.waitForIdle()
        }
        runCatching { clearAppNotifications() }
        runCatching { ensureLoggedOut() }
    }

    @Test
    fun top_bar_settings_is_accessible() {
        assertTopBarSafe()
    }

    @Test
    fun settings_page_opens_and_returns_to_login() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        waitForTag(TestTags.LoginUsername)
        composeRule.onNodeWithTag(TestTags.TopBarSettingsButton).performClick()
        waitForTag(TestTags.SettingsScreen)
        composeRule.onNodeWithTag(TestTags.SettingsBackButton).performClick()
        waitForTagToDisappear(TestTags.SettingsScreen)
        waitForTag(TestTags.LoginUsername)
    }

    @Test
    fun settings_page_uses_rendered_back_button() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        waitForTag(TestTags.LoginUsername)
        val device = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
        val settingsButton = device.wait(Until.findObject(By.desc("Settings")), 5_000)
            ?: throw AssertionError("top-bar settings button was not visible to UIAutomator")
        settingsButton.click()
        waitForTag(TestTags.SettingsScreen)
        val backButton = device.wait(Until.findObject(By.desc("Back")), 5_000)
            ?: throw AssertionError("settings back button was not visible to UIAutomator")
        clickRenderedObjectOrClickableAncestor(backButton)
        waitForTagToDisappear(TestTags.SettingsScreen)
        device.wait(Until.findObject(By.desc("Settings")), 5_000)
            ?: throw AssertionError("top-bar settings button did not return after closing settings")
    }

    @Test
    fun settings_page_closes_and_reload_triggers() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)

        composeRule.onNodeWithTag(TestTags.TopBarSettingsButton).performClick()
        waitForTag(TestTags.SettingsScreen)
        composeRule.onNodeWithTag(TestTags.SettingsBackButton).performClick()
        waitForTagToDisappear(TestTags.SettingsScreen)
        composeRule.onNodeWithContentDescription("Settings")

        val refreshBefore = appViewModel().state.value.lastManualRefreshAtMs
        composeRule.onNodeWithTag(TestTags.ReloadButton, useUnmergedTree = true).performClick()
        waitUntilNoError(5_000) { appViewModel().state.value.lastManualRefreshAtMs > refreshBefore }
        waitUntilNoError(10_000) { !appViewModel().state.value.isRefreshing }
    }

    @Test
    fun follow_on_read_settings_toggle_defaults_off_and_toggles() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        composeRule.runOnUiThread {
            appViewModel().setFollowOnReadEnabled(false)
        }
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) { !appViewModel().state.value.followOnReadEnabled }

        openSettings()
        composeRule.onNodeWithTag(TestTags.FollowOnReadToggle, useUnmergedTree = true).assertIsOff()
        composeRule.onNodeWithTag(TestTags.FollowOnReadMenuItem, useUnmergedTree = true).performClick()
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) { appViewModel().state.value.followOnReadEnabled }

        composeRule.onNodeWithTag(TestTags.FollowOnReadToggle, useUnmergedTree = true).assertIsOn()
        composeRule.onNodeWithTag(TestTags.FollowOnReadMenuItem, useUnmergedTree = true).performClick()
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) { !appViewModel().state.value.followOnReadEnabled }
        composeRule.onNodeWithTag(TestTags.SettingsBackButton).performClick()
        waitForTagToDisappear(TestTags.SettingsScreen)
    }

    @Test
    fun endpoint_setting_dialog_stays_in_settings_flow() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        openSettings()
        composeRule.onNodeWithTag(TestTags.EndpointButton).performClick()
        waitForTag(TestTags.EndpointInput)
        composeRule.onNodeWithTag(TestTags.SettingsScreen).assertExists()
        composeRule.onNodeWithTag(TestTags.LoginUsername).assertDoesNotExist()

        composeRule.onNodeWithTag(TestTags.EndpointInput).performTextReplacement(testConfig.endpoint)
        composeRule.onNodeWithTag(TestTags.EndpointSave).performClick()
        waitForTagToDisappear(TestTags.EndpointInput)
        composeRule.onNodeWithTag(TestTags.SettingsScreen).assertExists()
        composeRule.onNodeWithTag(TestTags.SettingsBackButton).performClick()
        waitForTagToDisappear(TestTags.SettingsScreen)
    }

    @Test
    fun certificates_setting_returns_to_settings_flow() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        openSettings()
        composeRule.onNodeWithTag(TestTags.CertificatesButton).performClick()
        waitForTag(TestTags.SettingsCertificatesScreen)
        composeRule.onNodeWithTag(TestTags.SettingsScreen).assertExists()
        composeRule.onNodeWithText("Done").performClick()
        waitForTagToDisappear(TestTags.SettingsCertificatesScreen)
        composeRule.onNodeWithTag(TestTags.SettingsScreen).assertExists()
        composeRule.onNodeWithTag(TestTags.SettingsBackButton).performClick()
        waitForTagToDisappear(TestTags.SettingsScreen)
    }

    @Test
    fun refreshes_sessions_when_host_starts_late() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUserNoTerminal()
        waitUntilNoError(5_000) { appViewModel().state.value.sessions.isEmpty() }
        waitForTagToDisappear(TestTags.TabList)

        startHostViaHarness()

        waitUntilNoError(20_000) { appViewModel().state.value.sessions.isNotEmpty() }
        waitForTerminalReady(timeoutMs = 20_000L)
    }

    @Test
    fun login_with_invalid_credentials_shows_error() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        attemptLogin(testConfig.username, "wrong-password", generateTotp(testConfig.totpSecret))
        waitForTag(TestTags.LoginError)
        waitForTag(TestTags.LoginUsername)
    }

    @Test
    fun login_with_unreachable_endpoint_shows_error() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        setEndpointViaUi(UNREACHABLE_ENDPOINT)
        attemptLogin(testConfig.username, testConfig.password, generateTotp(testConfig.totpSecret))
        waitForTag(TestTags.LoginError)
        waitForTag(TestTags.LoginUsername)
    }

    @Test
    fun login_success_shows_prompt() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
    }

    @Test
    fun reopened_shared_session_restores_persisted_zoom() {
        val token = testConfig.viewToken ?: throw AssertionError("missing share token")
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        attachViaShareToken(token)
        val state = appViewModel().state.value
        val sessionId = state.activeSessionId ?: throw AssertionError("missing authenticated share session")
        val app = composeRule.activity.application as LingonApplication
        try {
            setZoomFactor(1.7f)
            val zoomed = waitForTerminalDebugInfo(SHORT_UI_TIMEOUT_MS) { info ->
                info.activeSessionId == sessionId && kotlin.math.abs(info.zoomFactor - 1.7f) < 0.001f
            } ?: throw AssertionError("shared terminal did not apply initial zoom")
            waitUntil(10_000L) {
                runBlocking { kotlin.math.abs(app.repository.loadSessionZoom(state.endpoint, sessionId) - 1.7f) < 0.001f }
            }
            ensureLoggedOut()
            attachViaShareToken(token)
            val restored = waitForTerminalDebugInfo(SHORT_UI_TIMEOUT_MS) { info ->
                info.activeSessionId == sessionId && kotlin.math.abs(info.zoomFactor - 1.7f) < 0.001f
            } ?: throw AssertionError("reopened authenticated share did not restore persisted 1.7x zoom; state=${appViewModel().state.value.zoomFactor}")
            assertEquals(1.7f, restored.zoomFactor, 0.001f)
            assertEquals(zoomed.renderScaleX, restored.renderScaleX, 0.03f)
            assertEquals(zoomed.renderScaleY, restored.renderScaleY, 0.03f)
        } finally {
            resetZoomPan()
        }
    }

    @Test
    fun settings_session_management_hidden_for_share() {
        val token = testConfig.viewToken ?: throw AssertionError("missing share token")
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        attachViaShareToken(token)
        openSettings()
        composeRule.onAllNodesWithTag(TestTags.NewTerminalButton).assertCountEquals(0)
        composeRule.onAllNodesWithTag(TestTags.RenameSessionButton).assertCountEquals(0)
    }

    @Test
    fun rename_dialog_lock_hides_and_discards_private_draft() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        val viewModel = appViewModel()
        val initial = viewModel.state.value
        val sessionId = initial.activeSessionId ?: throw AssertionError("missing active session")
        val originalName = initial.sessions.first { it.id == sessionId }.name.orEmpty()
        val draft = "Private draft before lock"
        val displayed = mutableStateOf(initial)
        val viewportCache = mutableStateMapOf<String, TerminalViewportState>()
        fun assertRenameText(expected: String) {
            val input = composeRule.onNodeWithTag(TestTags.RenameSessionInput).fetchSemanticsNode()
            assertEquals(expected, input.config[SemanticsProperties.EditableText].text)
        }
        composeRule.runOnUiThread {
            composeRule.activity.setContent {
                LingonAppContent(state = displayed.value, viewModel = viewModel, viewportCache = viewportCache)
            }
        }
        composeRule.waitForIdle()
        openSettings()
        composeRule.onNodeWithTag(TestTags.RenameSessionButton).performClick()
        waitForTag(TestTags.RenameSessionInput)
        assertRenameText(originalName)
        composeRule.onNodeWithTag(TestTags.RenameSessionInput).performTextReplacement(draft)
        assertRenameText(draft)

        // This is the state after timeout and cancellation of credential unlock.
        // Exercise the real composition without depending on device credentials
        // or sleeping through a wall-clock lock timeout.
        composeRule.runOnIdle {
            displayed.value = displayed.value.copy(requiresAppUnlock = true, unlockPromptPending = false)
        }
        composeRule.waitForIdle()
        composeRule.onAllNodesWithTag(TestTags.RenameSessionInput).assertCountEquals(0)
        composeRule.onAllNodesWithTag(TestTags.RenameSessionSave).assertCountEquals(0)
        composeRule.onAllNodesWithText(draft).assertCountEquals(0)
        composeRule.onAllNodesWithText("App locked").assertCountEquals(1)

        composeRule.runOnIdle { displayed.value = displayed.value.copy(requiresAppUnlock = false) }
        composeRule.waitForIdle()
        composeRule.onAllNodesWithTag(TestTags.RenameSessionInput).assertCountEquals(0)
        composeRule.onAllNodesWithTag(TestTags.RenameSessionSave).assertCountEquals(0)
        composeRule.onAllNodesWithText(draft).assertCountEquals(0)
        composeRule.onNodeWithTag(TestTags.RenameSessionButton).performClick()
        waitForTag(TestTags.RenameSessionInput)
        assertRenameText(originalName)
    }

    @Test
    fun settings_create_terminal_and_rename_session() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
        val original = appViewModel().state.value.activeSessionId
        composeRule.onNodeWithTag(TestTags.TopBarSettingsButton).performClick()
        waitForTag(TestTags.NewTerminalButton)
        composeRule.onNodeWithTag(TestTags.NewTerminalButton).performClick()
        waitUntilNoError(15_000) {
            val state = appViewModel().state.value
            state.activeSessionId != original && state.activeSessionHeadless && state.connectionState == ConnectionState.Connected
        }
        val created = appViewModel().state.value.activeSessionId!!
        startedHeadlessSessions += created
        // The configured harness script retains its echo label when re-executed.
        assertTerminalResponsive(sessionId = original)
        composeRule.onNodeWithTag(TestTags.TopBarSettingsButton).performClick()
        waitForTag(TestTags.RenameSessionButton)
        composeRule.onNodeWithTag(TestTags.RenameSessionButton).performClick()
        waitForTag(TestTags.RenameSessionInput)
        composeRule.onNodeWithTag(TestTags.RenameSessionInput).performTextReplacement("Android build 🦊")
        composeRule.onNodeWithTag(TestTags.RenameSessionSave).performClick()
        waitUntilNoError(10_000) {
            appViewModel().state.value.sessions.any { it.id == created && it.name == "Android build 🦊" }
        }
        assertEquals(created, appViewModel().state.value.activeSessionId)
        assertTerminalResponsive(sessionId = original)
    }

    @Test
    fun reconnect_session_fetch_does_not_restore_old_endpoint_sessions() {
        assertSupersededReconnectResult(unauthorized = false)
    }

    @Test
    fun stale_reconnect_authentication_failure_preserves_new_account_frames() {
        assertSupersededReconnectResult(unauthorized = true)
    }

    private fun assertSupersededReconnectResult(unauthorized: Boolean) {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        val viewModel = appViewModel()
        val application = composeRule.activity.application as LingonApplication
        val entered = kotlinx.coroutines.CompletableDeferred<Unit>()
        val release = kotlinx.coroutines.CompletableDeferred<Unit>()
        var fetches = 0
        val gated = object : systems.pkt.lingon.data.LingonClient by application.repository {
            override suspend fun listSessions(): List<systems.pkt.lingon.data.relay.RelaySession> {
                fetches += 1
                if (fetches > 1) return application.repository.listSessions()
                val sessions = application.repository.listSessions()
                entered.complete(Unit)
                release.await()
                if (unauthorized) throw systems.pkt.lingon.data.ApiException("obsolete owner expired", 401)
                return sessions
            }
            override suspend fun refreshAuth(): Boolean = false
        }
        val repositoryField = AppViewModel::class.java.getDeclaredField("repository").apply { isAccessible = true }
        val refreshField = AppViewModel::class.java.getDeclaredField("refreshJob").apply { isAccessible = true }
        lateinit var oldJob: Job
        composeRule.runOnIdle {
            repositoryField.set(viewModel, gated)
            // Manual recovery shares the exact refresh path used by timed reconnects;
            // the unit regression drives their backoff through mock time.
            viewModel.manualRefresh()
            oldJob = refreshField.get(viewModel) as Job
        }
        try {
            waitUntilNoError(SHORT_UI_TIMEOUT_MS) { entered.isCompleted }
            val expectedSessions: List<systems.pkt.lingon.data.relay.RelaySession>
            val expectedSession: String?
            if (unauthorized) {
                val secondary = testConfig.secondaryUser() ?: throw AssertionError("missing second account")
                ensureLoggedOut()
                loginWithUser(secondary)
                waitForSessionTick(secondary.sessions.first())
                expectedSessions = viewModel.state.value.sessions
                expectedSession = viewModel.state.value.activeSessionId
                assertEquals(secondary.username, viewModel.state.value.username)
            } else {
                composeRule.runOnIdle { viewModel.updateEndpoint("https://new.example/v1") }
                waitForTagNoError(TestTags.LoginUsername)
                expectedSessions = emptyList()
                expectedSession = null
            }
            release.complete(Unit)
            runBlocking { kotlinx.coroutines.withTimeout(15_000) { oldJob.join() } }
            composeRule.runOnIdle {
                assertEquals(expectedSessions, viewModel.state.value.sessions)
                assertEquals(expectedSession, viewModel.state.value.activeSessionId)
                assertEquals(unauthorized, viewModel.state.value.loggedIn)
            }
            if (unauthorized) {
                assertEquals(ConnectionState.Connected, viewModel.state.value.connectionState)
                waitForTagNoError(TestTags.TerminalInput)
                val seq = viewModel.state.value.lastFrameSeq
                waitUntilNoError(SHORT_UI_TIMEOUT_MS) { viewModel.state.value.lastFrameSeq > seq }
            } else {
                waitForTagNoError(TestTags.LoginUsername)
            }
        } finally {
            release.complete(Unit)
            composeRule.runOnIdle { repositoryField.set(viewModel, application.repository) }
        }
    }

    @Test
    fun management_session_fetch_does_not_restore_old_endpoint_sessions() {
        assertSessionFetchDiscardedAfterEndpointChange(management = true)
    }

    @Test
    fun cold_bootstrap_restores_authenticated_terminal_after_delayed_endpoint() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        val originalModel = appViewModel()
        val expectedSession = originalModel.state.value.activeSessionId
            ?: throw AssertionError("missing persisted session")
        val application = composeRule.activity.application as LingonApplication
        val endpointCollectorStarted = kotlinx.coroutines.CompletableDeferred<Unit>()
        val releaseEndpoint = kotlinx.coroutines.CompletableDeferred<Unit>()
        val store = androidx.lifecycle.ViewModelStore()
        lateinit var restoredModel: AppViewModel
        val gated = object : systems.pkt.lingon.data.LingonClient by application.repository {
            override val endpointFlow = flow {
                endpointCollectorStarted.complete(Unit)
                releaseEndpoint.await()
                application.repository.endpointFlow.collect { emit(it) }
            }
            override suspend fun listSessions(): List<systems.pkt.lingon.data.relay.RelaySession> {
                releaseEndpoint.await()
                // Reproduce a successful response arriving after the UI's first
                // endpoint emission, without depending on disk/network timing.
                restoredModel.state.first { it.endpoint == testConfig.endpoint }
                return application.repository.listSessions()
            }
        }
        try {
            composeRule.runOnIdle {
                restoredModel = AppViewModel(gated, application.wsClient)
                store.put("cold-start", restoredModel)
                composeRule.activity.setContent { systems.pkt.lingon.ui.LingonApp(restoredModel) }
            }
            runBlocking {
                kotlinx.coroutines.withTimeout(15_000) { endpointCollectorStarted.await() }
            }
            composeRule.runOnIdle {
                assertEquals("", restoredModel.state.value.endpoint)
                assertFalse(restoredModel.state.value.loggedIn)
            }
            composeRule.onNodeWithTag(TestTags.LoginUsername).assertExists()
            releaseEndpoint.complete(Unit)
            runBlocking {
                kotlinx.coroutines.withTimeout(15_000) {
                    restoredModel.state.first {
                        it.loggedIn && it.activeSessionId == expectedSession && it.activeSnapshot != null
                    }
                }
            }
            composeRule.waitForIdle()
            composeRule.onNodeWithTag(TestTags.TerminalInput).assertExists()
            composeRule.onAllNodesWithTag(TestTags.LoginUsername).assertCountEquals(0)
            assertTrue(restoredModel.state.value.sessions.any { it.id == expectedSession })
        } finally {
            releaseEndpoint.complete(Unit)
            composeRule.runOnUiThread {
                store.clear()
                composeRule.activity.setContent { systems.pkt.lingon.ui.LingonApp(originalModel) }
            }
        }
    }

    @Test
    fun bootstrap_session_fetch_does_not_restore_old_endpoint_sessions() {
        assertSessionFetchDiscardedAfterEndpointChange(management = false)
    }

    private fun assertSessionFetchDiscardedAfterEndpointChange(management: Boolean) {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        val viewModel = appViewModel()
        val application = composeRule.activity.application as LingonApplication
        val ready = kotlinx.coroutines.CompletableDeferred<Unit>()
        val release = kotlinx.coroutines.CompletableDeferred<Unit>()
        val finished = kotlinx.coroutines.CompletableDeferred<Unit>()
        var fetches = 0
        val gated = object : systems.pkt.lingon.data.LingonClient by application.repository {
            override suspend fun listSessions(): List<systems.pkt.lingon.data.relay.RelaySession> {
                fetches += 1
                if (fetches > 1) throw systems.pkt.lingon.data.ApiException("new context is unauthenticated", 401)
                val sessions = application.repository.listSessions()
                ready.complete(Unit)
                release.await()
                finished.complete(Unit)
                return sessions
            }
        }
        val field = AppViewModel::class.java.getDeclaredField("repository").apply { isAccessible = true }
        composeRule.runOnIdle { field.set(viewModel, gated) }
        try {
            if (management) {
                openSettings()
                composeRule.onNodeWithTag(TestTags.RenameSessionButton).performClick()
                waitForTag(TestTags.RenameSessionInput)
                composeRule.onNodeWithTag(TestTags.RenameSessionInput).performTextReplacement("Superseded rename")
                composeRule.onNodeWithTag(TestTags.RenameSessionSave).performClick()
            } else {
                composeRule.runOnIdle { viewModel.updateEndpoint(testConfig.endpoint) }
            }
            waitUntilNoError(SHORT_UI_TIMEOUT_MS) { ready.isCompleted }
            composeRule.runOnIdle { viewModel.updateEndpoint("https://new.example/v1") }
            release.complete(Unit)
            waitUntilNoError(SHORT_UI_TIMEOUT_MS) { finished.isCompleted }
            composeRule.runOnIdle {
                val state = viewModel.state.value
                assertEquals("https://new.example/v1", state.endpoint)
                assertTrue(state.sessions.isEmpty())
                assertNull(state.activeSessionId)
                assertFalse(state.loggedIn)
                assertFalse(state.sessionManagementBusy)
                assertFalse(state.status?.message == "Session renamed")
            }
            closeSettingsIfOpen()
            waitForTagNoError(TestTags.LoginUsername)
        } finally {
            release.complete(Unit)
            composeRule.runOnIdle { field.set(viewModel, application.repository) }
        }
    }

    @Test
    fun terminal_updates_live() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive(requireControl = false)
        val initial = readTerminalHash()
        waitUntilNoError(8_000) { readTerminalHash() != initial }
    }

    @Test
    fun keyboard_input_echoes_quickly() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
    }

    @Test
    fun keyboard_input_backspace_updates_remote_session() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()

        val sessionId = activeSessionId()
        val expectedSequence = arrayOf(
            "ECHO_${sessionId} 66",
            "ECHO_${sessionId} 6f",
            "ECHO_${sessionId} 6f",
            "ECHO_${sessionId} 7f",
            "ECHO_${sessionId} 62",
            "ECHO_${sessionId} 61",
            "ECHO_${sessionId} 72",
        )

        sendTerminalInput("foo")
        sendTerminalBackspace()
        sendTerminalInput("bar")
        sendTerminalEnter()

        waitUntilNoError(20_000L) { snapshotContainsSequence(*expectedSequence) }
        waitUntilNoError(5_000L) {
            val cr = "ECHO_${sessionId} 0d"
            val lf = "ECHO_${sessionId} 0a"
            snapshotContainsToken(cr) || snapshotContainsToken(lf)
        }
    }

    @Test
    fun keyboard_input_forwards_ctrl_alt_and_modified_arrows() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()

        val sessionId = activeSessionId()
        focusTerminalInput()
        sendModifiedKeyStroke(KeyEvent.KEYCODE_DPAD_UP, alt = true)
        sendModifiedKeyStroke(KeyEvent.KEYCODE_DPAD_DOWN, ctrl = true, alt = true, shift = true)
        sendModifiedKeyStroke(KeyEvent.KEYCODE_D, ctrl = true, alt = true, shift = true)

        waitUntilNoError(20_000L) {
            snapshotContainsSequence(
                "ECHO_${sessionId} 1b",
                "ECHO_${sessionId} 5b",
                "ECHO_${sessionId} 31",
                "ECHO_${sessionId} 3b",
                "ECHO_${sessionId} 33",
                "ECHO_${sessionId} 41",
                "ECHO_${sessionId} 1b",
                "ECHO_${sessionId} 5b",
                "ECHO_${sessionId} 31",
                "ECHO_${sessionId} 3b",
                "ECHO_${sessionId} 38",
                "ECHO_${sessionId} 42",
                "ECHO_${sessionId} 1b",
                "ECHO_${sessionId} 04",
            )
        }
    }

    @Test
    fun touch_modifiers_forward_all_arrows_and_fn_keys_stay_active() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
        val sessionId = activeSessionId()
        focusTerminalInput()

        val arrows = listOf("↑" to "41", "↓" to "42", "→" to "43", "←" to "44")
        for ((label, finalHex) in arrows) {
            for ((ctrl, alt, modifierHex) in listOf(Triple(false, true, "33"), Triple(true, false, "35"), Triple(true, true, "37"))) {
                if (ctrl) composeRule.onNodeWithText("CTRL").performClick()
                if (alt) composeRule.onNodeWithText("ALT").performClick()
                composeRule.onNodeWithText(label).performClick()
                waitUntilNoError(10_000L) {
                    snapshotContainsSequence(
                        "ECHO_${sessionId} 1b", "ECHO_${sessionId} 5b", "ECHO_${sessionId} 31",
                        "ECHO_${sessionId} 3b", "ECHO_${sessionId} $modifierHex", "ECHO_${sessionId} $finalHex",
                    )
                }
            }
        }

        composeRule.onNodeWithText("ALT").performClick()
        composeRule.onNodeWithText("ALT").performClick()
        waitUntilNoError(10_000L) { hasTextNode("FN") }
        val functionKeys = listOf(
            KeyEvent.KEYCODE_1 to listOf("1b", "4f", "50"),
            KeyEvent.KEYCODE_2 to listOf("1b", "4f", "51"),
            KeyEvent.KEYCODE_0 to listOf("1b", "5b", "32", "31", "7e"),
        )
        for ((keyCode, encoded) in functionKeys) {
            sendKeyStroke(keyCode)
            waitUntilNoError(10_000L) { snapshotContainsSequence(*encoded.map { "ECHO_${sessionId} $it" }.toTypedArray()) }
            assertTrue("FN was reset by a function key", hasTextNode("FN"))
        }
        sendModifiedKeyStroke(KeyEvent.KEYCODE_2, shift = true)
        waitUntilNoError(10_000L) { snapshotContainsToken("ECHO_${sessionId} 40") }
        sendModifiedKeyStroke(KeyEvent.KEYCODE_3, shift = true)
        waitUntilNoError(10_000L) { snapshotContainsToken("ECHO_${sessionId} 23") }
        composeRule.onNodeWithText("FN").performClick()
        waitUntilNoError(10_000L) { hasTextNode("ALT") && !hasTextNode("FN") }
    }

    @Test
    fun soft_ime_preserves_supplementary_unicode_in_fn_and_modifier_modes() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
        val sessionId = activeSessionId()
        focusTerminalInput()
        composeRule.onNodeWithText("ALT").performClick()
        composeRule.onNodeWithText("ALT").performClick()
        assertTrue(hasTextNode("FN"))
        fun commitImeText(text: String) = composeRule.runOnUiThread {
            fun findInput(view: View): View? {
                if (view.javaClass.simpleName == "TerminalInputView") return view
                if (view is ViewGroup) {
                    for (index in 0 until view.childCount) {
                        findInput(view.getChildAt(index))?.let { return it }
                    }
                }
                return null
            }
            val input = checkNotNull(findInput(composeRule.activity.window.decorView))
            assertTrue(input.onCreateInputConnection(EditorInfo()).commitText(text, 1))
        }
        commitImeText("😀𠮷e\u0301")
        val expected = "😀𠮷e\u0301".encodeToByteArray().map { "%02x".format(it.toInt() and 0xff) }
        waitUntilNoError(10_000L) { terminalEchoTail(sessionId).takeLast(expected.size) == expected }
        assertTrue("FN was reset by literal input", hasTextNode("FN"))
        composeRule.onNodeWithText("FN").performClick()
        composeRule.onNodeWithText("CTRL").performClick()
        commitImeText("🌍𠮷")
        val ctrlExpected = "🌍𠮷".encodeToByteArray().map { "%02x".format(it.toInt() and 0xff) }
        waitUntilNoError(10_000L) { terminalEchoTail(sessionId).takeLast(ctrlExpected.size) == ctrlExpected }
        composeRule.onNodeWithText("ALT").performClick()
        commitImeText("😀𠮷")
        val altExpected = "\u001b😀\u001b𠮷".encodeToByteArray().map { "%02x".format(it.toInt() and 0xff) }
        waitUntilNoError(10_000L) { terminalEchoTail(sessionId).takeLast(altExpected.size) == altExpected }
    }

    @Test
    fun soft_ime_enter_respects_alt_modifier() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
        val sessionId = activeSessionId()
        focusTerminalInput()

        fun sendImeEnter(asKeyEvent: Boolean) {
            composeRule.runOnUiThread {
                fun findInput(view: View): View? {
                    if (view.javaClass.simpleName == "TerminalInputView") return view
                    if (view is ViewGroup) {
                        for (index in 0 until view.childCount) {
                            findInput(view.getChildAt(index))?.let { return it }
                        }
                    }
                    return null
                }
                val input = checkNotNull(findInput(composeRule.activity.window.decorView))
                val connection = input.onCreateInputConnection(EditorInfo())
                assertTrue(if (asKeyEvent) {
                    connection.sendKeyEvent(KeyEvent(KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_ENTER))
                } else {
                    connection.commitText("\n", 1)
                })
            }
        }

        composeRule.onNodeWithText("ALT").performClick()
        sendImeEnter(asKeyEvent = true)
        waitUntilNoError(10_000L) { terminalEchoTail(sessionId).takeLast(2) == listOf("1b", "0d") }

        composeRule.onNodeWithText("ALT").performClick()
        sendImeEnter(asKeyEvent = false)
        waitUntilNoError(10_000L) { terminalEchoTail(sessionId).takeLast(4) == listOf("1b", "0d", "1b", "0d") }

        sendImeEnter(asKeyEvent = true)
        waitUntilNoError(10_000L) { terminalEchoTail(sessionId).takeLast(3) == listOf("1b", "0d", "0d") }
    }

    @Test
    fun touch_navigation_long_press_sends_shift_with_active_modifiers() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
        val sessionId = activeSessionId()
        focusTerminalInput()

        val beforeShortHold = terminalEchoTail(sessionId)
        val upNode = composeRule.onNodeWithText("↑")
        upNode.performTouchInput { down(center) }
        composeRule.mainClock.advanceTimeBy(1_900L)
        upNode.performTouchInput { up() }
        waitUntilNoError(10_000L) {
            val tail = terminalEchoTail(sessionId)
            tail != beforeShortHold && tail.takeLast(3) == listOf("1b", "5b", "41")
        }

        fun holdAndAssert(label: String, modifier: Int, finalHex: String, keyParameter: String = "31") {
            val expected = listOf("1b", "5b", keyParameter, "3b", (0x30 + modifier).toString(16), finalHex)
            val node = composeRule.onNodeWithText(label)
            node.performTouchInput { down(center) }
            composeRule.mainClock.advanceTimeBy(2_100L)
            node.performTouchInput { up() }
            waitUntilNoError(10_000L) {
                terminalEchoTail(sessionId).takeLast(expected.size) == expected
            }
            assertFalse("$label hold sent more than one key", terminalEchoTail(sessionId).takeLast(expected.size * 2) == expected + expected)
        }

        holdAndAssert("↑", 2, "41")
        composeRule.onNodeWithText("ALT").performClick()
        holdAndAssert("←", 4, "44")
        composeRule.onNodeWithText("CTRL").performClick()
        composeRule.onNodeWithText("ALT").performClick()
        holdAndAssert("↓", 8, "42")
        holdAndAssert("HOME", 2, "48")
        holdAndAssert("END", 2, "46")
        holdAndAssert("PGUP", 2, "7e", "35")
        holdAndAssert("PGDN", 2, "7e", "36")
    }

    @Test
    fun fn_mode_replaces_page_keys_with_f11_and_f12() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
        val sessionId = activeSessionId()
        focusTerminalInput()

        assertTrue(hasTextNode("PGUP"))
        assertTrue(hasTextNode("PGDN"))
        composeRule.onNodeWithText("ALT").performClick()
        composeRule.onNodeWithText("ALT").performClick()
        assertTrue(hasTextNode("FN"))
        assertTrue(hasTextNode("F11"))
        assertTrue(hasTextNode("F12"))
        assertTrue(hasTextNode("SHFT"))
        assertFalse(hasTextNode("/"))
        assertFalse(hasTextNode("PGUP"))
        assertFalse(hasTextNode("PGDN"))

        composeRule.onNodeWithText("F11").performClick()
        waitUntilNoError(10_000L) {
            terminalEchoTail(sessionId).takeLast(5) == listOf("1b", "5b", "32", "33", "7e")
        }
        assertTrue(hasTextNode("FN"))
        composeRule.onNodeWithText("F12").performClick()
        waitUntilNoError(10_000L) {
            terminalEchoTail(sessionId).takeLast(5) == listOf("1b", "5b", "32", "34", "7e")
        }
        assertTrue(hasTextNode("FN"))

        composeRule.onNodeWithText("SHFT").performClick()
        sendKeyStroke(KeyEvent.KEYCODE_1)
        waitUntilNoError(10_000L) {
            terminalEchoTail(sessionId).takeLast(6) == listOf("1b", "5b", "31", "3b", "32", "50")
        }
        composeRule.onNodeWithText("F11").performClick()
        waitUntilNoError(10_000L) {
            terminalEchoTail(sessionId).takeLast(7) == listOf("1b", "5b", "32", "33", "3b", "32", "7e")
        }
        composeRule.onNodeWithText("SHFT").performClick()
        sendKeyStroke(KeyEvent.KEYCODE_2)
        waitUntilNoError(10_000L) {
            terminalEchoTail(sessionId).takeLast(3) == listOf("1b", "4f", "51")
        }
        composeRule.onNodeWithText("SHFT").performClick()
        composeRule.onNodeWithText("FN").performClick()
        assertTrue(hasTextNode("/"))
        assertFalse(hasTextNode("SHFT"))
        assertTrue(hasTextNode("PGUP"))
        assertTrue(hasTextNode("PGDN"))
        composeRule.onNodeWithText("ALT").performClick()
        composeRule.onNodeWithText("ALT").performClick()
        assertTrue(hasTextNode("SHFT"))
        sendKeyStroke(KeyEvent.KEYCODE_1)
        waitUntilNoError(10_000L) {
            terminalEchoTail(sessionId).takeLast(3) == listOf("1b", "4f", "50")
        }
    }

    @Test
    fun fn_modifier_button_cycles_ctrl_alt_and_both_without_consuming_them() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
        val sessionId = activeSessionId()
        focusTerminalInput()
        composeRule.onNodeWithText("ALT").performClick()
        composeRule.onNodeWithText("ALT").performClick()
        assertTrue(hasTextNode("FN"))
        composeRule.onNodeWithText("CTRL").assertIsEnabled()

        fun expectFunctionKey(keyCode: Int, vararg bytes: String) {
            sendKeyStroke(keyCode)
            waitUntilNoError(10_000L) { terminalEchoTail(sessionId).takeLast(bytes.size) == bytes.toList() }
            assertTrue("FN was reset by a function key", hasTextNode("FN"))
        }

        composeRule.onNodeWithText("CTRL").performClick()
        expectFunctionKey(KeyEvent.KEYCODE_1, "1b", "5b", "31", "3b", "35", "50")
        expectFunctionKey(KeyEvent.KEYCODE_2, "1b", "5b", "31", "3b", "35", "51")

        composeRule.onNodeWithText("CTRL").performClick()
        assertTrue(hasTextNode("ALT"))
        expectFunctionKey(KeyEvent.KEYCODE_1, "1b", "5b", "31", "3b", "33", "50")
        expectFunctionKey(KeyEvent.KEYCODE_2, "1b", "5b", "31", "3b", "33", "51")

        composeRule.onNodeWithText("ALT").performClick()
        assertTrue(hasTextNode("C+A"))
        expectFunctionKey(KeyEvent.KEYCODE_0, "1b", "5b", "32", "31", "3b", "37", "7e")
        composeRule.onNodeWithText("SHFT").performClick()
        composeRule.onNodeWithText("F11").performClick()
        waitUntilNoError(10_000L) {
            terminalEchoTail(sessionId).takeLast(7) == listOf("1b", "5b", "32", "33", "3b", "38", "7e")
        }

        composeRule.onNodeWithText("C+A").performClick()
        assertTrue(hasTextNode("CTRL"))
        composeRule.onNodeWithText("F12").performClick()
        waitUntilNoError(10_000L) {
            terminalEchoTail(sessionId).takeLast(7) == listOf("1b", "5b", "32", "34", "3b", "32", "7e")
        }

        composeRule.onNodeWithText("CTRL").performClick()
        composeRule.onNodeWithText("FN").performClick()
        composeRule.onNodeWithText("ALT").performClick()
        composeRule.onNodeWithText("ALT").performClick()
        assertTrue(hasTextNode("CTRL"))
        expectFunctionKey(KeyEvent.KEYCODE_1, "1b", "4f", "50")
    }

    @Test
    fun fn_selection_copies_visible_cells_and_pastes_current_clipboard_in_another_session() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
        val sourceSession = activeSessionId()
        val destinationSession = testConfig.primaryUser().sessions.firstOrNull { it != sourceSession }
            ?: throw AssertionError("selection paste test needs a second host session")
        val clipboard = composeRule.activity.getSystemService(ClipboardManager::class.java)
        val previousClip = clipboard.primaryClip
        fun selectEchoPrefix(sessionId: String) {
            waitUntilNoError(10_000L) {
                var visible = false
                composeRule.runOnIdle {
                    val view = findTerminalView() as? TerminalGridView
                    val snapshot = stateForTest().activeSnapshot
                    if (view != null && snapshot != null) {
                        visible = (view.getVisibleStartRow() until view.getVisibleEndRowExclusive()).any { row ->
                            snapshotRow(snapshot, row).contains("ECHO_${sessionId} 61")
                        }
                    }
                }
                visible
            }
            composeRule.runOnIdle {
                val view = findTerminalView() as? TerminalGridView ?: throw AssertionError("missing terminal grid")
                val snapshot = stateForTest().activeSnapshot ?: throw AssertionError("missing terminal snapshot")
                val row = (view.getVisibleStartRow() until view.getVisibleEndRowExclusive())
                    .lastOrNull { snapshotRow(snapshot, it).contains("ECHO_${sessionId} 61") }
                    ?: throw AssertionError(
                        "echo line is not visible for selection: session=$sessionId " +
                            "visible=${view.getVisibleStartRow()}-${view.getVisibleEndRowExclusive()} " +
                            "camera=${view.getCameraOffsetYForTesting()} " +
                            "matchingRows=${(0 until snapshot.rows).filter { snapshotRow(snapshot, it).contains("ECHO_${sessionId} 61") }}",
                    )
                val col = snapshotRow(snapshot, row).indexOf("ECHO_${sessionId} 61")
                val cellWidth = view.getScaledCellWidthForTesting()
                val cellHeight = view.getScaledCellHeightForTesting()
                val x1 = (col + 0.5f) * cellWidth - view.getCameraOffsetXForTesting()
                val x2 = (col + 3.5f) * cellWidth - view.getCameraOffsetXForTesting()
                val y = (row + 0.5f) * cellHeight - view.getCameraOffsetYForTesting()
                val time = SystemClock.uptimeMillis()
                view.dispatchTouchEvent(MotionEvent.obtain(time, time, MotionEvent.ACTION_DOWN, x1, y, 0))
                view.dispatchTouchEvent(MotionEvent.obtain(time, time + 16, MotionEvent.ACTION_MOVE, x2, y, 0))
                view.dispatchTouchEvent(MotionEvent.obtain(time, time + 32, MotionEvent.ACTION_UP, x2, y, 0))
                assertEquals("ECHO", view.selectedText())
            }
        }
        try {
            sendTerminalInput("a")
            waitUntilNoError(10_000L) { snapshotContainsToken("ECHO_${sourceSession} 61") }
            composeRule.onNodeWithText("ALT").performClick()
            composeRule.onNodeWithText("ALT").performClick()
            assertTrue(hasTextNode("FN"))

            selectEchoPrefix(sourceSession)
            sendKeyStroke(KeyEvent.KEYCODE_C)
            waitUntilNoError(10_000L) {
                clipboard.primaryClip?.getItemAt(0)?.coerceToText(composeRule.activity)?.toString() == "ECHO"
            }
            assertTrue("FN was reset after copy", hasTextNode("FN"))

            composeRule.onNodeWithText("FN").performClick()
            composeRule.runOnIdle {
                assertNull((findTerminalView() as TerminalGridView).selectedText())
            }
            composeRule.onNodeWithText("ALT").performClick()
            composeRule.onNodeWithText("ALT").performClick()

            selectSessionTab(destinationSession, timeoutMs = 10_000L)
            focusTerminalInput()
            assertTrue("FN was reset on session switch", hasTextNode("FN"))
            sendKeyStroke(KeyEvent.KEYCODE_V)
            waitUntilNoError(10_000L) {
                snapshotContainsSequence(*listOf("45", "43", "48", "4f").map { "ECHO_${destinationSession} $it" }.toTypedArray())
            }

            clipboard.setPrimaryClip(ClipData.newPlainText("external", "NOW"))
            sendKeyStroke(KeyEvent.KEYCODE_V)
            waitUntilNoError(10_000L) {
                snapshotContainsSequence(*listOf("4e", "4f", "57").map { "ECHO_${destinationSession} $it" }.toTypedArray())
            }

            sendKeyStroke(KeyEvent.KEYCODE_A)
            waitUntilNoError(10_000L) { snapshotContainsToken("ECHO_${destinationSession} 61") }
            selectEchoPrefix(destinationSession)
            sendKeyStroke(KeyEvent.KEYCODE_C)
            waitUntilNoError(10_000L) {
                clipboard.primaryClip?.getItemAt(0)?.coerceToText(composeRule.activity)?.toString() == "ECHO"
            }
        } finally {
            if (previousClip != null) clipboard.setPrimaryClip(previousClip) else clipboard.clearPrimaryClip()
        }
    }

    @Test
    fun pinch_zoom_adjusts_view_and_resets() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
        resetZoomPan()
        val baseline = waitForTerminalDebugInfo { info ->
            info.viewCols > 0 && info.viewRows > 0 && info.renderScaleX > 0f
        }
        val initial = baseline
            ?: throw AssertionError("missing terminal debug info")

        performPinchZoom(zoomIn = true)
        val zoomedIn = waitForTerminalDebugInfo(SHORT_UI_TIMEOUT_MS) { info ->
            info.renderScaleX > initial.renderScaleX + 0.05f
        } ?: throw AssertionError("missing zoomed-in terminal debug info")
        assertTrue(zoomedIn.renderScaleX > initial.renderScaleX)

        resetZoomPan()
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) {
            val info = readTerminalDebugInfo()
            info != null &&
                kotlin.math.abs(info.renderScaleX - initial.renderScaleX) <= 0.03f
        }
    }

    @Test
    fun zoomed_viewport_does_not_reset_after_frame_and_resume() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
        val original = readTerminalDebugInfo()
            ?: throw AssertionError("missing original terminal debug info")
        val originalSessionId = original.activeSessionId
        val tallSessionId = startHostsViaHarness(count = 1, cols = 110, rows = 50).single()
        try {
            selectSessionForFixture(tallSessionId, expectedCols = 110, expectedRows = 50)
            resetZoomPan()
            val baseline = waitForTerminalDebugInfo { info ->
                info.activeSessionId == tallSessionId &&
                    info.cols == 110 &&
                    info.rows == 50 &&
                    info.viewCols > 0 &&
                    info.viewRows > 0 &&
                    info.renderScaleX > 0f
            } ?: throw AssertionError("missing baseline terminal debug info")

            var zoomed = baseline
            repeat(2) {
                setZoomFactor(zoomed.zoomFactor + 0.35f)
                zoomed = waitForTerminalDebugInfo(SHORT_UI_TIMEOUT_MS) { info ->
                    info.renderScaleX > zoomed.renderScaleX + 0.03f &&
                        info.viewCols > 0 &&
                        info.viewRows > 0
                } ?: throw AssertionError("missing zoomed terminal debug info")
            }

            sendTerminalInput("echo ZOOM-STABILITY")
            sendTerminalEnter()
            val afterFrame = waitForTerminalDebugInfo(timeoutMs = 20_000L) { info ->
                info.lastFrameSeq > zoomed.lastFrameSeq &&
                    kotlin.math.abs(info.renderScaleX - zoomed.renderScaleX) <= 0.03f &&
                    kotlin.math.abs(info.viewCols - zoomed.viewCols) <= 2
            } ?: throw AssertionError("zoomed viewport changed after a new terminal frame")

            InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(KeyEvent.KEYCODE_BACK)
            waitUntilNoError(10_000L) { !hasTextNode("CTRL") }
            val beforeBackground = waitForTerminalDebugInfo(timeoutMs = SHORT_UI_TIMEOUT_MS) { info ->
                info.activeSessionId == afterFrame.activeSessionId &&
                    kotlin.math.abs(info.zoomFactor - afterFrame.zoomFactor) <= 0.03f &&
                    info.viewCols > 0 &&
                    info.viewRows > 0 &&
                    info.renderScaleX > 0f
            } ?: throw AssertionError("missing keyboard-hidden zoomed terminal debug info")

            backgroundAndResumeActivity()
            waitForTagNoError(TestTags.TerminalInput, timeoutMs = SHORT_UI_TIMEOUT_MS)
            waitForTerminalDebugInfo(timeoutMs = SHORT_UI_TIMEOUT_MS) { info ->
                info.activeSessionId == beforeBackground.activeSessionId &&
                    kotlin.math.abs(info.zoomFactor - beforeBackground.zoomFactor) <= 0.03f &&
                    kotlin.math.abs(info.renderScaleX - beforeBackground.renderScaleX) <= 0.03f &&
                    kotlin.math.abs(info.viewCols - beforeBackground.viewCols) <= 2
            } ?: throw AssertionError("zoomed viewport reset after background/foreground cycle")
        } finally {
            cleanupFixtureSessions(originalSessionId, original.cols, original.rows, listOf(tallSessionId))
        }
    }

    @Test
    fun zoomed_scrollback_live_reentry_waits_for_matching_snapshot() {
        lateinit var view: TerminalGridView
        var scrollbackDelta = 0
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                setOnScrollback { scrollbackDelta += it }
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(480))
                layout(0, 0, 480, 480)
                val live = terminalSnapshotForViewTest(rows = 30, cols = 20)
                update(
                    snapshot = buildScrollbackSnapshot(
                        live = live,
                        scrollback = scrollbackRowsForViewTest(rows = 4, cols = 20),
                        offset = 4,
                    ),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 30,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom + 0.8f,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 4,
                    imeVisible = false,
                    isLoading = false,
                    localInputNonce = 7,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            view.restoreViewportState(
                TerminalViewportState(
                    cameraOffsetXPx = 0f,
                    preferredCameraOffsetXPx = 0f,
                    cameraOffsetYPx = cellHeight * 3.5f,
                    scrollRemainderY = 0f,
                    viewportHeightPx = view.height,
                    scaledCellHeightPx = cellHeight,
                    totalRows = 30,
                ),
            )
            val beforeReentry = view.getCameraOffsetYForTesting()

            dispatchSinglePointerDrag(view, startX = 120f, startY = 120f, endX = 119f, endY = 120f)

            assertTrue("expected pan to request live reentry rows", scrollbackDelta < 0)
            assertEquals(
                "camera moved before the matching scrollback snapshot arrived",
                beforeReentry,
                view.getCameraOffsetYForTesting(),
                0.01f,
            )

            val live = terminalSnapshotForViewTest(rows = 30, cols = 20)
            view.update(
                snapshot = buildScrollbackSnapshot(
                    live = live,
                    scrollback = scrollbackRowsForViewTest(rows = 4, cols = 20),
                    offset = 4 + scrollbackDelta,
                ),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 20,
                hostRows = 30,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom + 0.8f,
                panResetNonce = 0,
                scrollbackOffsetRows = 4 + scrollbackDelta,
                imeVisible = false,
                isLoading = false,
            )
            assertEquals(
                beforeReentry + (scrollbackDelta * cellHeight),
                view.getCameraOffsetYForTesting(),
                0.01f,
            )
        }
        composeRule.waitForIdle()
    }

    @Test
    fun keyboard_height_change_bottom_aligns_live_view_when_cursor_still_fits() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(1600))
                layout(0, 0, 480, 1600)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 30, cols = 20, cursorY = 18),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 30,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                    localInputNonce = 7,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            view.restoreViewportState(
                TerminalViewportState(
                    cameraOffsetXPx = 0f,
                    preferredCameraOffsetXPx = 0f,
                    cameraOffsetYPx = 0f,
                    scrollRemainderY = 0f,
                    viewportHeightPx = view.height,
                    scaledCellHeightPx = cellHeight,
                    totalRows = 30,
                    mode = systems.pkt.lingon.terminal.TerminalViewportMode.LiveBottom,
                ),
            )
            assertEquals(0f, view.getCameraOffsetYForTesting(), 0.01f)

            val keyboardHeight = (cellHeight * 20f).toInt().coerceAtLeast(1)
            view.measure(exactlyMeasureSpec(480), exactlyMeasureSpec(keyboardHeight))
            view.layout(0, 0, 480, keyboardHeight)
            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 30, cols = 20, cursorY = 18),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 20,
                hostRows = 30,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = true,
                isLoading = false,
            )
            view.draw(Canvas(Bitmap.createBitmap(480, keyboardHeight, Bitmap.Config.ARGB_8888)))

            assertEquals(
                "keyboard shrink in live mode should align to the terminal bottom even when the cursor still fits",
                (30 * cellHeight) - keyboardHeight.toFloat(),
                view.getCameraOffsetYForTesting(),
                0.01f,
            )
            assertEquals(10, view.getVisibleStartRow())
        }
        composeRule.waitForIdle()
    }

    @Test
    fun terminal_resize_invalidates_after_live_bottom_reanchor() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(1600))
                layout(0, 0, 480, 1600)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 60, cols = 20, cursorY = 59),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 60,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                    localInputNonce = 7,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            val sizeChangeInvalidations = view.getSizeChangeInvalidateCountForTesting()

            val keyboardHeight = (cellHeight * 24f).toInt().coerceAtLeast(1)
            view.measure(exactlyMeasureSpec(480), exactlyMeasureSpec(keyboardHeight))
            view.layout(0, 0, 480, keyboardHeight)

            assertEquals(
                "terminal resize should bottom-align live content immediately",
                60,
                view.getVisibleEndRowExclusive(),
            )
            assertTrue(
                "terminal resize must invalidate so the first frame after IME/session layout cannot show stale pixels",
                view.getSizeChangeInvalidateCountForTesting() > sizeChangeInvalidations,
            )
        }
        composeRule.waitForIdle()
    }

    @Test
    fun loading_terminal_content_initializes_at_live_bottom_even_when_cursor_is_above_bottom() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(480))
                layout(0, 0, 480, 480)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 12),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 80,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom + 2.2f,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = true,
                    localInputNonce = 7,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            val expected = maxOf(0f, (80 * cellHeight) - view.getViewportHeightForTesting().toFloat())
            assertEquals(
                "initial loading content must bottom-align immediately instead of rendering from an arbitrary camera",
                expected,
                view.getCameraOffsetYForTesting(),
                maxOf(1f, cellHeight * 0.1f),
            )
            assertEquals(
                "loading terminal should expose the bottom row on the first content frame",
                80,
                view.getVisibleEndRowExclusive(),
            )
        }
        composeRule.waitForIdle()
    }

    @Test
    fun terminal_view_reanchors_when_soft_keyboard_overlays_physical_view() {
        lateinit var terminalView: TerminalGridView
        lateinit var input: EditText
        composeRule.runOnUiThread {
            composeRule.activity.window.setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_ADJUST_NOTHING)
            terminalView = TerminalGridView(composeRule.activity)
            input = EditText(composeRule.activity).apply {
                isFocusable = true
                isFocusableInTouchMode = true
                alpha = 0f
            }
            val root = FrameLayout(composeRule.activity).apply {
                addView(
                    terminalView,
                    FrameLayout.LayoutParams(
                        FrameLayout.LayoutParams.MATCH_PARENT,
                        FrameLayout.LayoutParams.MATCH_PARENT,
                    ),
                )
                addView(input, FrameLayout.LayoutParams(1, 1))
            }
            composeRule.activity.setContentView(root)
        }
        waitForViewLayout(terminalView)

        composeRule.runOnUiThread {
            terminalView.update(
                snapshot = terminalSnapshotForViewTest(rows = 240, cols = 80, cursorY = 239),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 1,
                hostCols = 80,
                hostRows = 240,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
            )
            terminalView.draw(Canvas(Bitmap.createBitmap(terminalView.width, terminalView.height, Bitmap.Config.ARGB_8888)))
        }

        showKeyboard(input)
        waitForImeOverlay(terminalView)
        composeRule.runOnUiThread {
            terminalView.requestApplyInsets()
            terminalView.update(
                snapshot = terminalSnapshotForViewTest(rows = 240, cols = 80, cursorY = 239),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 80,
                hostRows = 240,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = true,
                isLoading = false,
            )
            terminalView.draw(Canvas(Bitmap.createBitmap(terminalView.width, terminalView.height, Bitmap.Config.ARGB_8888)))
            assertOverlayCameraBottomAnchored("overlay keyboard visible", terminalView)
        }
        assertOverlayTerminalViewportDoesNotFlicker("overlay keyboard visible", terminalView)
        val visibleViewportHeight = terminalView.getViewportHeightForTesting()

        hideKeyboard(input)
        waitForImeHidden()
        composeRule.runOnUiThread {
            terminalView.requestApplyInsets()
            terminalView.update(
                snapshot = terminalSnapshotForViewTest(rows = 240, cols = 80, cursorY = 239),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 3,
                hostCols = 80,
                hostRows = 240,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
            )
            terminalView.draw(Canvas(Bitmap.createBitmap(terminalView.width, terminalView.height, Bitmap.Config.ARGB_8888)))
            assertOverlayCameraBottomAnchored("overlay keyboard hidden", terminalView)
        }
        assertOverlayCameraUsesVisibleSystemViewport("overlay keyboard hidden", terminalView)
        assertOverlayTerminalViewportDoesNotFlicker("overlay keyboard hidden", terminalView)

        showKeyboard(input)
        waitForImeOverlay(terminalView)
        composeRule.runOnUiThread {
            terminalView.requestApplyInsets()
            terminalView.update(
                snapshot = terminalSnapshotForViewTest(rows = 240, cols = 80, cursorY = 239),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 4,
                hostCols = 80,
                hostRows = 240,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = true,
                isLoading = false,
            )
            terminalView.draw(Canvas(Bitmap.createBitmap(terminalView.width, terminalView.height, Bitmap.Config.ARGB_8888)))
            assertTrue(
                "showing IME again should shrink the effective viewport without requiring a pan",
                terminalView.getViewportHeightForTesting() <= visibleViewportHeight + 1,
            )
            assertOverlayCameraBottomAnchored("overlay keyboard visible again", terminalView)
        }
        assertOverlayTerminalViewportDoesNotFlicker("overlay keyboard visible again", terminalView)
    }

    @Test
    fun terminal_cursor_follow_defaults_to_keyboard_input_only() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(480))
                layout(0, 0, 480, 480)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 10),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 80,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                    localInputNonce = 7,
                )
                restoreViewportState(
                    TerminalViewportState(
                        cameraOffsetXPx = 0f,
                        preferredCameraOffsetXPx = 0f,
                        cameraOffsetYPx = 0f,
                        scrollRemainderY = 0f,
                        viewportHeightPx = height,
                        scaledCellHeightPx = getScaledCellHeightForTesting(),
                        totalRows = 80,
                    ),
                )
                draw(Canvas(Bitmap.createBitmap(480, 480, Bitmap.Config.ARGB_8888)))
            }
        }

        composeRule.runOnUiThread {
            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 70),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 20,
                hostRows = 80,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
                followOnReadEnabled = false,
                localInputNonce = 7,
            )
            view.draw(Canvas(Bitmap.createBitmap(480, 480, Bitmap.Config.ARGB_8888)))
            assertEquals(
                "read-driven cursor movement must not move the camera when follow-on-read is disabled",
                0f,
                view.getCameraOffsetYForTesting(),
                0.01f,
            )

            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 79),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 3,
                hostCols = 20,
                hostRows = 80,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
                followOnReadEnabled = false,
                localInputNonce = 8,
            )
            view.draw(Canvas(Bitmap.createBitmap(480, 480, Bitmap.Config.ARGB_8888)))
            assertTrue(
                "keyboard-associated cursor movement should move the camera when follow-on-read is disabled",
                view.getCameraOffsetYForTesting() > 0f,
            )
        }
    }

    @Test
    fun terminal_keyboard_enter_follow_waits_for_cursor_movement_before_consuming_input() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(320), exactlyMeasureSpec(360))
                layout(0, 0, 320, 360)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 40, cols = 120, cursorY = 38, cursorX = 68),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 120,
                    hostRows = 40,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom + 1.6f,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                    followOnReadEnabled = false,
                    localInputNonce = 1,
                )
                draw(Canvas(Bitmap.createBitmap(320, 360, Bitmap.Config.ARGB_8888)))
            }
        }

        composeRule.runOnUiThread {
            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 40, cols = 120, cursorY = 38, cursorX = 69),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 120,
                hostRows = 40,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom + 1.6f,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
                followOnReadEnabled = false,
                localInputNonce = 2,
            )
            view.draw(Canvas(Bitmap.createBitmap(320, 360, Bitmap.Config.ARGB_8888)))
            assertTrue(
                "typing a long command should pan right before Enter",
                view.getCameraOffsetXForTesting() > view.getPreferredCameraOffsetXForTesting(),
            )
            val pannedRight = view.getCameraOffsetXForTesting()

            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 40, cols = 120, cursorY = 38, cursorX = 69),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 3,
                hostCols = 120,
                hostRows = 40,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom + 1.6f,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
                followOnReadEnabled = false,
                localInputNonce = 3,
            )
            view.draw(Canvas(Bitmap.createBitmap(320, 360, Bitmap.Config.ARGB_8888)))
            assertEquals(
                "a redraw before terminal echo must not consume Enter follow or move the existing camera",
                pannedRight,
                view.getCameraOffsetXForTesting(),
                0.01f,
            )

            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 40, cols = 120, cursorY = 39, cursorX = 0),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 4,
                hostCols = 120,
                hostRows = 40,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom + 1.6f,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
                followOnReadEnabled = false,
                localInputNonce = 3,
            )
            view.draw(Canvas(Bitmap.createBitmap(320, 360, Bitmap.Config.ARGB_8888)))

            assertEquals(
                "Enter-associated cursor movement should restore the preferred horizontal camera when the cursor fits there",
                view.getPreferredCameraOffsetXForTesting(),
                view.getCameraOffsetXForTesting(),
                0.01f,
            )
        }
    }

    @Test
    fun terminal_default_zoom_drag_pans_wide_live_content() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(320), exactlyMeasureSpec(360))
                layout(0, 0, 320, 360)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 40, cols = 120, cursorY = 39, cursorX = 0),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 120,
                    hostRows = 40,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                )
                draw(Canvas(Bitmap.createBitmap(320, 360, Bitmap.Config.ARGB_8888)))
            }
        }

        composeRule.runOnUiThread {
            assertEquals(0f, view.getCameraOffsetXForTesting(), 0.01f)
            dispatchSinglePointerDrag(view, startX = 220f, startY = 180f, endX = 80f, endY = 180f)
            assertTrue(
                "default/full-zoom terminal should allow horizontal panning when live content is wider than the viewport",
                view.getCameraOffsetXForTesting() > 0f,
            )
        }
    }

    @Test
    fun terminal_default_zoom_drag_can_enter_scrollback_from_live_edge() {
        lateinit var view: TerminalGridView
        var scrollbackDelta = 0
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                setOnScrollback { scrollbackDelta += it }
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(720))
                layout(0, 0, 480, 720)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 8, cols = 20, cursorY = 7, cursorX = 0),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 8,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                )
                draw(Canvas(Bitmap.createBitmap(480, 720, Bitmap.Config.ARGB_8888)))
            }
        }

        composeRule.runOnUiThread {
            dispatchSinglePointerDrag(view, startX = 240f, startY = 180f, endX = 240f, endY = 420f)
            assertTrue(
                "default/full-zoom terminal should request scrollback when dragging down from the live top edge",
                scrollbackDelta > 0,
            )
        }
    }

    @Test
    fun terminal_follow_on_read_toggle_preserves_existing_cursor_follow() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(480))
                layout(0, 0, 480, 480)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 10),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 80,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                    followOnReadEnabled = true,
                )
                restoreViewportState(
                    TerminalViewportState(
                        cameraOffsetXPx = 0f,
                        preferredCameraOffsetXPx = 0f,
                        cameraOffsetYPx = 0f,
                        scrollRemainderY = 0f,
                        viewportHeightPx = height,
                        scaledCellHeightPx = getScaledCellHeightForTesting(),
                        totalRows = 80,
                    ),
                )
                draw(Canvas(Bitmap.createBitmap(480, 480, Bitmap.Config.ARGB_8888)))
            }
        }

        composeRule.runOnUiThread {
            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 79),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 20,
                hostRows = 80,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
                followOnReadEnabled = true,
                localInputNonce = 0,
            )
            view.draw(Canvas(Bitmap.createBitmap(480, 480, Bitmap.Config.ARGB_8888)))
            assertTrue(
                "follow-on-read enabled should keep existing read-driven cursor-follow behavior",
                view.getCameraOffsetYForTesting() > 0f,
            )
        }
    }

    @Test
    fun disabling_follow_on_read_stops_passive_cursor_follow() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(480))
                layout(0, 0, 480, 480)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 10),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 80,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                    followOnReadEnabled = true,
                )
                draw(Canvas(Bitmap.createBitmap(480, 480, Bitmap.Config.ARGB_8888)))
            }
        }

        composeRule.runOnUiThread {
            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 79),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 20,
                hostRows = 80,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
                followOnReadEnabled = true,
            )
            view.draw(Canvas(Bitmap.createBitmap(480, 480, Bitmap.Config.ARGB_8888)))
            val followedCameraY = view.getCameraOffsetYForTesting()
            assertTrue("fixture should enter read-driven cursor-follow", followedCameraY > 0f)

            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 79),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 20,
                hostRows = 80,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
                followOnReadEnabled = false,
            )
            view.draw(Canvas(Bitmap.createBitmap(480, 480, Bitmap.Config.ARGB_8888)))
            val disabledCameraY = view.getCameraOffsetYForTesting()

            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 10),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 3,
                hostCols = 20,
                hostRows = 80,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
                followOnReadEnabled = false,
            )
            view.draw(Canvas(Bitmap.createBitmap(480, 480, Bitmap.Config.ARGB_8888)))

            assertEquals(
                "passive reads must not keep cursor-following after follow-on-read is disabled",
                disabledCameraY,
                view.getCameraOffsetYForTesting(),
                0.01f,
            )
        }
    }

    @Test
    fun lifecycle_viewport_restore_preserves_saved_camera_without_new_frame() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(480))
                layout(0, 0, 480, 480)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 40, cols = 20, cursorY = 35),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 40,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            view.restoreViewportState(
                TerminalViewportState(
                    cameraOffsetXPx = 0f,
                    preferredCameraOffsetXPx = 0f,
                    cameraOffsetYPx = 0f,
                    scrollRemainderY = 0f,
                    viewportHeightPx = view.height,
                    scaledCellHeightPx = cellHeight,
                    totalRows = 40,
                ),
            )
            view.draw(Canvas(Bitmap.createBitmap(480, 480, Bitmap.Config.ARGB_8888)))

            assertEquals(
                "lifecycle restore should preserve the saved camera when no new frame arrived",
                0f,
                view.getCameraOffsetYForTesting(),
                0.01f,
            )
            assertEquals(0, view.getVisibleStartRow())
        }
        composeRule.waitForIdle()
    }

    @Test
    fun app_lock_unlock_preserves_terminal_camera_viewport() {
        val sessionId = "lock-viewport"
        val snapshot = terminalSnapshotForViewTest(rows = 120, cols = 40, cursorY = 110)
        val locked = mutableStateOf(false)
        val viewportCache = mutableStateMapOf<String, TerminalViewportState>()
        val baseState = UiState(
            endpoint = "https://relay.example/v1",
            username = "alice",
            loggedIn = true,
            activeSessionId = sessionId,
            activeSnapshot = snapshot,
            connectionState = ConnectionState.Connected,
            hasControl = true,
            terminalCols = 40,
            terminalRows = 120,
            lastFrameSeq = 42L,
            lastFrameType = "snapshot",
            appLockTimeoutMinutes = 3,
            zoomFactor = DefaultTerminalZoom + 0.5f,
            restoreTerminalImeOnLifecycleStart = false,
        )
        val viewModel = appViewModel()
        composeRule.runOnUiThread {
            composeRule.activity.setContent {
                LingonAppContent(
                    state = baseState.copy(requiresAppUnlock = locked.value),
                    viewModel = viewModel,
                    viewportCache = viewportCache,
                )
            }
        }
        composeRule.waitForIdle()
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) {
            val view = findTerminalView() as? TerminalGridView
            view != null &&
                view.width > 0 &&
                view.height > 0 &&
                view.getScaledCellHeightForTesting() > 0f &&
                view.getVisibleEndRowExclusive() > 0
        }

        composeRule.runOnIdle {
            val view = findTerminalView() as? TerminalGridView
                ?: throw AssertionError("missing terminal view before app lock")
            val cellHeight = view.getScaledCellHeightForTesting()
            val savedCamera = cellHeight * 10f
            view.restoreViewportState(
                TerminalViewportState(
                    cameraOffsetXPx = 0f,
                    preferredCameraOffsetXPx = 0f,
                    cameraOffsetYPx = savedCamera,
                    scrollRemainderY = 0f,
                    viewportHeightPx = view.getViewportHeightForTesting(),
                    scaledCellHeightPx = cellHeight,
                    totalRows = snapshot.rows,
                ),
            )
            view.draw(Canvas(Bitmap.createBitmap(view.width, view.height, Bitmap.Config.ARGB_8888)))
            assertEquals(
                "fixture should place a manual camera before app lock",
                10,
                view.getVisibleStartRow(),
            )
        }
        val before = terminalViewCameraSnapshot("before app lock")

        composeRule.runOnIdle { locked.value = true }
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) { hasTextNode("App locked") }
        composeRule.runOnIdle {
            assertTrue(
                "app lock disposal should capture the terminal viewport",
                viewportCache.isNotEmpty(),
            )
            assertEquals("app lock should cache one scoped viewport", 1, viewportCache.size)
            assertEquals(
                "app lock disposal should capture the pre-lock camera offset",
                before.cameraOffsetYPx,
                viewportCache.values.single().cameraOffsetYPx,
                maxOf(1f, before.scaledCellHeightPx * 0.1f),
            )
        }

        composeRule.runOnIdle { locked.value = false }
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) {
            val view = findTerminalView() as? TerminalGridView
            view != null && view.width > 0 && view.height > 0 && view.getScaledCellHeightForTesting() > 0f
        }
        val restored = terminalViewCameraSnapshot("after app unlock")

        assertEquals(
            "unlock should preserve the saved visible start row instead of cursor-following to the bottom",
            before.visibleStartRow,
            restored.visibleStartRow,
        )
        assertEquals(
            "unlock should preserve the saved camera offset instead of cursor-following to the bottom",
            before.cameraOffsetYPx,
            restored.cameraOffsetYPx,
            maxOf(1f, before.scaledCellHeightPx * 0.1f),
        )
    }

    @Test
    fun logout_clears_viewport_cache_and_reused_session_id_does_not_restore_stale_camera() {
        val sessionId = "host-1"
        val firstSnapshot = terminalSnapshotForViewTest(rows = 120, cols = 40, cursorY = 110)
        val secondSnapshot = terminalSnapshotForViewTest(rows = 120, cols = 40, cursorY = 110)
        val state = mutableStateOf(
            UiState(
                endpoint = "https://relay-a.example/v1",
                username = "alice",
                loggedIn = true,
                activeSessionId = sessionId,
                activeSnapshot = firstSnapshot,
                connectionState = ConnectionState.Connected,
                hasControl = true,
                terminalCols = 40,
                terminalRows = 120,
                lastFrameSeq = 100L,
                lastFrameType = "snapshot",
                zoomFactor = DefaultTerminalZoom + 0.5f,
                restoreTerminalImeOnLifecycleStart = false,
            ),
        )
        val viewportCache = mutableStateMapOf<String, TerminalViewportState>()
        val viewModel = appViewModel()
        composeRule.runOnUiThread {
            composeRule.activity.setContent {
                LingonAppContent(
                    state = state.value,
                    viewModel = viewModel,
                    viewportCache = viewportCache,
                )
            }
        }
        waitForTerminalViewReady()

        composeRule.runOnIdle {
            val view = findTerminalView() as? TerminalGridView
                ?: throw AssertionError("missing first identity terminal view")
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            view.restoreViewportState(
                TerminalViewportState(
                    cameraOffsetXPx = 0f,
                    preferredCameraOffsetXPx = 0f,
                    cameraOffsetYPx = cellHeight * 10f,
                    scrollRemainderY = 0f,
                    viewportHeightPx = view.getViewportHeightForTesting(),
                    scaledCellHeightPx = cellHeight,
                    totalRows = firstSnapshot.rows,
                ),
            )
            view.draw(Canvas(Bitmap.createBitmap(view.width, view.height, Bitmap.Config.ARGB_8888)))
            assertEquals("fixture should place first identity camera at row 10", 10, view.getVisibleStartRow())
        }
        val staleCamera = terminalViewCameraSnapshot("before logout")

        composeRule.runOnIdle {
            state.value = UiState(
                endpoint = "https://relay-a.example/v1",
                username = null,
                loggedIn = false,
                restoreTerminalImeOnLifecycleStart = false,
            )
        }
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) {
            findTerminalView() == null && viewportCache.isEmpty()
        }

        composeRule.runOnIdle {
            state.value = UiState(
                endpoint = "https://relay-b.example/v1",
                username = "bob",
                loggedIn = true,
                activeSessionId = sessionId,
                activeSnapshot = secondSnapshot,
                connectionState = ConnectionState.Connected,
                hasControl = true,
                terminalCols = 40,
                terminalRows = 120,
                lastFrameSeq = 200L,
                lastFrameType = "snapshot",
                zoomFactor = DefaultTerminalZoom + 0.5f,
                restoreTerminalImeOnLifecycleStart = false,
            )
        }
        waitForTerminalViewReady()
        val afterRelogin = terminalViewCameraSnapshot("after relogin with reused session id")

        assertFalse(
            "a reused session id after logout must not restore the previous identity's camera row",
            afterRelogin.visibleStartRow == staleCamera.visibleStartRow,
        )
        assertFalse(
            "a reused session id after logout must not restore the previous identity's camera offset",
            kotlin.math.abs(afterRelogin.cameraOffsetYPx - staleCamera.cameraOffsetYPx) <=
                maxOf(1f, staleCamera.scaledCellHeightPx * 0.1f),
        )
    }

    @Test
    fun lifecycle_viewport_restore_survives_ime_height_bounce_without_new_frame() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(640))
                layout(0, 0, 480, 640)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 18),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 80,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = true,
                    isLoading = false,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            val saved = TerminalViewportState(
                cameraOffsetXPx = 0f,
                preferredCameraOffsetXPx = 0f,
                cameraOffsetYPx = 0f,
                scrollRemainderY = 0f,
                viewportHeightPx = view.height,
                scaledCellHeightPx = cellHeight,
                totalRows = 80,
            )
            view.restoreViewportState(saved)
            assertEquals(0f, view.getCameraOffsetYForTesting(), 0.01f)

            view.measure(exactlyMeasureSpec(480), exactlyMeasureSpec(1200))
            view.layout(0, 0, 480, 1200)
            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 18),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 1,
                hostCols = 20,
                hostRows = 80,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
            )
            assertEquals(
                "transient hidden IME height must not move a restored lifecycle camera without a new frame",
                0f,
                view.getCameraOffsetYForTesting(),
                0.01f,
            )

            view.measure(exactlyMeasureSpec(480), exactlyMeasureSpec(640))
            view.layout(0, 0, 480, 640)
            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 18),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 1,
                hostCols = 20,
                hostRows = 80,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = true,
                isLoading = false,
            )
            assertEquals(
                "restored lifecycle camera should survive the IME height returning without a new frame",
                0f,
                view.getCameraOffsetYForTesting(),
                0.01f,
            )
        }
        composeRule.waitForIdle()
    }

    @Test
    fun lifecycle_viewport_restore_keeps_horizontal_preference_separate_from_temporary_camera() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(200), exactlyMeasureSpec(240))
                layout(0, 0, 200, 240)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 20, cols = 80, cursorX = 4),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 80,
                    hostRows = 20,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            view.restoreViewportState(
                TerminalViewportState(
                    cameraOffsetXPx = 80f,
                    preferredCameraOffsetXPx = 0f,
                    cameraOffsetYPx = 0f,
                    scrollRemainderY = 0f,
                    viewportHeightPx = view.height,
                    scaledCellHeightPx = cellHeight,
                    totalRows = 20,
                ),
            )

            assertEquals(
                "restore should apply the saved camera for the current frame",
                80f,
                view.getCameraOffsetXForTesting(),
                0.01f,
            )
            assertEquals(
                "restore must not promote a temporary cursor-follow camera to the preferred horizontal offset",
                0f,
                view.getPreferredCameraOffsetXForTesting(),
                0.01f,
            )

            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 20, cols = 80, cursorX = 5),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 80,
                hostRows = 20,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
                localInputNonce = 1,
            )
            view.draw(Canvas(Bitmap.createBitmap(200, 240, Bitmap.Config.ARGB_8888)))

            assertEquals(
                "later cursor-follow should return to the saved horizontal preference when the cursor still fits",
                0f,
                view.getCameraOffsetXForTesting(),
                0.01f,
            )
        }
        composeRule.waitForIdle()
    }

    @Test
    fun lifecycle_viewport_capture_keeps_horizontal_preference_separate_from_temporary_camera() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(200), exactlyMeasureSpec(240))
                layout(0, 0, 200, 240)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 20, cols = 80, cursorX = 4),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 80,
                    hostRows = 20,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            view.restoreViewportState(
                TerminalViewportState(
                    cameraOffsetXPx = 40f,
                    preferredCameraOffsetXPx = 40f,
                    cameraOffsetYPx = 0f,
                    scrollRemainderY = 0f,
                    viewportHeightPx = view.height,
                    scaledCellHeightPx = cellHeight,
                    totalRows = 20,
                ),
            )
            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 20, cols = 80, cursorX = 30),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 80,
                hostRows = 20,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
                localInputNonce = 1,
            )
            view.draw(Canvas(Bitmap.createBitmap(200, 240, Bitmap.Config.ARGB_8888)))

            val saved = view.captureViewportState()
            assertTrue(
                "fixture should create a temporary cursor-follow camera to the right of the saved preference",
                saved.cameraOffsetXPx > saved.preferredCameraOffsetXPx,
            )
            assertEquals(
                "capture must preserve the user's horizontal preference separately from cursor-follow camera",
                40f,
                saved.preferredCameraOffsetXPx,
                0.01f,
            )
        }
        composeRule.waitForIdle()
    }

    @Test
    fun lifecycle_viewport_restore_preserves_live_bottom_across_height_change() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(480))
                layout(0, 0, 480, 480)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 60, cols = 20, cursorY = 59),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 60,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            view.draw(Canvas(Bitmap.createBitmap(480, 480, Bitmap.Config.ARGB_8888)))
            val saved = view.captureViewportState()
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            assertEquals(
                "fixture should save a live-bottom viewport",
                (60 * cellHeight) - 480f,
                saved.cameraOffsetYPx,
                0.01f,
            )

            view.measure(exactlyMeasureSpec(480), exactlyMeasureSpec(240))
            view.layout(0, 0, 480, 240)
            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 60, cols = 20, cursorY = 59),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 1,
                hostCols = 20,
                hostRows = 60,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = true,
                isLoading = false,
            )
            view.restoreViewportState(saved)

            assertEquals(
                "restoring a saved live-bottom viewport after IME shrink should preserve the live bottom",
                (60 * cellHeight) - 240f,
                view.getCameraOffsetYForTesting(),
                0.01f,
            )
        }
        composeRule.waitForIdle()
    }

    @Test
    fun lifecycle_viewport_restore_preserves_manual_bottom_anchor_across_height_change() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(480))
                layout(0, 0, 480, 480)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 60, cols = 20, cursorY = 20),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 60,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom + 0.8f,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            view.draw(Canvas(Bitmap.createBitmap(480, 480, Bitmap.Config.ARGB_8888)))
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            val savedCamera = cellHeight * 10f
            val saved = TerminalViewportState(
                cameraOffsetXPx = 0f,
                preferredCameraOffsetXPx = 0f,
                cameraOffsetYPx = savedCamera,
                scrollRemainderY = 0f,
                viewportHeightPx = view.height,
                scaledCellHeightPx = cellHeight,
                totalRows = 60,
            )

            view.measure(exactlyMeasureSpec(480), exactlyMeasureSpec(240))
            view.layout(0, 0, 480, 240)
            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 60, cols = 20, cursorY = 20),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 1,
                hostCols = 20,
                hostRows = 60,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom + 0.8f,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = true,
                isLoading = false,
            )
            view.restoreViewportState(saved)

            assertEquals(
                "restoring a saved manual viewport after IME shrink should preserve the visible content bottom edge",
                savedCamera + 240f,
                view.getCameraOffsetYForTesting(),
                0.01f,
            )
        }
        composeRule.waitForIdle()
    }

    @Test
    fun lifecycle_viewport_restore_preserves_live_bottom_when_rows_advance() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(480))
                layout(0, 0, 480, 480)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 60, cols = 20, cursorY = 59),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 60,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            view.draw(Canvas(Bitmap.createBitmap(480, 480, Bitmap.Config.ARGB_8888)))
            val saved = view.captureViewportState()
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            assertEquals(
                "fixture should save the 60-row live bottom",
                (60 * cellHeight) - 480f,
                saved.cameraOffsetYPx,
                0.01f,
            )

            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 61, cols = 20, cursorY = 60),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 20,
                hostRows = 61,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
            )
            view.restoreViewportState(saved)

            assertEquals(
                "restoring a saved live-bottom viewport after output advances should follow the new live bottom",
                (61 * cellHeight) - 480f,
                view.getCameraOffsetYForTesting(),
                0.01f,
            )
        }
        composeRule.waitForIdle()
    }

    @Test
    fun lifecycle_capture_does_not_persist_transient_keyboard_cursor_follow() {
        lateinit var view: TerminalGridView
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(480))
                layout(0, 0, 480, 480)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 70),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 80,
                    fitToViewWidth = false,
                    zoomFactor = DefaultTerminalZoom + 2.2f,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = true,
                    followOnReadEnabled = false,
                    localInputNonce = 1,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 79),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 20,
                hostRows = 80,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom + 2.2f,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = false,
                isLoading = false,
                followOnReadEnabled = false,
                localInputNonce = 2,
            )
            val capturedBeforeDraw = view.captureViewportState()

            view.measure(exactlyMeasureSpec(480), exactlyMeasureSpec(240))
            view.layout(0, 0, 480, 240)
            view.update(
                snapshot = terminalSnapshotForViewTest(rows = 80, cols = 20, cursorY = 12),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 3,
                hostCols = 20,
                hostRows = 80,
                fitToViewWidth = false,
                zoomFactor = DefaultTerminalZoom + 2.2f,
                panResetNonce = 0,
                scrollbackOffsetRows = 0,
                imeVisible = true,
                isLoading = true,
                followOnReadEnabled = false,
                localInputNonce = 2,
            )
            view.restoreViewportState(capturedBeforeDraw)

            assertEquals(
                "capturing during a pending keyboard-follow frame must restore as live-bottom, not durable cursor-follow",
                (80 * cellHeight) - 240f,
                view.getCameraOffsetYForTesting(),
                0.01f,
            )
            assertEquals(80, view.getVisibleEndRowExclusive())
        }
        composeRule.waitForIdle()
    }

    @Test
    fun zoomed_scrollback_entry_preserves_pixel_pan_before_row_boundary() {
        assertScrollbackEntryPreservesPixelPanBeforeRowBoundary(DefaultTerminalZoom + 0.8f)
    }

    @Test
    fun default_zoom_scrollback_entry_preserves_pixel_pan_before_row_boundary() {
        assertScrollbackEntryPreservesPixelPanBeforeRowBoundary(DefaultTerminalZoom)
    }

    private fun assertScrollbackEntryPreservesPixelPanBeforeRowBoundary(zoomFactor: Float) {
        lateinit var view: TerminalGridView
        var scrollbackDelta = 0
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                setOnScrollback { scrollbackDelta += it }
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(240))
                layout(0, 0, 480, 240)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 30, cols = 20),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 30,
                    fitToViewWidth = false,
                    zoomFactor = zoomFactor,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = false,
                    isLoading = false,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            val live = terminalSnapshotForViewTest(rows = 30, cols = 20)
            view.restoreViewportState(
                TerminalViewportState(
                    cameraOffsetXPx = 0f,
                    preferredCameraOffsetXPx = 0f,
                    cameraOffsetYPx = 0f,
                    scrollRemainderY = 0f,
                    viewportHeightPx = view.height,
                    scaledCellHeightPx = cellHeight,
                    totalRows = 30,
                ),
            )
            val partialPanPx = cellHeight * 0.35f
            dispatchSinglePointerDrag(
                view,
                startX = 120f,
                startY = 120f,
                endX = 120f,
                endY = 120f + partialPanPx,
            )

            assertEquals(
                "partial overflow into scrollback must request a row immediately so panning can stay pixel-continuous",
                1,
                scrollbackDelta,
            )

            view.update(
                snapshot = buildScrollbackSnapshot(
                    live = live,
                    scrollback = scrollbackRowsForViewTest(rows = 1, cols = 20),
                    offset = 1,
                ),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 20,
                hostRows = 30,
                fitToViewWidth = false,
                zoomFactor = zoomFactor,
                panResetNonce = 0,
                scrollbackOffsetRows = 1,
                imeVisible = false,
                isLoading = false,
            )
            assertEquals(
                cellHeight - partialPanPx,
                view.getCameraOffsetYForTesting(),
                0.01f,
            )
        }
        composeRule.waitForIdle()
    }

    @Test
    fun zoomed_scrollback_entry_reverse_before_snapshot_preserves_live_position() {
        assertScrollbackEntryReverseBeforeSnapshotPreservesLivePosition(
            imeVisible = false,
            fitToViewWidth = false,
        )
    }

    @Test
    fun keyboard_visible_scrollback_entry_reverse_before_snapshot_preserves_live_position() {
        assertScrollbackEntryReverseBeforeSnapshotPreservesLivePosition(
            imeVisible = true,
            fitToViewWidth = true,
        )
    }

    @Test
    fun keyboard_visible_zoomed_scrollback_entry_without_width_fit_preserves_live_position() {
        assertScrollbackEntryReverseBeforeSnapshotPreservesLivePosition(
            imeVisible = true,
            fitToViewWidth = false,
            zoomFactor = DefaultTerminalZoom + 0.8f,
        )
    }

    @Test
    fun default_zoom_scrollback_entry_reverse_before_snapshot_preserves_live_position() {
        assertScrollbackEntryReverseBeforeSnapshotPreservesLivePosition(
            imeVisible = false,
            fitToViewWidth = false,
            zoomFactor = DefaultTerminalZoom,
        )
    }

    private fun assertScrollbackEntryReverseBeforeSnapshotPreservesLivePosition(
        imeVisible: Boolean,
        fitToViewWidth: Boolean,
        zoomFactor: Float = DefaultTerminalZoom + 0.8f,
    ) {
        lateinit var view: TerminalGridView
        var scrollbackDelta = 0
        composeRule.runOnUiThread {
            view = TerminalGridView(composeRule.activity).apply {
                setOnScrollback { scrollbackDelta += it }
                measure(exactlyMeasureSpec(480), exactlyMeasureSpec(240))
                layout(0, 0, 480, 240)
                update(
                    snapshot = terminalSnapshotForViewTest(rows = 30, cols = 20),
                    fontSizeSp = 14,
                    minFontSizeSp = 8,
                    palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                    frameSeq = 1,
                    hostCols = 20,
                    hostRows = 30,
                    fitToViewWidth = fitToViewWidth,
                    zoomFactor = zoomFactor,
                    panResetNonce = 0,
                    scrollbackOffsetRows = 0,
                    imeVisible = imeVisible,
                    isLoading = false,
                )
            }
        }
        composeRule.waitForIdle()

        composeRule.runOnUiThread {
            val cellHeight = view.getScaledCellHeightForTesting()
            assertTrue("terminal cell height was not measured", cellHeight > 0f)
            val live = terminalSnapshotForViewTest(rows = 30, cols = 20)
            view.restoreViewportState(
                TerminalViewportState(
                    cameraOffsetXPx = 0f,
                    preferredCameraOffsetXPx = 0f,
                    cameraOffsetYPx = 0f,
                    scrollRemainderY = 0f,
                    viewportHeightPx = view.height,
                    scaledCellHeightPx = cellHeight,
                    totalRows = 30,
                ),
            )
            val partialPanPx = cellHeight * 0.35f
            dispatchSinglePointerDrag(
                view,
                startX = 120f,
                startY = 120f,
                endX = 120f,
                endY = 120f + partialPanPx,
            )
            assertEquals(1, scrollbackDelta)

            dispatchSinglePointerDrag(
                view,
                startX = 120f,
                startY = 120f,
                endX = 120f,
                endY = 120f - partialPanPx,
            )
            assertEquals(
                "reversing before the scrollback snapshot arrives should move within the live snapshot",
                partialPanPx,
                view.getCameraOffsetYForTesting(),
                0.01f,
            )

            view.update(
                snapshot = buildScrollbackSnapshot(
                    live = live,
                    scrollback = scrollbackRowsForViewTest(rows = 1, cols = 20),
                    offset = 1,
                ),
                fontSizeSp = 14,
                minFontSizeSp = 8,
                palette = TerminalPalette(defaultFg = Color.White, defaultBg = Color.Black),
                frameSeq = 2,
                hostCols = 20,
                hostRows = 30,
                fitToViewWidth = fitToViewWidth,
                zoomFactor = zoomFactor,
                panResetNonce = 0,
                scrollbackOffsetRows = 1,
                imeVisible = imeVisible,
                isLoading = false,
            )
            assertEquals(
                "late scrollback snapshot must preserve the live position reached after reversing direction",
                cellHeight + partialPanPx,
                view.getCameraOffsetYForTesting(),
                0.01f,
            )
        }
        composeRule.waitForIdle()
    }

    @Test
    fun terminal_fills_from_top_before_live_scrolling() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
        val original = readTerminalDebugInfo()
            ?: throw AssertionError("missing original terminal debug info")
        val originalSessionId = original.activeSessionId
        val tallSessionId = startHostsViaHarness(count = 1, cols = 110, rows = 50).single()
        try {
            selectSessionForFixture(tallSessionId, expectedCols = 110, expectedRows = 50)
            resetZoomPan()
            val baseline = waitForTerminalDebugInfo(TERMINAL_READY_TIMEOUT_MS) { info ->
                info.activeSessionId == tallSessionId &&
                    info.cols == 110 &&
                    info.rows == 50 &&
                    info.viewRows > 0 &&
                    info.isLiveBottomAnchored() &&
                    info.visibleEndRowExclusive > info.visibleStartRow &&
                    info.renderScaleX > 0f
            } ?: throw AssertionError("missing baseline terminal debug info")
            var zoomed = baseline
            for (attempt in 0 until 3) {
                setZoomFactor(zoomed.zoomFactor + 0.35f)
                zoomed = waitForTerminalDebugInfo(SHORT_UI_TIMEOUT_MS) { info ->
                    info.renderScaleX > zoomed.renderScaleX + 0.03f &&
                        info.isLiveBottomAnchored() &&
                        info.viewRows > 0
                } ?: throw AssertionError("missing zoomed terminal debug info")
                val visibleRows = zoomed.visibleEndRowExclusive - zoomed.visibleStartRow
                if (visibleRows < zoomed.rows) {
                    break
                }
            }
            val visibleRows = zoomed.visibleEndRowExclusive - zoomed.visibleStartRow
            if (visibleRows >= zoomed.rows) {
                throw AssertionError(
                    "zoomed viewport still fits the whole host screen " +
                        "(visibleRows=$visibleRows, rows=${zoomed.rows})",
                )
            }

            val partialCount = max(3, visibleRows - 4)
            val partialEnd = 100 + partialCount
            sendTerminalInput("seq 101 $partialEnd")
            sendTerminalEnter()
            val partial = waitForTerminalDebugInfo(timeoutMs = 20_000L) { info ->
                info.lastFrameSeq > zoomed.lastFrameSeq &&
                    info.hash != zoomed.hash
            } ?: throw AssertionError("partial terminal output did not render")
            assertTrue(
                "invalid visible range ${partial.visibleStartRow}..${partial.visibleEndRowExclusive}",
                partial.visibleEndRowExclusive > partial.visibleStartRow,
            )
        } finally {
            cleanupFixtureSessions(originalSessionId, original.cols, original.rows, listOf(tallSessionId))
        }
    }

    @Test
    fun resize_setting_default_off_does_not_resize_host() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        setResizeHostEnabled(false)

        loginWithConfiguredUser()
        assertTerminalResponsive()
        val info = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info")
        assertEquals(testConfig.hostCols, info.cols)
        assertEquals(testConfig.hostRows, info.rows)
        assertFalse(info.resizeEnabled)
    }

    @Test
    fun host_width_is_authoritative() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        setResizeHostEnabled(false)

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        val render = waitForTerminalRenderInfo()
            ?: throw AssertionError("terminal view did not render visible output")
        val info = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info")
        assertEquals(testConfig.hostCols, info.cols)
        assertEquals(testConfig.hostRows, info.rows)
        if (info.viewCols <= 0 || info.viewRows <= 0) {
            throw AssertionError(
                "terminal viewport was not measured: cols=${info.viewCols} rows=${info.viewRows}",
            )
        }
        assertTrue(
            "expected minimum zoom to fit the host width into the viewport " +
                "(viewCols=${info.viewCols}, hostCols=${info.cols}, ink=${render.inkLeft},${render.inkTop}..${render.inkRight},${render.inkBottom})",
            info.viewCols == info.cols,
        )
    }

    @Test
    fun terminal_geometry_invariants_cover_small_production_and_large_sizes() {
        data class GeometryCase(val label: String, val cols: Int, val rows: Int)

        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        val original = readTerminalDebugInfo()
            ?: throw AssertionError("missing original terminal debug info")

        val cases = listOf(
            GeometryCase("small", cols = 80, rows = 24),
            GeometryCase("production-minimum", cols = 110, rows = 50),
            GeometryCase("large", cols = 132, rows = 70),
        )
        val originalSessionId = original.activeSessionId
        val fixtureSessionIds = mutableListOf<String>()
        try {
            for (case in cases) {
                val sessionId = startHostsViaHarness(count = 1, cols = case.cols, rows = case.rows).single()
                fixtureSessionIds += sessionId
                selectSessionForFixture(sessionId, expectedCols = case.cols, expectedRows = case.rows)
                composeRule.runOnIdle {
                    appViewModel().resetZoomAndPan()
                }

                val base = waitForTerminalDebugInfo(SHORT_UI_TIMEOUT_MS) { info ->
                    info.activeSessionId == sessionId &&
                        info.cols == case.cols &&
                        info.rows == case.rows &&
                        info.viewCols > 0 &&
                        info.viewRows > 0 &&
                        info.isLiveBottomAnchored() &&
                        info.visibleEndRowExclusive > info.visibleStartRow &&
                        info.visibleEndRowExclusive <= info.rows &&
                        info.renderScaleX > 0f
                } ?: throw AssertionError("missing measured ${case.label} terminal geometry")
                assertEquals("${case.label} terminal should fit width at default zoom", case.cols, base.viewCols)

                setZoomFactor(DefaultTerminalZoom + 0.55f)
                val zoomed = waitForTerminalDebugInfo(SHORT_UI_TIMEOUT_MS) { info ->
                    info.activeSessionId == sessionId &&
                        info.cols == case.cols &&
                        info.rows == case.rows &&
                        info.renderScaleX > base.renderScaleX + 0.03f &&
                        info.isLiveBottomAnchored() &&
                        info.visibleEndRowExclusive > info.visibleStartRow &&
                        info.visibleEndRowExclusive <= info.rows
                } ?: throw AssertionError("missing zoomed ${case.label} terminal geometry")
                if (case.rows >= 50) {
                    assertTrue(
                        "${case.label} zoomed production-sized terminal should not still fit every row " +
                            "(visible=${zoomed.visibleStartRow}-${zoomed.visibleEndRowExclusive}, rows=${zoomed.rows})",
                        zoomed.visibleEndRowExclusive - zoomed.visibleStartRow < zoomed.rows,
                    )
                }
            }
        } finally {
            cleanupFixtureSessions(originalSessionId, original.cols, original.rows, fixtureSessionIds)
        }
    }

    @Test
    fun share_token_width_is_authoritative() {
        val token = testConfig.viewToken ?: return
        setEndpoint(testConfig.endpoint)
        setResizeHostEnabled(false)

        attachViaShareToken(token)
        val render = waitForTerminalRenderInfo()
            ?: throw AssertionError("terminal view did not render visible output")
        val info = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info")
        assertEquals(testConfig.hostCols, info.cols)
        assertEquals(testConfig.hostRows, info.rows)
        if (info.viewCols <= 0 || info.viewRows <= 0) {
            throw AssertionError(
                "terminal viewport was not measured: cols=${info.viewCols} rows=${info.viewRows}",
            )
        }
        assertTrue(
            "expected minimum zoom to fit the share-token host width into the viewport " +
                "(viewCols=${info.viewCols}, hostCols=${info.cols}, ink=${render.inkLeft},${render.inkTop}..${render.inkRight},${render.inkBottom})",
            info.viewCols == info.cols,
        )
    }

    @Test
    fun owner_typing_reclaims_control_after_real_handoff() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        setResizeHostEnabled(false)

        loginWithConfiguredUser()
        openSettings()
        composeRule.onNodeWithTag(TestTags.HeadlessResizeButton, useUnmergedTree = true).assertIsNotEnabled()
        closeSettingsIfOpen()
        assertFirstInputReclaimsControl()
    }

    @Test
    fun control_share_typing_reclaims_control_after_real_handoff() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        val token = testConfig.controlToken ?: throw AssertionError("missing control share token")
        attachViaShareToken(token)
        assertFirstInputReclaimsControl()
    }

    private fun assertFirstInputReclaimsControl() {
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) { appViewModel().state.value.hasControl }
        val application = composeRule.activity.application as LingonApplication
        val sessionId = activeSessionId()
        val epoch = appViewModel().state.value.terminalConnectionEpoch
        val token = appViewModel().state.value.shareToken
        val shareAuthentication = token?.let {
            runBlocking(Dispatchers.IO) { application.wsClient.authenticateShareSession(testConfig.endpoint.toHttpUrl(), it) }
        }
        val peer = application.wsClient.connect(
            RelayWebSocketClient.ConnectOptions(
                baseUrl = testConfig.endpoint.toHttpUrl(), sessionId = sessionId,
                shareToken = token, shareAuthentication = shareAuthentication,
                clientId = "handoff-${UUID.randomUUID()}", cols = 0, rows = 0, wantsControl = true,
            ),
            object : RelayWebSocketClient.Listener {
                override fun onFrame(webSocket: WebSocket, frame: Frame) {}
            },
        )
        try {
            waitUntilNoError(SHORT_UI_TIMEOUT_MS) { !appViewModel().state.value.hasControl }
            val echo = "ECHO_${sessionId} 71"
            val before = snapshotTokenCount(echo)
            val inputNonce = appViewModel().state.value.localInputNonce
            // One actual hardware key, without the retry helper: this very key must reach the host.
            sendTerminalInput("q")
            waitUntilNoError(SHORT_UI_TIMEOUT_MS) {
                appViewModel().state.value.hasControl && snapshotTokenCount(echo) == before + 1
            }
            assertEquals(epoch, appViewModel().state.value.terminalConnectionEpoch)
            assertEquals(inputNonce + 1, appViewModel().state.value.localInputNonce)
            assertEquals(before + 1, snapshotTokenCount(echo))
        } finally {
            peer.close(1000, "handoff test finished")
        }
    }

    @Test
    fun resize_setting_override_still_does_not_resize_host_when_in_control() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        setResizeHostEnabled(false)

        loginWithConfiguredUser()
        assertTerminalResponsive()
        waitUntilNoError(5_000) {
            val info = readTerminalDebugInfo()
            info != null && info.hasControl
        }
        setResizeHostEnabled(true)
        composeRule.runOnIdle {
            val state = appViewModel().state.value
            if (state.terminalCols > 0 && state.terminalRows > 0) {
                appViewModel().updateTerminalSize(state.terminalCols, state.terminalRows)
            }
        }
        val info = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info")
        assertEquals(testConfig.hostCols, info.cols)
        assertEquals(testConfig.hostRows, info.rows)
        assertFalse(info.resizeEnabled)
    }

    @Test
    fun resize_setting_disabled_for_view_only_share_token() {
        val token = testConfig.viewToken ?: return
        setEndpoint(testConfig.endpoint)
        setResizeHostEnabled(false)

        attachViaShareToken(token)
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) {
            val info = readTerminalDebugInfo()
            info != null && !info.hasControl
        }
        openSettings()
        composeRule.onNodeWithTag(TestTags.HeadlessResizeButton, useUnmergedTree = true).assertIsNotEnabled()
        val info = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info")
        assertTrue("view cols=${info.viewCols}", info.viewCols > 0)
        assertTrue("view rows=${info.viewRows}", info.viewRows > 0)
        closeSettingsIfOpen()
        assertTerminalInputBlocked()
    }

    @Test
    fun tab_bar_shows_sessions_when_available() {
        if (testConfig.sessions.isEmpty()) return
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        assertTerminalResponsive()
        testConfig.sessions.forEach { sessionId ->
            waitForTagNoError(TestTags.tabTag(sessionId))
        }
    }

    @Test
    fun tab_switch_routes_keyboard_to_active_session() {
        if (testConfig.sessions.size < 2) return
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        val first = activeSessionId()
        val second = testConfig.sessions.firstOrNull { it != first } ?: return
        assertTerminalResponsive(first)

        selectSessionTab(second, timeoutMs = 10_000L)
        assertTerminalResponsive(second)

        selectSessionTab(first, timeoutMs = 10_000L)
        assertTerminalResponsive(first)
    }

    @Test
    fun tab_switch_does_not_rearm_wall_inactivity_without_terminal_input() {
        if (testConfig.sessions.size < 2) return
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        val first = activeSessionId()
        val second = testConfig.sessions.firstOrNull { it != first } ?: return
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        ensureAllWallInactivityOff()

        val baselineWalls = wallEventCount()
        clickSettingsWallInactivity()
        try {
            waitUntilNoError(5_000L) {
                appViewModel().state.value.wallInactivityEnabled &&
                    appViewModel().state.value.wallInactivityLabel == "250ms"
            }
        } catch (err: AssertionError) {
            throw AssertionError("wall enable setup: baseline=$baselineWalls, actual=${wallEventCount()}, status=${readStatusBanner()}; ${err.message}", err)
        }
        assertEquals("wall must wait for controlled time", baselineWalls, wallEventCount())
        // The relay schedules inactivity timers with a one-second minimum.
        advanceWallClockViaHarness(999)
        assertEquals("wall fired before its exact deadline", baselineWalls, wallEventCount())
        advanceWallClockViaHarness(1)
        waitUntilNoError(5_000L) { wallEventCount() == baselineWalls + 1 }

        selectSessionTab(second, timeoutMs = 10_000L)
        waitUntilNoError(10_000L) { activeSessionId() == second }
        selectSessionTab(first, timeoutMs = 10_000L)
        waitUntilNoError(10_000L) { activeSessionId() == first }
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)

        advanceWallClockViaHarness(1500)
        assertEquals("tab switches must not rearm inactivity", baselineWalls + 1, wallEventCount())
        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        sendTerminalInput("echo WALLINPUT$(echo ACK)")
        sendTerminalEnter()
        // The marker appears only in shell output, not in the command's echo.
        waitUntilNoError(5_000L) { snapshotContainsToken("WALLINPUTACK") }
        advanceWallClockViaHarness(1000)
        waitUntilNoError(5_000L) { wallEventCount() == baselineWalls + 2 }
    }

    @Test
    fun wall_inactivity_banner_auto_dismisses_without_tab_switch() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        ensureAllWallInactivityOff()

        val observedBanner = AtomicBoolean(false)
        lateinit var observer: Job
        composeRule.runOnUiThread {
            observer = CoroutineScope(Dispatchers.Main.immediate).launch(start = CoroutineStart.UNDISPATCHED) {
                appViewModel().state.collect { current ->
                    if (current.transientStatus?.message == "wall 250ms") observedBanner.set(true)
                }
            }
        }
        try {
            openSettings()
            composeRule.onNodeWithText("Wall inactivity")
                .performScrollTo()
                .performClick()
            waitUntilNoError(5_000L) { observedBanner.get() }
            closeSettingsIfOpen()
            waitUntilNoError(6_000L) { readStatusBanner() == null }
        } finally {
            observer.cancel()
        }
    }

    @Test
    fun keyboard_tab_switch_preserves_bottom_anchor_visual() {
        if (testConfig.sessions.size < 2) return
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        clearScreenshotArtifacts()

        loginWithConfiguredUser()
        val first = activeSessionId()
        val second = testConfig.sessions.firstOrNull { it != first } ?: return
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        assertTerminalResponsive(first)
        composeRule.runOnIdle {
            appViewModel().resetZoomAndPan()
        }
        selectSessionTab(second, timeoutMs = 10_000L)
        waitUntilNoError(10_000L) { activeSessionId() == second }
        assertTerminalResponsive(second)
        composeRule.runOnIdle {
            appViewModel().resetZoomAndPan()
        }
        selectSessionTab(first, timeoutMs = 10_000L)
        waitUntilNoError(10_000L) { activeSessionId() == first }
        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        composeRule.waitForIdle()
        val firstBeforeLoad = readTerminalDebugInfo() ?: throw AssertionError("missing terminal debug info before loading first tab")
        sendTerminalInput("seq 1 60")
        sendTerminalEnter()
        waitUntilNoError(20_000L) {
            val info = readTerminalDebugInfo()
            info != null &&
                info.activeSessionId == first &&
                info.lastFrameSeq > firstBeforeLoad.lastFrameSeq &&
                info.hash != firstBeforeLoad.hash
        }

        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        waitUntilNoError(10_000L) { hasTextNode("CTRL") }
        captureScreenshot("tab-first-loaded")
        val firstLoaded = readTerminalDebugInfo() ?: throw AssertionError("missing terminal debug info for first loaded tab")

        selectSessionTab(second, timeoutMs = 10_000L)
        waitUntilNoError(10_000L) { activeSessionId() == second }
        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        composeRule.waitForIdle()
        val secondBeforeLoad = readTerminalDebugInfo() ?: throw AssertionError("missing terminal debug info before loading second tab")
        sendTerminalInput("seq 1001 1060")
        sendTerminalEnter()
        waitUntilNoError(20_000L) {
            val info = readTerminalDebugInfo()
            info != null &&
                info.activeSessionId == second &&
                info.lastFrameSeq > secondBeforeLoad.lastFrameSeq &&
                info.hash != secondBeforeLoad.hash
        }
        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        waitUntilNoError(10_000L) { hasTextNode("CTRL") }
        captureScreenshot("tab-second-loaded")

        var firstRestored: TerminalDebugInfo? = null
        repeat(20) { hop ->
            val hopNumber = hop + 1

            selectSessionTab(first, timeoutMs = 10_000L)
            waitUntilNoError(10_000L) {
                val info = readTerminalDebugInfo()
                info != null && info.activeSessionId == first
            }
            composeRule.waitForIdle()
            Thread.sleep(150)
            captureScreenshot("tab-first-hop-${hopNumber}-immediate")
            waitUntilNoError(10_000L) {
                val info = readTerminalDebugInfo()
                info != null && info.activeSessionId == first && !stateForTest().sessionSyncing
            }
            composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
            waitUntilNoError(10_000L) { hasTextNode("CTRL") }
            assertLiveBottomAnchored("first tab hop $hopNumber")
            captureScreenshot("tab-first-hop-${hopNumber}-settled")
            if (hop == 0) {
                firstRestored = readTerminalDebugInfo()
            }

            selectSessionTab(second, timeoutMs = 10_000L)
            waitUntilNoError(10_000L) {
                val info = readTerminalDebugInfo()
                info != null && info.activeSessionId == second
            }
            composeRule.waitForIdle()
            Thread.sleep(150)
            captureScreenshot("tab-second-hop-${hopNumber}-immediate")
            waitUntilNoError(10_000L) {
                val info = readTerminalDebugInfo()
                info != null && info.activeSessionId == second && !stateForTest().sessionSyncing
            }
            composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
            waitUntilNoError(10_000L) { hasTextNode("CTRL") }
            assertLiveBottomAnchored("second tab hop $hopNumber")
            captureScreenshot("tab-second-hop-${hopNumber}-settled")
        }

        val restored = firstRestored ?: throw AssertionError("missing terminal debug info after restoring first tab")
        assertEquals(first, restored.activeSessionId)
        assertTrue(
            "visible terminal content changed across tab switch: before=${firstLoaded.hash} after=${restored.hash}",
            firstLoaded.hash == restored.hash || restored.lastFrameSeq >= firstLoaded.lastFrameSeq,
        )
    }

    @Test
    fun keyboard_hide_show_preserves_bottom_anchor_visual() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        clearScreenshotArtifacts()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        activeSessionId()
        composeRule.runOnIdle {
            appViewModel().resetZoomAndPan()
        }
        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        composeRule.waitForIdle()

        val beforeLoad = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info before loading keyboard hide/show fixture")
        sendTerminalInput("seq 1 60")
        sendTerminalEnter()
        waitUntilNoError(20_000L) {
            val info = readTerminalDebugInfo()
            info != null &&
                info.lastFrameSeq > beforeLoad.lastFrameSeq &&
                info.hash != beforeLoad.hash
        }
        assertLiveBottomAnchored("keyboard precheck")
        captureScreenshot("keyboard-precheck")

        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        waitUntilNoError(10_000L) { hasTextNode("CTRL") }
        assertTerminalViewportAboveQuickKeys("keyboard visible initial")
        assertLiveBottomAnchored("keyboard visible initial")
        captureScreenshot("keyboard-visible-initial")

        InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(KeyEvent.KEYCODE_BACK)
        waitUntilNoError(10_000L) { !hasTextNode("CTRL") }
        assertLiveBottomAnchored("keyboard hidden")
        captureScreenshot("keyboard-hidden")

        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        waitUntilNoError(10_000L) { hasTextNode("CTRL") }
        assertTerminalViewportAboveQuickKeys("keyboard visible restored")
        assertLiveBottomAnchored("keyboard visible restored")
        captureScreenshot("keyboard-visible-restored")
    }

    @Test
    fun keyboard_hide_show_preserves_bottom_anchor_for_tall_sessions() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        // Static geometry checks need the complete tall payload, without an
        // unrelated periodic echo producer keeping Compose continuously busy.
        val tallHosts = startHostsViaHarness(count = 2, rows = 240, shell = "/bin/sh", initialLines = 260)
        val first = tallHosts[0]
        val second = tallHosts[1]
        val sessions = listOf(first, second)
        sessions.forEach { sessionId ->
            waitUntilNoError(20_000L) { appViewModel().state.value.sessions.any { it.id == sessionId } }
            selectSessionTab(sessionId, timeoutMs = 10_000L)
            waitUntilNoError(10_000L) {
                activeSessionId() == sessionId && !stateForTest().sessionSyncing &&
                    snapshotContainsToken("LINE_${sessionId} 259")
            }
            composeRule.runOnIdle {
                appViewModel().resetZoomAndPan()
            }
            focusTerminalInput()
            waitUntilNoError(10_000L) { hasTextNode("CTRL") }
            assertTerminalViewportAboveQuickKeys("tall session $sessionId keyboard visible")
            waitForTerminalDebugInfo(timeoutMs = 20_000L) { info ->
                info.activeSessionId == sessionId &&
                    info.rows > info.viewRows &&
                    info.isLiveBottomAnchored()
            } ?: throw AssertionError("tall session $sessionId did not settle at live bottom")

            InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(KeyEvent.KEYCODE_BACK)
            waitUntilNoError(10_000L) { !hasTextNode("CTRL") }
            assertTerminalCameraUsesVisibleSystemViewport("tall session $sessionId keyboard hidden")
            waitForTerminalDebugInfo(timeoutMs = SHORT_UI_TIMEOUT_MS) { info ->
                info.activeSessionId == sessionId &&
                    info.isLiveBottomAnchored()
            } ?: throw AssertionError("hidden keyboard misaligned live bottom for $sessionId")

            focusTerminalInput()
            waitUntilNoError(10_000L) { hasTextNode("CTRL") }
            assertTerminalViewportAboveQuickKeys("tall session $sessionId keyboard visible restored")
            assertTerminalCameraUsesVisibleSystemViewport("tall session $sessionId keyboard visible restored")
            waitForTerminalDebugInfo(timeoutMs = SHORT_UI_TIMEOUT_MS) { info ->
                info.activeSessionId == sessionId &&
                    info.isLiveBottomAnchored()
            } ?: throw AssertionError("visible keyboard misaligned live bottom for $sessionId")
        }

        InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(KeyEvent.KEYCODE_BACK)
        waitUntilNoError(10_000L) { !hasTextNode("CTRL") }
        selectSessionTab(first, timeoutMs = 10_000L)
        waitUntilNoError(10_000L) { activeSessionId() == first && !stateForTest().sessionSyncing }
        assertTerminalCameraUsesVisibleSystemViewport("first tall session hidden after cross-switch")
        waitForTerminalDebugInfo(timeoutMs = SHORT_UI_TIMEOUT_MS) { info ->
            info.activeSessionId == first && info.isLiveBottomAnchored()
        } ?: throw AssertionError("hidden keyboard misaligned live bottom for cached first tall session")

        selectSessionTab(second, timeoutMs = 10_000L)
        waitUntilNoError(10_000L) { activeSessionId() == second && !stateForTest().sessionSyncing }
        focusTerminalInput()
        waitUntilNoError(10_000L) { hasTextNode("CTRL") }
        assertTerminalViewportAboveQuickKeys("second tall session visible before cross-switch")
        waitForTerminalDebugInfo(timeoutMs = SHORT_UI_TIMEOUT_MS) { info ->
            info.activeSessionId == second && info.isLiveBottomAnchored()
        } ?: throw AssertionError("visible keyboard misaligned live bottom for second tall session before cross-switch")

        selectSessionTab(first, timeoutMs = 10_000L)
        waitUntilNoError(10_000L) { activeSessionId() == first && !stateForTest().sessionSyncing }
        assertTerminalViewportAboveQuickKeys("first tall session visible after cross-switch")
        assertTerminalCameraUsesVisibleSystemViewport("first tall session visible after cross-switch")
        waitForTerminalDebugInfo(timeoutMs = SHORT_UI_TIMEOUT_MS) { info ->
            info.activeSessionId == first && info.isLiveBottomAnchored()
        } ?: throw AssertionError("visible keyboard misaligned live bottom after restoring cached first tall session")
    }

    @Test
    fun loaded_tall_session_uses_system_visible_bottom_before_and_after_keyboard_toggle() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        enableSoftKeyboardWithHardwareKeyboard()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        val sessionId = startHostsViaHarness(count = 1, rows = 240, initialLines = 260).single()
        waitUntilNoError(20_000L) { appViewModel().state.value.sessions.any { it.id == sessionId } }
        selectSessionTab(sessionId, timeoutMs = 10_000L)
        waitUntilNoError(10_000L) { activeSessionId() == sessionId && !stateForTest().sessionSyncing }
        setZoomFactor(3.2f)

        waitForTerminalDebugInfo(timeoutMs = 20_000L) { info ->
            info.activeSessionId == sessionId &&
                info.rows > info.viewRows &&
                info.isLiveBottomAnchored()
        } ?: throw AssertionError("loaded tall session did not start live-bottom anchored")
        assertTerminalCameraUsesVisibleSystemViewport("loaded tall session with keyboard hidden")
        assertTerminalViewportStable("loaded tall session with keyboard hidden")

        focusTerminalInput()
        waitUntilNoError(15_000L) { hasTextNode("CTRL") && readImeBottomInsetPx() > readNavigationBottomInsetPx() }
        assertTerminalViewportAboveQuickKeys("loaded tall session keyboard visible")
        assertTerminalCameraUsesVisibleSystemViewport("loaded tall session keyboard visible")
        waitForTerminalDebugInfo(timeoutMs = SHORT_UI_TIMEOUT_MS) { info ->
            info.activeSessionId == sessionId &&
                info.rows > info.viewRows &&
                info.isLiveBottomAnchored()
        } ?: throw AssertionError("loaded tall session did not remain live-bottom anchored after keyboard show")
        assertTerminalViewportStable("loaded tall session keyboard visible")

        InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(KeyEvent.KEYCODE_BACK)
        waitUntilNoError(10_000L) { !hasTextNode("CTRL") }
        assertTerminalCameraUsesVisibleSystemViewport("loaded tall session keyboard hidden again")
        waitForTerminalDebugInfo(timeoutMs = SHORT_UI_TIMEOUT_MS) { info ->
            info.activeSessionId == sessionId &&
                info.rows > info.viewRows &&
                info.isLiveBottomAnchored()
        } ?: throw AssertionError("loaded tall session did not remain live-bottom anchored after keyboard hide")
        assertTerminalViewportStable("loaded tall session keyboard hidden again")
    }

    @Test
    fun soft_keyboard_inset_keeps_terminal_viewport_above_keyboard() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        enableSoftKeyboardWithHardwareKeyboard()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        val sessionId = startHostsViaHarness(count = 1, rows = 240, initialLines = 260).single()
        waitUntilNoError(20_000L) { appViewModel().state.value.sessions.any { it.id == sessionId } }
        selectSessionTab(sessionId, timeoutMs = 10_000L)
        waitUntilNoError(10_000L) { activeSessionId() == sessionId && !stateForTest().sessionSyncing }
        composeRule.runOnIdle {
            appViewModel().resetZoomAndPan()
        }

        focusTerminalInput()
        waitUntilNoError(15_000L) { hasTextNode("CTRL") && readImeBottomInsetPx() > readNavigationBottomInsetPx() }
        assertTerminalViewportAboveIme("focused tall terminal with real soft keyboard")
        waitForTerminalDebugInfo(timeoutMs = SHORT_UI_TIMEOUT_MS) { info ->
            info.activeSessionId == sessionId &&
                info.rows > info.viewRows &&
                info.isLiveBottomAnchored()
        } ?: throw AssertionError("real soft keyboard did not leave tall terminal bottom-anchored in camera mode")
        assertTerminalCameraUsesVisibleImeViewport("focused tall terminal with real soft keyboard")
        assertTerminalViewportStable("focused tall terminal with real soft keyboard")
    }

    @Test
    fun keyboard_hidden_before_background_stays_hidden_after_resume() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        focusTerminalInput()
        waitUntilNoError(10_000L) { hasTextNode("CTRL") }

        InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(KeyEvent.KEYCODE_BACK)
        waitUntilNoError(10_000L) { !hasTextNode("CTRL") }
        waitUntilNoError(10_000L) { stateForTest().restoreTerminalImeOnLifecycleStart == false }

        backgroundAndResumeActivity()
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = SHORT_UI_TIMEOUT_MS)
        Thread.sleep(1_000L)
        assertFalse("IME lifecycle preference changed after hidden background resume", stateForTest().restoreTerminalImeOnLifecycleStart == true)
        assertFalse("IME controls were restored even though keyboard was hidden before background", hasTextNode("CTRL"))
    }

    @Test
    fun keyboard_visible_before_background_is_restored_after_resume() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        focusTerminalInput()
        waitUntilNoError(10_000L) { hasTextNode("CTRL") }

        backgroundAndResumeActivity()
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = SHORT_UI_TIMEOUT_MS)
        waitUntilNoError(10_000L) { hasTextNode("CTRL") }
        Thread.sleep(1_500L)
        assertTrue("IME controls disappeared after the delayed post-resume hide window", hasTextNode("CTRL"))
    }

    @Test
    fun focused_background_resume_preserves_live_camera_when_cursor_still_fits() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        val headlessId = startHeadlessViaHarness(rows = 240)
        waitUntilNoError(20_000L) { appViewModel().state.value.sessions.any { it.id == headlessId } }
        selectSessionForFixture(headlessId)
        waitUntilNoError(10_000L) { activeSessionId() == headlessId && !stateForTest().sessionSyncing }
        composeRule.runOnIdle {
            appViewModel().resetZoomAndPan()
        }
        focusTerminalInput()
        waitUntilNoError(10_000L) { hasTextNode("CTRL") }

        val beforeLoad = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info before lifecycle fixture")
        assertTrue(
            "fixture requires a terminal taller than the focused viewport: rows=${beforeLoad.rows}, viewRows=${beforeLoad.viewRows}",
            beforeLoad.rows > beforeLoad.viewRows,
        )
        val visibleRows = (beforeLoad.visibleEndRowExclusive - beforeLoad.visibleStartRow).coerceAtLeast(4)
        val outputRows = (visibleRows - 3).coerceAtLeast(1)
        sendTerminalInput("clear; seq 1 $outputRows")
        sendTerminalEnter()
        waitForTerminalDebugInfo(timeoutMs = 20_000L) { info ->
            info.lastFrameSeq > beforeLoad.lastFrameSeq && info.hash != beforeLoad.hash
        } ?: throw AssertionError("terminal fixture did not render before positioning the camera")
        composeRule.runOnIdle {
            val view = findTerminalView() as? TerminalGridView ?: throw AssertionError("missing terminal grid")
            view.restoreViewportState(view.captureViewportState().copy(
                cameraOffsetYPx = 0f,
                mode = systems.pkt.lingon.terminal.TerminalViewportMode.Manual,
            ))
        }
        val loaded = waitForTerminalDebugInfo(timeoutMs = 20_000L) { info ->
            info.lastFrameSeq > beforeLoad.lastFrameSeq &&
                info.hash != beforeLoad.hash &&
                info.visibleStartRow == 0 &&
                info.cursorY in info.visibleStartRow until info.visibleEndRowExclusive
        } ?: throw AssertionError("terminal fixture did not settle with cursor visible before lifecycle refocus")
        assertEquals(
            "fixture must start with the saved camera at the top",
            0f,
            loaded.cameraOffsetYPx,
            0.01f,
        )
        var beforeViewport: systems.pkt.lingon.terminal.TerminalViewportState? = null
        composeRule.runOnIdle {
            beforeViewport = (findTerminalView() as? TerminalGridView)?.captureViewportState()
        }

        backgroundAndResumeActivity()
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = SHORT_UI_TIMEOUT_MS)
        val resumed = waitForTerminalDebugInfo(timeoutMs = SHORT_UI_TIMEOUT_MS) { info ->
            info.activeSessionId == headlessId &&
                info.viewRows > 0 &&
                !stateForTest().sessionSyncing
        } ?: throw AssertionError("focused lifecycle refocus moved the live camera without new terminal output")
        var afterViewport: systems.pkt.lingon.terminal.TerminalViewportState? = null
        composeRule.runOnIdle {
            afterViewport = (findTerminalView() as? TerminalGridView)?.captureViewportState()
        }
        assertEquals(
            "focused lifecycle refocus changed visible start row (before=$beforeViewport after=$afterViewport)",
            loaded.visibleStartRow,
            resumed.visibleStartRow,
        )
        assertEquals(
            "focused lifecycle refocus moved camera without new terminal output",
            loaded.cameraOffsetYPx,
            resumed.cameraOffsetYPx,
            0.01f,
        )
    }

    @Test
    fun keyboard_visible_zoomed_pan_across_scrollback_boundary_is_continuous() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        composeRule.runOnIdle {
            appViewModel().resetZoomAndPan()
        }
        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        waitUntilNoError(10_000L) { hasTextNode("CTRL") }

        val beforeLoad = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info before loading scrollback fixture")
        sendTerminalInput("seq 1 180")
        sendTerminalEnter()
        val loaded = waitForTerminalDebugInfo(timeoutMs = 20_000L) { info ->
            info.lastFrameSeq > beforeLoad.lastFrameSeq && info.hash != beforeLoad.hash
        } ?: throw AssertionError("scrollback fixture did not render")
        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        waitUntilNoError(10_000L) { hasTextNode("CTRL") }

        setZoomFactor(3.2f)
        val zoomed = waitForTerminalDebugInfo(SHORT_UI_TIMEOUT_MS) { info ->
            info.renderScaleY > loaded.renderScaleY + 0.1f &&
                info.scaledCellHeightPx > 0f &&
                info.viewRows > 0 &&
                info.visibleEndRowExclusive > info.visibleStartRow
        } ?: throw AssertionError("zoomed keyboard-visible terminal debug info did not settle")

        val stepRows = maxOf(0.35f, (zoomed.liveTopRows() + 3f) / 55f)
        val stepPx = (zoomed.scaledCellHeightPx * stepRows).coerceAtLeast(8f)
        val olderMoveCount = kotlin.math.ceil((zoomed.liveTopRows() + 3f) / stepRows)
            .toInt()
            .coerceAtLeast(20)
        val moves = List(olderMoveCount) { stepPx } + List(olderMoveCount + 8) { -stepPx }
        val samples = dispatchTerminalDragPulses(moves)
        assertTrue(
            "test never entered scrollback while keyboard was visible: ${samples.describePanSamples()}",
            samples.any { it.scrollbackOffsetRows > 0 },
        )
        assertTrue(
            "test never panned back toward live after entering scrollback: ${samples.describePanSamples()}",
            samples.dropWhile { it.scrollbackOffsetRows <= 0 }.any { it.scrollbackOffsetRows == 0 },
        )

        for (i in 1 until samples.size) {
            val previous = samples[i - 1]
            val current = samples[i]
            if (previous.scaledCellHeightPx <= 0f || current.scaledCellHeightPx <= 0f) continue
            val previousLiveTopRows = previous.liveTopRows()
            val currentLiveTopRows = current.liveTopRows()
            val expectedMoveRows = kotlin.math.abs(moves[i]) / current.scaledCellHeightPx
            val actualMoveRows = kotlin.math.abs(currentLiveTopRows - previousLiveTopRows)
            assertTrue(
                "pan jumped at move $i: actualRows=$actualMoveRows expectedRows=$expectedMoveRows " +
                    "previous=$previous current=$current all=${samples.describePanSamples()}",
                actualMoveRows <= expectedMoveRows + 0.65f,
            )
        }
    }

    @Test
    fun tab_switch_after_heavy_output_renders_latest_frame_without_input() {
        if (testConfig.sessions.size < 2) return
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        val first = activeSessionId()
        val second = testConfig.sessions.firstOrNull { it != first } ?: return
        assertTerminalResponsive(first)

        val beforeLoad = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info before heavy output")
        sendTerminalInput("seq 1 120")
        sendTerminalEnter()
        val loaded = waitForTerminalDebugInfo(timeoutMs = 20_000L) { info ->
            info.activeSessionId == first &&
                info.lastFrameSeq > beforeLoad.lastFrameSeq &&
                info.hash != beforeLoad.hash
        } ?: throw AssertionError("latest frame did not render before tab switch")

        selectSessionTab(second, timeoutMs = 10_000L)
        waitUntilNoError(1_000L) { activeSessionId() == second }

        composeRule.runOnIdle {
            appViewModel().selectSession(first)
        }
        waitForTerminalDebugInfo(timeoutMs = 20_000L) { info ->
            info.activeSessionId == first &&
                (info.hash == loaded.hash || info.lastFrameSeq >= loaded.lastFrameSeq)
        } ?: throw AssertionError("latest frame did not render after tab switch without input")
    }

    @Test
    fun active_tab_persists_after_activity_recreate() {
        if (testConfig.sessions.size < 2) return
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        val initial = activeSessionId()
        val target = testConfig.sessions.firstOrNull { it != initial } ?: return
        selectSessionTab(target, timeoutMs = SHORT_UI_TIMEOUT_MS)
        assertTerminalResponsive(target)

        recreateActivity()
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = 15_000L)
        waitUntilNoError(15_000L) {
            val info = readTerminalDebugInfo()
            info != null && info.state == "Connected" && info.activeSessionId == target
        }
    }

    @Test
    fun active_tab_persists_after_background_foreground_cycle() {
        if (testConfig.sessions.size < 2) return
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        val initial = activeSessionId()
        val target = testConfig.sessions.firstOrNull { it != initial } ?: return
        selectSessionTab(target, timeoutMs = SHORT_UI_TIMEOUT_MS)
        assertTerminalResponsive(target)

        backgroundAndResumeActivity()
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = SHORT_UI_TIMEOUT_MS)
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) {
            val info = readTerminalDebugInfo()
            info != null && info.state == "Connected" && info.activeSessionId == target
        }
    }

    @Test
    fun background_foreground_resumes_input() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        assertTerminalResponsive()

        backgroundAndResumeActivity()
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = SHORT_UI_TIMEOUT_MS)
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) {
            val info = readTerminalDebugInfo()
            info != null && info.state == "Connected" && info.activeSessionId.isNotBlank()
        }
        assertTerminalResponsive(requireControl = false)
    }

    @Test
    fun background_foreground_preserves_bottom_anchor_visual() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        val sessionId = activeSessionId()
        assertTerminalResponsive(sessionId)
        composeRule.runOnIdle {
            appViewModel().resetZoomAndPan()
        }
        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        composeRule.waitForIdle()
        val beforeLoad = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info before background/foreground fixture")
        sendTerminalInput("seq 1 60")
        sendTerminalEnter()
        val loaded = waitForTerminalDebugInfo(timeoutMs = 20_000L) { info ->
            info.activeSessionId == sessionId &&
                info.lastFrameSeq > beforeLoad.lastFrameSeq &&
                info.hash != beforeLoad.hash &&
                info.visibleEndRowExclusive > info.visibleStartRow
        } ?: throw AssertionError("terminal fixture did not render before background/foreground cycle")

        backgroundAndResumeActivity()
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = SHORT_UI_TIMEOUT_MS)
        waitForTerminalDebugInfo(timeoutMs = SHORT_UI_TIMEOUT_MS) { info ->
            info.activeSessionId == sessionId &&
                info.visibleStartRow == loaded.visibleStartRow &&
                info.visibleEndRowExclusive == loaded.visibleEndRowExclusive &&
                (info.hash == loaded.hash || info.lastFrameSeq >= loaded.lastFrameSeq)
        } ?: throw AssertionError("viewport anchor changed across background/foreground cycle")
    }

    @Test
    fun background_wall_delivery_posts_system_notification() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        clearAppNotifications()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        ensureAllWallInactivityOff()
        syncWallCursorToLatest()

        composeRule.runOnUiThread {
            appViewModel().setBackgroundWallEnabled(true)
        }
        composeRule.waitForIdle()
        waitUntilNoError(5_000L) { appViewModel().state.value.backgroundWallEnabled }

        clickSettingsWallInactivity()
        waitUntilNoError(5_000L) {
            appViewModel().state.value.wallInactivityEnabled &&
                appViewModel().state.value.wallInactivityLabel == "250ms"
        }

        backgroundActivity()
        waitUntilDeviceCondition(10_000L, "background wall service notification") {
            activeNotifications().any { it.notification.channelId == "lingon_background_wall" }
        }
        waitUntilDeviceCondition(BACKGROUND_WALL_NOTIFICATION_TIMEOUT_MS, "wall inactivity notification") {
            wallNotifications().isNotEmpty()
        }
        assertNoWallNotificationAutogroupSummary()
        val wallNotification = wallNotifications()
            .firstOrNull()
            ?: throw AssertionError("missing lingon_wall notification")
        assertTrue(
            "wall notification should not be grouped: group=${wallNotification.notification.group.orEmpty()}",
            wallNotification.notification.group.isNullOrBlank(),
        )
        val title = wallNotification.notification.extras
            .getCharSequence(Notification.EXTRA_TITLE)
            ?.toString()
            ?.trim()
            .orEmpty()
        val text = wallNotification.notification.extras
            .getCharSequence(Notification.EXTRA_TEXT)
            ?.toString()
            ?.trim()
            .orEmpty()
        assertTrue("notification title missing username: $title", title.startsWith("${testConfig.username}@"))
        assertTrue("notification title missing session label: $title", title.endsWith("#${activeSessionId()}"))
        assertEquals("${activeSessionId()} inactive", text)
    }

    @Test
    fun headless_resize_button_only_enables_for_headless_sessions() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        openSettings()
        composeRule.onNodeWithTag(TestTags.HeadlessResizeButton, useUnmergedTree = true).assertIsNotEnabled()
        closeSettingsIfOpen()

        val headlessId = startHeadlessViaHarness()
        waitUntilNoError(20_000L) { appViewModel().state.value.sessions.any { it.id == headlessId } }
        selectSessionForFixture(headlessId)
        waitUntilNoError(10_000L) { activeSessionId() == headlessId && !stateForTest().sessionSyncing }

        openSettings()
        composeRule.onNodeWithTag(TestTags.HeadlessResizeButton, useUnmergedTree = true).assertIsEnabled()
        closeSettingsIfOpen()
    }

    @Test
    fun headless_resize_button_resizes_remote_headless_session() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)

        val headlessId = startHeadlessViaHarness()
        waitUntilNoError(20_000L) { appViewModel().state.value.sessions.any { it.id == headlessId } }
        selectSessionForFixture(headlessId)
        waitUntilNoError(10_000L) { activeSessionId() == headlessId && !stateForTest().sessionSyncing }
        waitUntilNoError(10_000L) {
            val info = readTerminalDebugInfo()
            info != null && info.viewCols > 0 && info.viewRows > 0
        }

        val before = readHeadlessSizeViaHarness(headlessId)
        assertEquals(120, before.cols)
        assertEquals(50, before.rows)

        val expectedCols = stateForTest().terminalCols
        val expectedRows = stateForTest().terminalRows
        openSettings()
        composeRule.onNodeWithTag(TestTags.HeadlessResizeButton, useUnmergedTree = true).performClick()
        closeSettingsIfOpen()
        waitUntilNoError(10_000L) {
            val after = readHeadlessSizeViaHarness(headlessId)
            after.cols == expectedCols && after.rows == expectedRows
        }
    }

    @Test
    fun headless_detach_removes_session_without_reconnect_placeholder() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)

        val headlessId = startHeadlessViaHarness()
        waitUntilNoError(20_000L) { appViewModel().state.value.sessions.any { it.id == headlessId } }
        selectSessionForFixture(headlessId)
        waitUntilNoError(10_000L) { activeSessionId() == headlessId && !stateForTest().sessionSyncing }

        detachHeadlessViaHarness(headlessId)

        waitUntilNoError(20_000L) {
            val state = stateForTest()
            state.sessions.none { it.id == headlessId } &&
                !state.activeSessionId.isNullOrBlank() &&
                state.sessions.any { it.id == state.activeSessionId } &&
                state.activeSessionId != headlessId &&
                !state.sessionSyncing
        }
        assertTerminalResponsive(requireNotNull(activeSessionId()))
        val banner = readStatusBanner()?.message.orEmpty()
        assertFalse("unexpected reconnect/not-connected banner: $banner", banner.contains("Not connected", ignoreCase = true))
        assertFalse("unexpected reconnect/not-connected banner: $banner", banner.contains("connection lost", ignoreCase = true))
    }

    @Test
    fun wall_event_count_includes_all_relay_pages() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        composeRule.runOnUiThread { appViewModel().setBackgroundWallEnabled(false) }
        composeRule.waitForIdle()
        ensureAllWallInactivityOff()
        val before = wallEventCount()
        val nonce = System.currentTimeMillis()
        repeat(34) { index -> sendWallViaHarness("page boundary $nonce $index") }
        assertEquals("wall count must include every relay page", before + 34, wallEventCount())
    }

    @Test
    fun foreground_manual_wall_delivery_does_not_post_system_notification() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        clearAppNotifications()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        composeRule.runOnUiThread {
            appViewModel().setBackgroundWallEnabled(false)
        }
        composeRule.waitForIdle()
        waitUntilNoError(5_000L) { !appViewModel().state.value.backgroundWallEnabled }
        ensureAllWallInactivityOff()
        syncWallCursorToLatest()
        val baselineWalls = wallEventCount()

        val message = "manual wall ${System.currentTimeMillis()}"
        val observedBanner = AtomicBoolean(false)
        lateinit var observer: Job
        composeRule.runOnUiThread {
            observer = CoroutineScope(Dispatchers.Main.immediate).launch(start = CoroutineStart.UNDISPATCHED) {
                appViewModel().state.collect { current ->
                    if (current.bannerStatus?.message?.contains(message) == true) observedBanner.set(true)
                }
            }
        }
        try {
            sendWallViaHarness(message)
            waitUntilNoError(5_000L) { wallEventCount() == baselineWalls + 1 }
            // Count queries may span several pages while live frames clear
            // status; the exact fresh banner must have been delivered meanwhile.
            waitUntilNoError(5_000L) { observedBanner.get() }
            SystemClock.sleep(3_000L)
            assertNoWallNotificationAutogroupSummary()
            val visibleMessages = wallNotifications().map { wallNotificationFullText(it) }
            assertFalse(
                "foreground wall notification appeared while background wall was disabled: $visibleMessages",
                visibleMessages.contains(message),
            )
        } finally {
            observer.cancel()
        }
    }

    @Test
    fun background_manual_wall_delivery_posts_system_notification() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        clearAppNotifications()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        ensureAllWallInactivityOff()
        syncWallCursorToLatest()

        composeRule.runOnUiThread {
            appViewModel().setBackgroundWallEnabled(true)
        }
        composeRule.waitForIdle()
        waitUntilNoError(5_000L) { appViewModel().state.value.backgroundWallEnabled }

        backgroundActivity()
        waitUntilDeviceCondition(10_000L, "background wall service notification") {
            activeNotifications().any { it.notification.channelId == "lingon_background_wall" }
        }

        val message = "background manual wall ${System.currentTimeMillis()}"
        sendWallViaHarness(message)

        waitForWallNotificationBody(message, "background manual wall notification")
        assertNoWallNotificationAutogroupSummary()
        assertSingleWallNotificationBody(message)
        val wallNotification = wallNotifications()
            .firstOrNull { wallNotificationFullText(it) == message }
            ?: throw AssertionError("missing lingon_wall notification for message=$message")
        assertTrue(
            "wall notification replacements must not alert again: flags=${wallNotification.notification.flags}",
            wallNotification.notification.flags and Notification.FLAG_ONLY_ALERT_ONCE != 0,
        )
        assertTrue(
            "wall notification should not be grouped: group=${wallNotification.notification.group.orEmpty()}",
            wallNotification.notification.group.isNullOrBlank(),
        )
        val title = wallNotificationTitle(wallNotification)
        val text = wallNotificationFullText(wallNotification)
        assertTrue("notification title missing username: $title", title.startsWith("${testConfig.username}@"))
        assertFalse("manual wall title should not include blank session suffix: $title", title.endsWith("#"))
        assertEquals(message, text)
    }

    @Test
    fun background_distinct_wall_messages_post_distinct_system_notifications() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        clearAppNotifications()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        ensureAllWallInactivityOff()
        syncWallCursorToLatest()

        composeRule.runOnUiThread {
            appViewModel().setBackgroundWallEnabled(true)
        }
        composeRule.waitForIdle()
        waitUntilNoError(5_000L) { appViewModel().state.value.backgroundWallEnabled }

        backgroundActivity()
        waitUntilDeviceCondition(10_000L, "background wall service notification") {
            activeNotifications().any { it.notification.channelId == "lingon_background_wall" }
        }

        val suffix = System.currentTimeMillis()
        val firstMessage = "background distinct wall first $suffix"
        val secondMessage = "background distinct wall second $suffix"
        sendWallViaHarness(firstMessage)
        waitForWallNotificationBody(firstMessage, "first distinct background wall notification")
        sendWallViaHarness(secondMessage)
        waitForWallNotificationBody(
            secondMessage,
            "second distinct background wall notification",
            clearUnexpectedWallNotifications = false,
        )

        val notifications = wallNotifications()
            .filter {
                val text = wallNotificationFullText(it)
                text == firstMessage || text == secondMessage
            }
        val visibleMessages = notifications.map { wallNotificationFullText(it) }
        assertEquals(
            "distinct wall messages must remain separate notifications so Android can alert each event",
            listOf(firstMessage, secondMessage).sorted(),
            visibleMessages.sorted(),
        )
        assertEquals(
            "distinct wall messages must use distinct notification ids",
            notifications.size,
            notifications.map { it.id }.distinct().size,
        )
        assertEquals(
            "distinct wall messages must use distinct notification tags",
            notifications.size,
            notifications.map { it.tag }.distinct().size,
        )
        notifications.forEach { wallNotification ->
            assertTrue(
                "wall notification retries must still be update-only for id=${wallNotification.id}",
                wallNotification.notification.flags and Notification.FLAG_ONLY_ALERT_ONCE != 0,
            )
        }
    }

    @Test
    fun foreground_resume_suppresses_background_wall_notifications() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        clearAppNotifications()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        ensureAllWallInactivityOff()
        syncWallCursorToLatest()

        composeRule.runOnUiThread {
            appViewModel().setBackgroundWallEnabled(true)
        }
        composeRule.waitForIdle()
        waitUntilNoError(5_000L) { appViewModel().state.value.backgroundWallEnabled }

        backgroundActivity()
        waitUntilDeviceCondition(10_000L, "background wall service notification") {
            activeNotifications().any { it.notification.channelId == "lingon_background_wall" }
        }

        resumeActivity()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        clearWallNotifications()

        val wallEventsBefore = wallEventCount()
        val message = "foreground suppressed wall ${System.currentTimeMillis()}"
        sendWallViaHarness(message)
        waitUntilNoError(BACKGROUND_WALL_NOTIFICATION_TIMEOUT_MS) {
            wallEventCount() > wallEventsBefore
        }
        Thread.sleep(3_000L)

        val visibleMessages = wallNotifications().map { wallNotificationFullText(it) }
        assertFalse(
            "wall notification appeared while app was foregrounded: $visibleMessages",
            visibleMessages.contains(message),
        )
    }

    @Test
    fun background_manual_wall_delivery_does_not_repost_previous_message() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        clearAppNotifications()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        ensureAllWallInactivityOff()
        syncWallCursorToLatest()

        composeRule.runOnUiThread {
            appViewModel().setBackgroundWallEnabled(true)
        }
        composeRule.waitForIdle()
        waitUntilNoError(5_000L) { appViewModel().state.value.backgroundWallEnabled }

        backgroundActivity()
        waitUntilDeviceCondition(10_000L, "background wall service notification") {
            activeNotifications().any { it.notification.channelId == "lingon_background_wall" }
        }

        val firstMessage = "background dedupe first wall ${System.currentTimeMillis()}"
        sendWallViaHarness(firstMessage)
        waitForWallNotificationBody(firstMessage, "first background wall notification")

        clearAppNotifications()
        waitUntilDeviceCondition(5_000L, "wall notifications cleared") { wallNotifications().isEmpty() }

        val secondMessage = "background dedupe second wall ${System.currentTimeMillis()}"
        sendWallViaHarness(secondMessage)
        waitUntilDeviceCondition(BACKGROUND_WALL_NOTIFICATION_TIMEOUT_MS, "second background wall notification") {
            wallNotifications().isNotEmpty()
        }

        val visibleMessages = wallNotifications().map { wallNotificationFullText(it) }
        assertFalse(
            "previous wall notification was reposted with the next wall message: $visibleMessages",
            visibleMessages.contains(firstMessage),
        )
        assertTrue(
            "second wall notification was not visible: $visibleMessages",
            visibleMessages.contains(secondMessage),
        )
        assertSingleWallNotificationBody(secondMessage)
    }

    @Test
    fun background_manual_wall_delivery_recovers_when_cursor_is_ahead_of_relay() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        clearAppNotifications()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        ensureAllWallInactivityOff()

        composeRule.runOnUiThread {
            appViewModel().setBackgroundWallEnabled(true)
        }
        composeRule.waitForIdle()
        waitUntilNoError(5_000L) { appViewModel().state.value.backgroundWallEnabled }

        backgroundActivity()
        waitUntilDeviceCondition(10_000L, "background wall service notification") {
            activeNotifications().any { it.notification.channelId == "lingon_background_wall" }
        }

        val latestBeforeReset = advanceWallCursorAheadOfRelay()
        waitForWallCursorAtMost(latestBeforeReset)
        clearWallNotifications()
        syncWallCursorToLatest()

        val message = "background wall cursor reset ${System.currentTimeMillis()}"
        sendWallViaHarness(message)

        waitForWallNotificationBody(message, "background cursor reset wall notification")
        val wallNotification = wallNotifications()
            .firstOrNull { wallNotificationFullText(it) == message }
            ?: throw AssertionError("missing lingon_wall notification for message=$message")
        assertEquals(message, wallNotificationFullText(wallNotification))
    }

    @Test
    fun background_wall_delivery_stops_without_notification_permission_and_recovers_after_grant() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        clearAppNotifications()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        ensureAllWallInactivityOff()
        syncWallCursorToLatest()

        composeRule.runOnUiThread {
            appViewModel().setBackgroundWallEnabled(true)
        }
        composeRule.waitForIdle()
        waitUntilNoError(5_000L) { appViewModel().state.value.backgroundWallEnabled }

        backgroundActivity()
        waitUntilDeviceCondition(10_000L, "background wall service notification") {
            activeNotifications().any { it.notification.channelId == "lingon_background_wall" }
        }

        val deliveryDisabled = setNotificationDeliveryEnabled(false)
        assertTrue(
            "notification gating failed: enabled=${notificationsEnabled()}, appops=" +
                shellCommandOutput("cmd appops get --uid systems.pkt.lingon POST_NOTIFICATION"),
            deliveryDisabled,
        )
        assertEquals(
            android.content.pm.PackageManager.PERMISSION_DENIED,
            InstrumentationRegistry.getInstrumentation().targetContext
                .checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS),
        )
        clearWallNotifications()

        val blockedMessage = "permission blocked wall ${System.currentTimeMillis()}"
        sendWallViaHarness(blockedMessage)
        val app = composeRule.activity.application as LingonApplication
        var fetchedBlockedEvent = false
        val blockedPoll = runBlocking {
            app.wallDeliveryCoordinator.pollOnce(testConfig.endpoint, 100) { since, limit ->
                app.repository.listWallEvents(sinceId = since, limit = limit).also { page ->
                    fetchedBlockedEvent = page.events.any { it.message == blockedMessage }
                }
            }
        }
        assertTrue("blocked relay message was not fetched", fetchedBlockedEvent)
        assertEquals(systems.pkt.lingon.notifications.WallPollStatus.Blocked, blockedPoll.status)
        assertEquals("blocked delivery must retain its cursor", blockedPoll.since, blockedPoll.cursor)
        assertFalse(
            "wall notification should be suppressed when POST_NOTIFICATIONS is revoked",
            activeNotifications().any { it.notification.channelId == "lingon_wall" },
        )

        assertTrue("failed to re-enable notifications", setNotificationDeliveryEnabled(true))
        val recoveredMessage = "permission restored wall ${System.currentTimeMillis()}"
        sendWallViaHarness(recoveredMessage)
        waitForWallNotificationBody(
            recoveredMessage,
            "permission restored wall notification",
            clearUnexpectedWallNotifications = false,
        )
        assertNoWallNotificationAutogroupSummary()
        val wallNotification = wallNotifications()
            .firstOrNull { wallNotificationFullText(it) == recoveredMessage }
            ?: throw AssertionError("missing lingon_wall notification after restoring permission")
        val title = wallNotification.notification.extras
            .getCharSequence(Notification.EXTRA_TITLE)
            ?.toString()
            ?.trim()
            .orEmpty()
        val text = wallNotification.notification.extras
            .getCharSequence(Notification.EXTRA_TEXT)
            ?.toString()
            ?.trim()
            .orEmpty()
        assertTrue("notification title missing username: $title", title.startsWith("${testConfig.username}@"))
        assertEquals(recoveredMessage, text)

        resumeActivity()
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = SHORT_UI_TIMEOUT_MS)
    }

    @Test
    fun share_token_dialog_rejects_invalid_token() {
        openSettings()
        composeRule.onNodeWithTag(TestTags.ShareTokenButton).performClick()
        waitForTag(TestTags.ShareTokenInput)
        composeRule.onNodeWithTag(TestTags.ShareTokenInput).performTextReplacement("not-a-token")
        composeRule.onNodeWithTag(TestTags.ShareTokenAttach).performClick()
        waitForTag(TestTags.ShareTokenError)
        composeRule.onNodeWithText("Cancel").performClick()
        waitForTagToDisappear(TestTags.ShareTokenInput)
    }

    @Test
    fun obsolete_share_authentication_failure_preserves_new_connection_frames() {
        val token = testConfig.viewToken ?: throw AssertionError("missing share token")
        val session = testConfig.primaryUser().sessions.firstOrNull() ?: throw AssertionError("missing host")
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        val viewModel = appViewModel()
        val application = composeRule.activity.application as LingonApplication
        val original = application.wsClient
        val providerField = RelayWebSocketClient::class.java.getDeclaredField("httpClientProvider").apply { isAccessible = true }
        val provider = providerField.get(original) as systems.pkt.lingon.data.HttpClientProvider
        val field = AppViewModel::class.java.getDeclaredField("wsClient").apply { isAccessible = true }
        val entered = kotlinx.coroutines.CompletableDeferred<Unit>()
        val release = kotlinx.coroutines.CompletableDeferred<Unit>()
        val attempts = java.util.concurrent.atomic.AtomicInteger()
        val connects = java.util.concurrent.atomic.AtomicInteger()
        val gated = object : RelayWebSocketClient(provider) {
            override fun authenticateShareSession(baseUrl: okhttp3.HttpUrl, shareToken: String): systems.pkt.lingon.data.relay.AuthenticatedShareSession {
                if (attempts.incrementAndGet() == 1) {
                    entered.complete(Unit)
                    runBlocking { release.await() }
                    throw java.io.IOException("obsolete authentication failure")
                }
                return original.authenticateShareSession(baseUrl, shareToken)
            }
            override fun connect(options: ConnectOptions, listener: Listener): WebSocket {
                connects.incrementAndGet()
                return original.connect(options, listener)
            }
        }
        lateinit var initialJobs: Set<Job>
        lateinit var oldJobs: List<Job>
        composeRule.runOnIdle {
            initialJobs = viewModel.viewModelScope.coroutineContext[Job]!!.children.toSet()
            field.set(viewModel, gated)
            viewModel.handleSharedToken(token, endpointOverride = null)
        }
        try {
            waitUntilNoError(SHORT_UI_TIMEOUT_MS) { entered.isCompleted }
            composeRule.runOnIdle {
                oldJobs = viewModel.viewModelScope.coroutineContext[Job]!!.children.filter { it !in initialJobs }.toList()
                viewModel.onAppForeground()
            }
            waitForSessionTick(session)
            val epoch = viewModel.state.value.terminalConnectionEpoch
            release.complete(Unit)
            runBlocking { kotlinx.coroutines.withTimeout(15_000) { oldJobs.joinAll() } }
            composeRule.runOnIdle {
                assertEquals(ConnectionState.Connected, viewModel.state.value.connectionState)
                assertEquals(epoch, viewModel.state.value.terminalConnectionEpoch)
                assertNull(viewModel.state.value.shareTokenError)
                assertEquals(2, attempts.get())
                assertEquals(1, connects.get())
            }
            val previousSeq = viewModel.state.value.lastFrameSeq
            waitUntilNoError(SHORT_UI_TIMEOUT_MS) { viewModel.state.value.lastFrameSeq > previousSeq }
            assertEquals(ConnectionState.Connected, viewModel.state.value.connectionState)
        } finally {
            release.complete(Unit)
            composeRule.runOnIdle { field.set(viewModel, original) }
        }
    }

    @Test
    fun shared_text_finds_wrapped_valid_token_after_invalid_candidate() {
        assertSharedTextOpensWrappedToken("LGBinvalid\n")
    }

    @Test
    fun shared_text_with_many_invalid_prefixes_opens_wrapped_token() {
        assertSharedTextOpensWrappedToken("LGE".repeat(1000) + "\n")
    }

    private fun assertSharedTextOpensWrappedToken(prefix: String) {
        val token = testConfig.viewToken ?: throw AssertionError("missing share token")
        val session = testConfig.primaryUser().sessions.firstOrNull() ?: throw AssertionError("missing host")
        val parsed = systems.pkt.lingon.share.ShareTokens.parse(token) ?: throw AssertionError("invalid harness token")
        val embedded = systems.pkt.lingon.share.ShareTokens.encodeEmbedded(parsed.random, testConfig.endpoint) ?: throw AssertionError("cannot encode fixture")
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        val intent = android.content.Intent(composeRule.activity, ShareReceiverActivity::class.java).apply {
            action = android.content.Intent.ACTION_SEND
            type = "text/plain"
            putExtra(android.content.Intent.EXTRA_TEXT, prefix + embedded.chunked(17).joinToString("\n") + "\nShared from chat")
        }
        composeRule.runOnIdle { composeRule.activity.startActivity(intent) }
        waitForSessionTick(session)
        assertEquals(token, appViewModel().state.value.shareToken)
        assertEquals(session, appViewModel().state.value.activeSessionId)
        assertEquals(ConnectionState.Connected, appViewModel().state.value.connectionState)
    }

    @Test
    fun share_authentication_recovers_after_two_transient_failures() {
        val token = testConfig.viewToken ?: throw AssertionError("missing share token")
        val session = testConfig.primaryUser().sessions.firstOrNull() ?: throw AssertionError("missing host")
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        assertEquals(2, shareAuthFailuresViaHarness(2))
        try {
            // This is the only attach request. Subsequent authentication must
            // come from automatic reconnect, without foreground/manual recovery.
            attachViaShareToken(token)
            waitForSessionTick(session)
            assertEquals(0, shareAuthFailuresViaHarness())
            assertNull(appViewModel().state.value.shareTokenError)
            assertEquals(ConnectionState.Connected, appViewModel().state.value.connectionState)
        } finally {
            shareAuthFailuresViaHarness(0)
        }
    }

    @Test
    fun new_share_retries_authentication_after_last_shared_session_closes() {
        val replacementToken = testConfig.viewToken ?: throw AssertionError("missing replacement share")
        val replacementSession = testConfig.primaryUser().sessions.firstOrNull() ?: throw AssertionError("missing host")
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        val temporary = startHeadlessWithShareViaHarness()
        try {
            attachViaShareToken(temporary.second)
            waitUntilNoError(10_000L) {
                val state = appViewModel().state.value
                state.activeSessionId == temporary.first && state.connectionState == ConnectionState.Connected
            }
            waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
            detachHeadlessViaHarness(temporary.first)
            waitUntilNoError(20_000L) {
                val state = appViewModel().state.value
                state.activeSessionId == null && state.sessions.isEmpty()
            }
            assertEquals(2, shareAuthFailuresViaHarness(2))
            // One new share request must recover automatically after closure.
            attachViaShareToken(replacementToken)
            waitForSessionTick(replacementSession)
            assertEquals(0, shareAuthFailuresViaHarness())
            assertEquals(ConnectionState.Connected, appViewModel().state.value.connectionState)
            assertNull(appViewModel().state.value.shareTokenError)
        } finally {
            shareAuthFailuresViaHarness(0)
        }
    }

    private fun shareAuthFailuresViaHarness(count: Int? = null): Int {
        val query = count?.let { "?count=$it" }.orEmpty()
        val url = URL("${testConfig.endpoint.trimEnd('/')}/__harness/share-auth-failures$query")
        val connection = (url.openConnection() as HttpURLConnection).apply {
            requestMethod = if (count == null) "GET" else "POST"
            connectTimeout = 5_000
            readTimeout = 5_000
        }
        if (connection is javax.net.ssl.HttpsURLConnection && !testConfig.caPem.isNullOrBlank()) {
            connection.sslSocketFactory = trustContextFor(testConfig.caPem).socketFactory
        }
        try {
            assertEquals(HttpURLConnection.HTTP_OK, connection.responseCode)
            val body = connection.inputStream.bufferedReader().use { it.readText() }
            return org.json.JSONObject(body).getInt("remaining")
        } finally {
            connection.disconnect()
        }
    }

    @Test
    fun multi_user_sessions_are_isolated() {
        val secondary = testConfig.secondaryUser() ?: return
        val primarySession = testConfig.primaryUser().sessions.firstOrNull() ?: return
        val secondarySession = secondary.sessions.firstOrNull() ?: return
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithUser(testConfig.primaryUser())
        selectSessionTab(primarySession, timeoutMs = SHORT_UI_TIMEOUT_MS)
        assertTerminalResponsive(primarySession)
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) { snapshotContainsToken("TICK_$primarySession") }
        if (snapshotContainsToken("TICK_$secondarySession")) {
            throw AssertionError("primary user session leaked secondary output")
        }

        ensureLoggedOut()
        loginWithUser(secondary)
        selectSessionTab(secondarySession, timeoutMs = SHORT_UI_TIMEOUT_MS)
        assertTerminalResponsive(secondarySession)
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) { snapshotContainsToken("TICK_$secondarySession") }
        if (snapshotContainsToken("TICK_$primarySession")) {
            throw AssertionError("secondary user session leaked primary output")
        }
    }

    @Test
    fun share_tokens_are_isolated() {
        val primaryToken = testConfig.viewToken ?: return
        val secondary = testConfig.secondaryUser() ?: return
        val secondaryToken = secondary.viewToken ?: return
        val primarySession = testConfig.primaryUser().sessions.firstOrNull() ?: return
        val secondarySession = secondary.sessions.firstOrNull() ?: return
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        attachViaShareToken(primaryToken)
        waitForSessionTick(primarySession)
        if (snapshotContainsToken("TICK_$secondarySession")) {
            throw AssertionError("share token leaked secondary output")
        }

        attachViaShareToken(secondaryToken)
        waitForSessionTick(secondarySession)
        if (snapshotContainsToken("TICK_$primarySession")) {
            throw AssertionError("share token leaked primary output")
        }
    }

    @Test
    fun cleartext_share_authentication_uses_token_fallback() {
        org.junit.Assume.assumeTrue("requires LINGON_IT_CLEARTEXT=1", testConfig.endpoint.startsWith("http://"))
        val token = testConfig.viewToken ?: throw AssertionError("missing view token")
        val session = testConfig.primaryUser().sessions.firstOrNull() ?: throw AssertionError("missing shared host")
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        attachViaShareToken(token)
        waitForSessionTick(session)
        assertEquals(ConnectionState.Connected, appViewModel().state.value.connectionState)
        assertEquals(session, appViewModel().state.value.activeSessionId)
        assertNull(appViewModel().state.value.shareTokenError)
        assertFalse(appViewModel().state.value.shareCanControl)
    }

    private fun selectSessionTab(sessionId: String, timeoutMs: Long = 6_000L) {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            val info = readTerminalDebugInfo()
            if (info != null && info.state == "Connected" && info.activeSessionId == sessionId) {
                return
            }
            if (hasTag(TestTags.tabTag(sessionId))) {
                composeRule.onNodeWithTag(TestTags.tabTag(sessionId)).performClick()
                waitUntilNoError(timeoutMs) {
                    val refreshed = readTerminalDebugInfo()
                    refreshed != null &&
                        refreshed.state == "Connected" &&
                        refreshed.activeSessionId == sessionId
                }
                return
            }
            composeRule.waitForIdle()
            Thread.sleep(POLL_INTERVAL_MS)
        }
        throw AssertionError("Timed out waiting for session tab $sessionId")
    }

    private fun selectSessionForFixture(
        sessionId: String,
        timeoutMs: Long = 10_000L,
        expectedCols: Int? = null,
        expectedRows: Int? = null,
    ) {
        waitUntilNoError(timeoutMs) { appViewModel().state.value.sessions.any { it.id == sessionId } }
        composeRule.runOnIdle {
            appViewModel().selectSessionForTesting(sessionId)
        }
        waitUntilNoError(timeoutMs) {
            val info = readTerminalDebugInfo()
            info != null &&
                info.activeSessionId == sessionId &&
                !stateForTest().sessionSyncing &&
                (expectedCols == null || info.cols == expectedCols) &&
                (expectedRows == null || info.rows == expectedRows)
        }
    }

    private fun cleanupFixtureSessions(
        originalSessionId: String,
        originalCols: Int,
        originalRows: Int,
        fixtureSessionIds: List<String>,
    ) {
        var restoreError: Throwable? = null
        runCatching {
            selectSessionForFixture(originalSessionId, expectedCols = originalCols, expectedRows = originalRows)
        }.onFailure { restoreError = it }

        val stopError = runCatching {
            stopHostsViaHarness(fixtureSessionIds)
            if (fixtureSessionIds.isNotEmpty()) {
                waitUntilNoError(10_000L) { stateForTest().sessions.none { it.id in fixtureSessionIds } }
            }
        }.exceptionOrNull()

        if (restoreError != null) {
            val retryError = runCatching {
                selectSessionForFixture(originalSessionId, expectedCols = originalCols, expectedRows = originalRows)
            }.exceptionOrNull()
            if (retryError == null) {
                restoreError = null
            } else {
                restoreError.addSuppressed(retryError)
            }
        }
        if (stopError != null) {
            if (restoreError != null) {
                restoreError.addSuppressed(stopError)
            } else {
                throw stopError
            }
        }
        restoreError?.let { throw it }
    }

    private fun waitForSessionTick(sessionId: String, timeoutMs: Long = 10_000L) {
        waitForTerminalReady(timeoutMs)
        waitUntilNoError(timeoutMs) { snapshotContainsToken("TICK_$sessionId") }
    }

    private fun openSettings() {
        if (hasTag(TestTags.SettingsScreen)) return
        composeRule.onNodeWithTag(TestTags.TopBarSettingsButton).performClick()
        waitForTag(TestTags.SettingsScreen)
    }

    private fun closeSettingsIfOpen() {
        if (!hasTag(TestTags.SettingsScreen)) return
        composeRule.onNodeWithTag(TestTags.SettingsBackButton).performClick()
        waitForTagToDisappear(TestTags.SettingsScreen)
    }

    private fun recreateActivity() {
        composeRule.activityRule.scenario.recreate()
        composeRule.waitForIdle()
    }

    private fun backgroundAndResumeActivity() {
        backgroundActivity()
        resumeActivity()
    }

    private fun backgroundActivity() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        consumeShellCommand(
            instrumentation.uiAutomation.executeShellCommand("input keyevent KEYCODE_HOME"),
        )
        waitUntilDeviceCondition(5_000L, "app backgrounded") {
            !(composeRule.activity.application as LingonApplication).isAppInForeground()
        }
    }

    private fun resumeActivity() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        consumeShellCommand(
            instrumentation.uiAutomation.executeShellCommand(
                "am start -W -n systems.pkt.lingon/.MainActivity",
            ),
        )
        composeRule.waitForIdle()
        waitUntilDeviceCondition(5_000L, "app foregrounded") {
            (composeRule.activity.application as LingonApplication).isAppInForeground()
        }
    }

    private fun attachViaShareToken(token: String) {
        openSettings()
        composeRule.onNodeWithTag(TestTags.ShareTokenButton).performClick()
        waitForTag(TestTags.ShareTokenInput)
        composeRule.onNodeWithTag(TestTags.ShareTokenInput).performTextReplacement(token)
        composeRule.onNodeWithTag(TestTags.ShareTokenAttach).performClick()
        composeRule.runOnIdle {
            appViewModel().handleSharedToken(token, endpointOverride = null)
        }
        val ready = runCatching {
            waitForTerminalReady(timeoutMs = 30_000L)
        }.isSuccess
        if (!ready) {
            val info = readTerminalDebugInfo()
            val state = appViewModel().state.value
            throw AssertionError(
                "share token attach timeout: " +
                    "conn=${state.connectionState} " +
                    "shareTokenSet=${!state.shareToken.isNullOrBlank()} " +
                    "shareErr=${state.shareTokenError.orEmpty()} " +
                    "status=${state.status?.message.orEmpty()} " +
                    "rows=${info?.rows ?: 0} cols=${info?.cols ?: 0} " +
                "active=${info?.activeSessionId.orEmpty()}",
            )
        }
        closeSettingsIfOpen()
    }

    private fun setEndpoint(endpoint: String) {
        val app = composeRule.activity.application as LingonApplication
        runBlocking {
            app.repository.setEndpoint(endpoint)
            testConfig.caPem?.let { pem ->
                app.repository.addTrustedCertificates(endpoint, pem)
            }
        }
        composeRule.waitForIdle()
    }

    private fun wallEventCount(): Int {
        val app = composeRule.activity.application as LingonApplication
        return runBlocking {
            var total = 0
            var since = 0L
            var more = true
            while (more) {
                val page = app.repository.listWallEvents(sinceId = since, limit = 32)
                total += page.events.size
                more = page.hasMore
                if (more) {
                    assertTrue("wall pagination made no progress", page.nextId > since)
                    since = page.nextId
                }
            }
            total
        }
    }

    private fun clearAppNotifications() {
        notificationManager()?.cancelAll()
        waitUntilDeviceCondition(5_000L, "wall notifications cleared") {
            activeNotifications().none { it.notification.channelId == "lingon_wall" }
        }
    }

    private fun clearWallNotifications() {
        val manager = notificationManager() ?: return
        manager.activeNotifications
            .filter {
                it.notification.channelId == "lingon_wall" ||
                    it.notification.channelId == "lingon_background_wall"
            }
            .forEach { manager.cancelNotification(it) }
    }

    private fun syncWallCursorToLatest() {
        val app = composeRule.activity.application as LingonApplication
        val endpoint = appViewModel().state.value.endpoint.trim()
        if (endpoint.isBlank()) {
            return
        }
        val latest = runBlocking {
            var since = 0L
            while (true) {
                val page = app.repository.listWallEvents(sinceId = since, limit = 500)
                if (page.nextId > since) {
                    since = page.nextId
                }
                if (!page.hasMore) {
                    break
                }
            }
            since
        }
        runBlocking {
            app.wallWorkStateStore.advanceCursor(endpoint, latest)
        }
    }

    private fun advanceWallCursorAheadOfRelay(): Long {
        val app = composeRule.activity.application as LingonApplication
        val endpoint = appViewModel().state.value.endpoint.trim()
        if (endpoint.isBlank()) {
            return 0L
        }
        val latest = runBlocking {
            var since = 0L
            while (true) {
                val page = app.repository.listWallEvents(sinceId = since, limit = 500)
                if (page.nextId > since) {
                    since = page.nextId
                }
                if (!page.hasMore) {
                    break
                }
            }
            since
        }
        runBlocking {
            app.wallWorkStateStore.advanceCursor(endpoint, latest + 100)
        }
        return latest
    }

    private fun waitForWallCursorAtMost(maxCursor: Long) {
        val app = composeRule.activity.application as LingonApplication
        val endpoint = appViewModel().state.value.endpoint.trim()
        if (endpoint.isBlank()) {
            return
        }
        waitUntilDeviceCondition(BACKGROUND_WALL_NOTIFICATION_TIMEOUT_MS, "wall cursor <= $maxCursor") {
            runBlocking {
                app.wallWorkStateStore.loadCursor(endpoint) <= maxCursor
            }
        }
    }

    private fun waitUntilDeviceCondition(
        timeoutMs: Long,
        description: String,
        condition: () -> Boolean,
    ) {
        val deadline = SystemClock.uptimeMillis() + timeoutMs
        while (SystemClock.uptimeMillis() < deadline) {
            if (condition()) return
            Thread.sleep(POLL_INTERVAL_MS)
        }
        if (condition()) return
        val active = activeNotifications().joinToString(",") {
            val title = wallNotificationTitle(it)
            val text = wallNotificationFullText(it)
            "${it.notification.channelId}:${it.id}:$title:$text"
        }
        throw AssertionError("Timed out waiting for $description after ${timeoutMs}ms (notifications=[$active])")
    }

    private fun waitForWallNotificationBody(
        message: String,
        description: String,
        clearUnexpectedWallNotifications: Boolean = true,
    ) {
        waitUntilDeviceCondition(
            BACKGROUND_WALL_NOTIFICATION_TIMEOUT_MS,
            "$description '$message'",
        ) {
            val notifications = wallNotifications()
            if (notifications.any { wallNotificationFullText(it) == message }) {
                return@waitUntilDeviceCondition true
            }
            if (clearUnexpectedWallNotifications && notifications.isNotEmpty()) {
                clearWallNotifications()
            }
            false
        }
    }

    private fun assertSingleWallNotificationBody(message: String) {
        val matching = wallNotifications()
            .filter { wallNotificationFullText(it) == message }
        assertEquals(
            "expected one active wall notification for message=$message, got=${matching.size}",
            1,
            matching.size,
        )
    }

    private fun setNotificationDeliveryEnabled(enabled: Boolean): Boolean {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        if (enabled) {
            consumeShellCommand(instrumentation.uiAutomation.executeShellCommand(
                "pm grant systems.pkt.lingon android.permission.POST_NOTIFICATIONS",
            ))
            consumeShellCommand(instrumentation.uiAutomation.executeShellCommand(
                "cmd appops set --uid systems.pkt.lingon POST_NOTIFICATION allow",
            ))
        } else {
            revokeNotificationPermissionWithoutKillingInstrumentation()
        }
        val deadline = System.currentTimeMillis() + 5_000L
        while (System.currentTimeMillis() < deadline) {
            if (notificationsEnabled() == enabled) {
                return true
            }
            composeRule.waitForIdle()
            Thread.sleep(POLL_INTERVAL_MS)
        }
        return notificationsEnabled() == enabled
    }

    private fun revokeNotificationPermissionWithoutKillingInstrumentation() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val automation = instrumentation.uiAutomation
        // Android CTS uses this TestApi; regular revocation kills the test's UID.
        // Reflection keeps this instrumentation-only API out of production SDK code.
        val managerClass = Class.forName("android.permission.PermissionManager")
        val manager = instrumentation.targetContext.getSystemService(managerClass)
            ?: throw AssertionError("platform permission manager missing")
        val user = android.os.Process.myUserHandle()
        val userId = user.javaClass.getMethod("getIdentifier").invoke(user) as Int
        automation.adoptShellPermissionIdentity(
            "android.permission.REVOKE_POST_NOTIFICATIONS_WITHOUT_KILL",
            "android.permission.REVOKE_RUNTIME_PERMISSIONS",
        )
        try {
            managerClass.getMethod(
                "revokePostNotificationPermissionWithoutKillForTest",
                String::class.java,
                Integer.TYPE,
            ).invoke(manager, instrumentation.targetContext.packageName, userId)
        } finally {
            automation.dropShellPermissionIdentity()
        }
    }

    private fun restoreNotificationDelivery(): Boolean = setNotificationDeliveryEnabled(true)

    private fun notificationsEnabled(): Boolean {
        return NotificationManagerCompat.from(InstrumentationRegistry.getInstrumentation().targetContext)
            .areNotificationsEnabled()
    }

    private fun ensureWallInactivityOff() {
        waitUntilNoError(5_000L) { appViewModel().state.value.connectionState == ConnectionState.Connected }
        repeat(4) {
            val state = appViewModel().state.value
            if (!state.wallInactivityEnabled) {
                return
            }
            val previousLabel = state.wallInactivityLabel
            clickSettingsWallInactivity()
            waitUntilNoError(5_000L) {
                val updated = appViewModel().state.value
                !updated.wallInactivityEnabled || updated.wallInactivityLabel != previousLabel
            }
        }
        val state = appViewModel().state.value
        throw AssertionError(
            "failed to reset wall inactivity to off: enabled=${state.wallInactivityEnabled} label=${state.wallInactivityLabel}",
        )
    }

    private fun clickSettingsWallInactivity() {
        openSettings()
        composeRule.onNodeWithText("Wall inactivity")
            .performScrollTo()
            .performClick()
        closeSettingsIfOpen()
    }

    private fun ensureAllWallInactivityOff() {
        val sessions = testConfig.sessions.ifEmpty { listOf(activeSessionId()) }
        setWallInactivityViaHarness(sessions, enabled = false)
    }

    private fun activeNotifications(): Array<StatusBarNotification> {
        return notificationManager()?.activeNotifications ?: emptyArray()
    }

    private fun notificationManager(): NotificationManager? {
        return InstrumentationRegistry.getInstrumentation().targetContext
            .getSystemService(NotificationManager::class.java)
    }

    private fun NotificationManager.cancelNotification(notification: StatusBarNotification) {
        val tag = notification.tag
        if (tag != null) {
            cancel(tag, notification.id)
            return
        }
        cancel(notification.id)
    }

    private fun wallNotifications(): List<StatusBarNotification> {
        return activeNotifications()
            .filter {
                it.notification.channelId == "lingon_wall" &&
                    it.tag != "ranker_group"
            }
    }

    private fun wallNotificationTitle(notification: StatusBarNotification): String {
        return notification.notification.extras
            .getCharSequence(Notification.EXTRA_TITLE)
            ?.toString()
            ?.trim()
            .orEmpty()
    }

    private fun wallNotificationFullText(notification: StatusBarNotification): String {
        val extras = notification.notification.extras
        return (extras.getCharSequence(Notification.EXTRA_BIG_TEXT)
            ?: extras.getCharSequence(Notification.EXTRA_TEXT))
            ?.toString()
            ?.trim()
            .orEmpty()
    }

    private fun assertNoWallNotificationAutogroupSummary() {
        val summary = activeNotifications().firstOrNull {
            it.notification.channelId == "lingon_wall" &&
                it.tag == "ranker_group"
        }
        if (summary != null) {
            val title = summary.notification.extras
                .getCharSequence(Notification.EXTRA_TITLE)
                ?.toString()
                ?.trim()
                .orEmpty()
            val text = summary.notification.extras
                .getCharSequence(Notification.EXTRA_TEXT)
                ?.toString()
                ?.trim()
                .orEmpty()
            val notifications = activeNotifications()
                .filter { it.notification.channelId == "lingon_wall" }
                .joinToString { "${it.tag}:${it.id}:${wallNotificationFullText(it)}" }
            throw AssertionError("unexpected lingon_wall auto-group summary title=$title text=$text active=$notifications")
        }
    }

    private fun assertTerminalResponsive(sessionId: String? = null, requireControl: Boolean = true) {
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        val echoChar = "z"
        val echoHex = echoChar[0].code.toString(16).padStart(2, '0')
        val effectiveSessionId = sessionId ?: activeSessionId()
        if (requireControl) {
            waitUntilNoError(SHORT_UI_TIMEOUT_MS) {
                val info = readTerminalDebugInfo()
                info != null && info.hasControl
            }
        }
        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        composeRule.waitForIdle()
        val expected = "ECHO_${effectiveSessionId} $echoHex"
        var echoed = false
        repeat(3) {
            sendTerminalInput(echoChar)
            val success = runCatching {
                waitUntilNoError(5_000L) { snapshotContainsToken(expected) }
            }.isSuccess
            if (success) {
                echoed = true
                return@repeat
            }
            composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
            composeRule.waitForIdle()
        }
        if (!echoed) {
            waitUntilNoError(SHORT_UI_TIMEOUT_MS) { snapshotContainsToken(expected) }
        }
        val newlineEchoCr = "ECHO_${effectiveSessionId} 0d"
        val newlineEchoLf = "ECHO_${effectiveSessionId} 0a"
        var newlineEchoed = false
        repeat(2) {
            sendTerminalEnter()
            val success = runCatching {
                waitUntilNoError(5_000L) {
                    snapshotContainsToken(newlineEchoCr) || snapshotContainsToken(newlineEchoLf)
                }
            }.isSuccess
            if (success) {
                newlineEchoed = true
                return@repeat
            }
            composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
            composeRule.waitForIdle()
        }
        if (!newlineEchoed) {
            waitUntilNoError(SHORT_UI_TIMEOUT_MS) {
                snapshotContainsToken(newlineEchoCr) || snapshotContainsToken(newlineEchoLf)
            }
        }
    }

    private fun assertTerminalInputBlocked() {
        val nonce = appViewModel().state.value.localInputNonce
        val observedBlockedStatus = AtomicBoolean(false)
        val observedInputOrControl = AtomicBoolean(false)
        lateinit var observer: Job
        composeRule.runOnUiThread {
            // Discard attachment feedback: the attempted keys must produce a
            // fresh warning. Live terminal frames may then clear that warning.
            appViewModel().dismissStatus()
            observer = CoroutineScope(Dispatchers.Main.immediate).launch(start = CoroutineStart.UNDISPATCHED) {
                appViewModel().state.collect { current ->
                    if (current.status?.message == "view-only session") observedBlockedStatus.set(true)
                    if (current.localInputNonce != nonce || current.hasControl) observedInputOrControl.set(true)
                }
            }
        }
        try {
            composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
            sendTerminalInput("Z")
            sendTerminalEnter()
            composeRule.runOnIdle {
                val state = appViewModel().state.value
                assertEquals(nonce, state.localInputNonce)
                assertFalse(state.hasControl)
                assertTrue("attempted input produced no view-only warning", observedBlockedStatus.get())
                assertFalse("view-only input changed nonce or gained control", observedInputOrControl.get())
            }
        } finally {
            observer.cancel()
        }
    }

    private fun setResizeHostEnabled(enabled: Boolean) {
        composeRule.runOnUiThread {
            appViewModel().setResizeHostEnabledForTesting(enabled)
        }
        composeRule.waitForIdle()
    }

    private fun sendTerminalInput(text: String) {
        focusTerminalInput()
        text.forEach { ch ->
            val lower = ch.lowercaseChar()
            val (keyCode, shift) = when (lower) {
                '0' -> KeyEvent.KEYCODE_0 to false
                '1' -> KeyEvent.KEYCODE_1 to false
                '2' -> KeyEvent.KEYCODE_2 to false
                '3' -> KeyEvent.KEYCODE_3 to false
                '4' -> KeyEvent.KEYCODE_4 to false
                '5' -> KeyEvent.KEYCODE_5 to false
                '6' -> KeyEvent.KEYCODE_6 to false
                '7' -> KeyEvent.KEYCODE_7 to false
                '8' -> KeyEvent.KEYCODE_8 to false
                '9' -> KeyEvent.KEYCODE_9 to false
                '$' -> KeyEvent.KEYCODE_4 to true
                '(' -> KeyEvent.KEYCODE_9 to true
                ')' -> KeyEvent.KEYCODE_0 to true
                ';' -> KeyEvent.KEYCODE_SEMICOLON to false
                '|' -> KeyEvent.KEYCODE_BACKSLASH to true
                '-' -> KeyEvent.KEYCODE_MINUS to false
                'a' -> KeyEvent.KEYCODE_A to false
                'b' -> KeyEvent.KEYCODE_B to false
                'c' -> KeyEvent.KEYCODE_C to false
                'd' -> KeyEvent.KEYCODE_D to false
                'e' -> KeyEvent.KEYCODE_E to false
                'f' -> KeyEvent.KEYCODE_F to false
                'g' -> KeyEvent.KEYCODE_G to false
                'h' -> KeyEvent.KEYCODE_H to false
                'i' -> KeyEvent.KEYCODE_I to false
                'j' -> KeyEvent.KEYCODE_J to false
                'k' -> KeyEvent.KEYCODE_K to false
                'l' -> KeyEvent.KEYCODE_L to false
                'm' -> KeyEvent.KEYCODE_M to false
                'n' -> KeyEvent.KEYCODE_N to false
                'o' -> KeyEvent.KEYCODE_O to false
                'p' -> KeyEvent.KEYCODE_P to false
                'q' -> KeyEvent.KEYCODE_Q to false
                'r' -> KeyEvent.KEYCODE_R to false
                's' -> KeyEvent.KEYCODE_S to false
                't' -> KeyEvent.KEYCODE_T to false
                'u' -> KeyEvent.KEYCODE_U to false
                'v' -> KeyEvent.KEYCODE_V to false
                'w' -> KeyEvent.KEYCODE_W to false
                'x' -> KeyEvent.KEYCODE_X to false
                'y' -> KeyEvent.KEYCODE_Y to false
                'z' -> KeyEvent.KEYCODE_Z to false
                ' ' -> KeyEvent.KEYCODE_SPACE to false
                else -> throw IllegalArgumentException("unsupported terminal test character: $ch")
            }
            sendKeyStroke(keyCode, shift || ch.isUpperCase())
        }
    }

    private fun sendKeyStroke(keyCode: Int, shift: Boolean = false) {
        sendModifiedKeyStroke(keyCode, shift = shift)
    }

    private fun sendModifiedKeyStroke(
        keyCode: Int,
        ctrl: Boolean = false,
        alt: Boolean = false,
        shift: Boolean = false,
    ) {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val metaState = (if (ctrl) KeyEvent.META_CTRL_ON or KeyEvent.META_CTRL_LEFT_ON else 0) or
            (if (alt) KeyEvent.META_ALT_ON or KeyEvent.META_ALT_LEFT_ON else 0) or
            (if (shift) KeyEvent.META_SHIFT_ON or KeyEvent.META_SHIFT_LEFT_ON else 0)
        if (ctrl) {
            instrumentation.sendKeySync(KeyEvent(KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_CTRL_LEFT))
        }
        if (alt) {
            instrumentation.sendKeySync(KeyEvent(KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_ALT_LEFT))
        }
        if (shift) {
            instrumentation.sendKeySync(KeyEvent(KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_SHIFT_LEFT))
        }
        val downTime = SystemClock.uptimeMillis()
        instrumentation.sendKeySync(KeyEvent(downTime, downTime, KeyEvent.ACTION_DOWN, keyCode, 0, metaState))
        instrumentation.sendKeySync(KeyEvent(downTime, SystemClock.uptimeMillis(), KeyEvent.ACTION_UP, keyCode, 0, metaState))
        if (shift) {
            instrumentation.sendKeySync(KeyEvent(KeyEvent.ACTION_UP, KeyEvent.KEYCODE_SHIFT_LEFT))
        }
        if (alt) {
            instrumentation.sendKeySync(KeyEvent(KeyEvent.ACTION_UP, KeyEvent.KEYCODE_ALT_LEFT))
        }
        if (ctrl) {
            instrumentation.sendKeySync(KeyEvent(KeyEvent.ACTION_UP, KeyEvent.KEYCODE_CTRL_LEFT))
        }
    }

    private fun sendTerminalEnter() {
        focusTerminalInput()
        InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(KeyEvent.KEYCODE_ENTER)
    }

    private fun sendTerminalBackspace() {
        focusTerminalInput()
        InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(KeyEvent.KEYCODE_DEL)
    }

    private fun focusTerminalInput() {
        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        composeRule.waitForIdle()
    }

    private fun setEndpointViaUi(endpoint: String) {
        openSettings()
        composeRule.onNodeWithTag(TestTags.EndpointButton).performClick()
        waitForTag(TestTags.EndpointInput)
        composeRule.onNodeWithTag(TestTags.EndpointInput).performTextReplacement(endpoint)
        composeRule.onNodeWithTag(TestTags.EndpointSave).performClick()
        waitForTagToDisappear(TestTags.EndpointInput)
        closeSettingsIfOpen()
    }

    private fun configureEndpointAndCerts() {
        setEndpoint(testConfig.endpoint)
    }

    private fun ensureLoggedOut() {
        if (hasTag(TestTags.LoginUsername)) return
        composeRule.runOnUiThread {
            appViewModel().logout()
        }
        waitForTag(TestTags.LoginUsername, timeoutMs = 10_000L)
    }

    private fun appViewModel(): AppViewModel {
        val app = composeRule.activity.application as LingonApplication
        val factory = AppViewModelFactory(
            app.repository,
            app.wsClient,
            app.wallDeliveryCoordinator,
            app.wallWorkScheduler,
            app.backgroundWallServiceController,
        )
        return ViewModelProvider(composeRule.activity, factory)[AppViewModel::class.java]
    }

    private fun loginWithConfiguredUser() {
        if (hasTag(TestTags.TerminalInput)) return
        loginWithUser(testConfig.primaryUser())
    }

    private fun loginWithConfiguredUserNoTerminal() {
        if (appViewModel().state.value.loggedIn) return
        val user = testConfig.primaryUser()
        attemptLogin(user.username, user.password, generateTotp(user.totpSecret))
        waitForLoginSuccess(timeoutMs = LOGIN_TIMEOUT_MS)
    }

    private fun loginWithUser(user: UserConfig) {
        if (hasTag(TestTags.TerminalInput)) return
        attemptLogin(user.username, user.password, generateTotp(user.totpSecret))
        waitForLoginSuccess(timeoutMs = LOGIN_TIMEOUT_MS)
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
    }

    private fun attemptLogin(username: String, password: String, totp: String) {
        waitForTag(TestTags.LoginUsername)
        composeRule.onNodeWithTag(TestTags.LoginUsername).performTextReplacement(username)
        composeRule.onNodeWithTag(TestTags.LoginPassword).performTextReplacement(password)
        composeRule.onNodeWithTag(TestTags.LoginTotp).performTextReplacement(totp)
        composeRule.onNodeWithTag(TestTags.LoginSubmit).performClick()
    }

    private fun waitForLoginSuccess(timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        waitUntilNoError(timeoutMs) { appViewModel().state.value.loggedIn }
    }

    private fun waitForTag(tag: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        waitUntil(timeoutMs) { hasTag(tag) }
    }

    private fun waitForTagNoError(tag: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        waitUntilNoError(timeoutMs) { hasTag(tag) }
    }

    private fun waitForTagToDisappear(tag: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        waitUntil(timeoutMs) { !hasTag(tag) }
    }

    private fun hasTextNode(text: String): Boolean {
        return runCatching {
            composeRule.onAllNodesWithText(text, substring = false).fetchSemanticsNodes().isNotEmpty()
        }.getOrDefault(false)
    }

    private fun hasTag(tag: String): Boolean {
        return composeRule.onAllNodesWithTag(tag).fetchSemanticsNodes().isNotEmpty()
    }

    private fun isNodeEnabled(tag: String): Boolean {
        val node = try {
            composeRule.onNodeWithTag(tag).fetchSemanticsNode()
        } catch (err: AssertionError) {
            composeRule.onNodeWithTag(tag, useUnmergedTree = true).fetchSemanticsNode()
        }
        return !node.config.contains(SemanticsProperties.Disabled)
    }

    private fun nodeBounds(tag: String): Rect {
        return composeRule.onNodeWithTag(tag).fetchSemanticsNode().boundsInRoot
    }

    private fun nodeBoundsOrNull(tag: String): Rect? {
        return composeRule.onAllNodesWithTag(tag).fetchSemanticsNodes().firstOrNull()?.boundsInRoot
    }

    private fun clickNodeCenter(tag: String) {
        val node = composeRule.onNodeWithTag(tag).fetchSemanticsNode()
        val bounds = node.boundsInRoot
        composeRule.onNodeWithTag(tag).performTouchInput {
            down(0, Offset(bounds.width / 2f, bounds.height / 2f))
            up(0)
        }
    }

    private fun clickRenderedObjectOrClickableAncestor(node: UiObject2) {
        var current: UiObject2? = node
        while (current != null && !current.isClickable) {
            current = current.parent
        }
        (current ?: node).click()
    }

    private fun TerminalDebugInfo.containsToken(token: String): Boolean {
        return cursorLine.contains(token) ||
            prevLine.contains(token) ||
            tail.contains(token) ||
            row0.contains(token)
    }

    private fun readTerminalHash(): Long {
        val info = readTerminalDebugInfo()
        return info?.hash ?: 0L
    }

    private fun assertLiveBottomAnchored(label: String) {
        val info = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info for $label")
        var viewport: TerminalViewportState? = null
        var diagnostics: String? = null
        composeRule.runOnIdle {
            val view = findTerminalView() as? TerminalGridView
            viewport = view?.captureViewportState()
            diagnostics = view?.getCameraDiagnosticsForTesting()
        }
        assertTrue(
            "$label should align live bottom: visible=${info.visibleStartRow}-${info.visibleEndRowExclusive} rows=${info.rows} " +
                "camera=${info.cameraOffsetYPx} viewport=${info.viewportHeightPx} cell=${info.scaledCellHeightPx} saved=$viewport $diagnostics",
            info.isLiveBottomAnchored(),
        )
    }

    private fun assertTerminalViewportAboveQuickKeys(label: String) {
        composeRule.waitForIdle()
        val terminal = nodeBounds(TestTags.TerminalList)
        val quickKeys = nodeBounds(TestTags.TerminalQuickKeys)
        assertTrue(
            "$label terminal viewport overlaps quick keys: terminal=$terminal quickKeys=$quickKeys",
            terminal.bottom <= quickKeys.top + 0.5f,
        )
    }

    private fun assertTerminalViewportAboveIme(label: String) {
        composeRule.waitForIdle()
        val imeBottom = readImeBottomInsetPx()
        assertTrue("$label expected real IME inset to be visible", imeBottom > 0)
        val rootHeight = readRootViewHeightPx()
        val imeTop = rootHeight - imeBottom
        val terminal = nodeBounds(TestTags.TerminalList)
        val quickKeys = nodeBounds(TestTags.TerminalQuickKeys)
        assertTrue(
            "$label terminal viewport overlaps real IME: terminal=$terminal imeTop=$imeTop imeBottom=$imeBottom rootHeight=$rootHeight",
            terminal.bottom <= imeTop + 0.5f,
        )
        assertTrue(
            "$label quick keys overlap real IME: quickKeys=$quickKeys imeTop=$imeTop imeBottom=$imeBottom rootHeight=$rootHeight",
            quickKeys.bottom <= imeTop + 0.5f,
        )
    }

    private fun assertTerminalCameraUsesVisibleImeViewport(label: String) {
        composeRule.waitForIdle()
        val imeBottom = readImeBottomInsetPx()
        assertTrue("$label expected real IME inset to be visible", imeBottom > 0)
        val imeTop = readSystemVisibleBottomPx()
        val terminal = nodeBounds(TestTags.TerminalList)
        val info = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info for $label")
        val expectedVisibleHeight = (imeTop - terminal.top).coerceAtLeast(0f)
        assertTrue(
            "$label terminal camera viewport must be the visible area above IME: " +
                "viewport=${info.viewportHeightPx} expectedVisibleHeight=$expectedVisibleHeight terminal=$terminal imeTop=$imeTop",
            info.viewportHeightPx <= kotlin.math.ceil(expectedVisibleHeight).toInt() + 1,
        )
    }

    private fun assertTerminalCameraUsesVisibleSystemViewport(label: String) {
        composeRule.waitForIdle()
        val visibleBottom = readSystemVisibleBottomPx()
        val terminal = nodeBounds(TestTags.TerminalList)
        val info = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info for $label")
        val expectedVisibleHeight = (visibleBottom - terminal.top).coerceAtLeast(0f)
        assertTrue(
            "$label terminal camera viewport must be bounded by the visible system area: " +
                "viewport=${info.viewportHeightPx} expectedVisibleHeight=$expectedVisibleHeight terminal=$terminal " +
                "visibleBottom=$visibleBottom imeBottom=${readImeBottomInsetPx()} navBottom=${readNavigationBottomInsetPx()}",
            info.viewportHeightPx <= kotlin.math.ceil(expectedVisibleHeight).toInt() + 1,
        )
    }

    private fun assertTerminalViewportStable(label: String) {
        val initialTerminal = nodeBounds(TestTags.TerminalList)
        val initialQuickKeys = nodeBoundsOrNull(TestTags.TerminalQuickKeys)
        val initialInfo = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info before stability check for $label")
        val deadline = System.currentTimeMillis() + 1_200L
        while (System.currentTimeMillis() < deadline) {
            safeWaitForIdle()
            val terminal = nodeBounds(TestTags.TerminalList)
            val quickKeys = nodeBoundsOrNull(TestTags.TerminalQuickKeys)
            val info = readTerminalDebugInfo()
                ?: throw AssertionError("missing terminal debug info during stability check for $label")
            assertEquals("$label terminal bounds flickered", initialTerminal, terminal)
            assertEquals("$label quick-key bounds flickered", initialQuickKeys, quickKeys)
            assertTrue(
                "$label camera moved during stable IME viewport check: before=${initialInfo.cameraOffsetYPx} after=${info.cameraOffsetYPx}",
                kotlin.math.abs(initialInfo.cameraOffsetYPx - info.cameraOffsetYPx) <= maxOf(1f, info.scaledCellHeightPx * 0.1f),
            )
            Thread.sleep(POLL_INTERVAL_MS)
        }
    }

    private fun waitForViewLayout(view: View) {
        waitUntilWithoutCompose(timeoutMs = 10_000L) {
            view.width > 0 && view.height > 0 && view.isAttachedToWindow
        }
    }

    private fun showKeyboard(input: EditText) {
        composeRule.runOnUiThread {
            input.requestFocus()
            composeRule.activity.getSystemService(InputMethodManager::class.java)
                .showSoftInput(input, InputMethodManager.SHOW_IMPLICIT)
        }
    }

    private fun hideKeyboard(input: EditText) {
        composeRule.runOnUiThread {
            composeRule.activity.getSystemService(InputMethodManager::class.java)
                .hideSoftInputFromWindow(input.windowToken, 0)
            input.clearFocus()
        }
    }

    private fun waitForImeOverlay(view: TerminalGridView) {
        waitUntilWithoutCompose(timeoutMs = 15_000L) {
            val imeBottom = readImeBottomInsetPx()
            val rootHeight = readRootViewHeightPx()
            imeBottom > readNavigationBottomInsetPx() &&
                view.height > 0 &&
                view.getViewportHeightForTesting() < view.getPhysicalHeightForTesting() &&
                viewBottomInRoot(view) > rootHeight - imeBottom
        }
    }

    private fun waitForImeHidden() {
        waitUntilWithoutCompose(timeoutMs = 10_000L) {
            readImeBottomInsetPx() <= readNavigationBottomInsetPx()
        }
    }

    private fun assertOverlayCameraBottomAnchored(label: String, view: TerminalGridView) {
        val cellHeight = view.getScaledCellHeightForTesting()
        val viewportHeight = view.getViewportHeightForTesting()
        val expected = maxOf(0f, (240 * cellHeight) - viewportHeight.toFloat())
        assertTrue("$label cell height was not measured", cellHeight > 0f)
        assertEquals(
            "$label should use effective viewport height for live-bottom camera anchoring",
            expected,
            view.getCameraOffsetYForTesting(),
            maxOf(1f, cellHeight * 0.1f),
        )
        assertEquals("$label should render through the terminal bottom", 240, view.getVisibleEndRowExclusive())
    }

    private fun assertOverlayCameraUsesVisibleSystemViewport(label: String, view: TerminalGridView) {
        val visibleBottom = readSystemVisibleBottomPx()
        val rootLocation = IntArray(2)
        val viewLocation = IntArray(2)
        composeRule.activity.window.decorView.rootView.getLocationInWindow(rootLocation)
        view.getLocationInWindow(viewLocation)
        val viewTopInRoot = viewLocation[1] - rootLocation[1]
        val expectedVisibleHeight = (visibleBottom - viewTopInRoot).coerceAtLeast(0)
        assertTrue(
            "$label overlay camera viewport must be bounded by the visible system area: " +
                "viewport=${view.getViewportHeightForTesting()} expectedVisibleHeight=$expectedVisibleHeight " +
                "viewHeight=${view.getPhysicalHeightForTesting()} visibleBottom=$visibleBottom " +
                "imeBottom=${readImeBottomInsetPx()} navBottom=${readNavigationBottomInsetPx()}",
            view.getViewportHeightForTesting() <= expectedVisibleHeight + 1,
        )
    }

    private fun assertOverlayTerminalViewportDoesNotFlicker(label: String, view: TerminalGridView) {
        val initial = overlayViewportSample(view)
        assertTrue("$label initial sample is not bottom anchored: $initial", initial.isBottomAnchored())
        val deadline = System.currentTimeMillis() + 1_200L
        while (System.currentTimeMillis() < deadline) {
            composeRule.runOnUiThread {
                view.draw(Canvas(Bitmap.createBitmap(view.width, view.height, Bitmap.Config.ARGB_8888)))
            }
            val sample = overlayViewportSample(view)
            assertEquals("$label physical height flickered", initial.physicalHeightPx, sample.physicalHeightPx)
            assertEquals("$label effective viewport height flickered", initial.viewportHeightPx, sample.viewportHeightPx)
            assertEquals("$label visible start row flickered", initial.visibleStartRow, sample.visibleStartRow)
            assertEquals("$label visible end row flickered", initial.visibleEndRowExclusive, sample.visibleEndRowExclusive)
            assertEquals(
                "$label camera Y flickered",
                initial.cameraOffsetYPx,
                sample.cameraOffsetYPx,
                maxOf(1f, initial.scaledCellHeightPx * 0.1f),
            )
            assertTrue("$label lost bottom anchoring during settle window: $sample", sample.isBottomAnchored())
            Thread.sleep(POLL_INTERVAL_MS)
        }
    }

    private fun overlayViewportSample(view: TerminalGridView): OverlayViewportSample {
        var sample: OverlayViewportSample? = null
        composeRule.runOnUiThread {
            sample = OverlayViewportSample(
                physicalHeightPx = view.getPhysicalHeightForTesting(),
                viewportHeightPx = view.getViewportHeightForTesting(),
                scaledCellHeightPx = view.getScaledCellHeightForTesting(),
                cameraOffsetYPx = view.getCameraOffsetYForTesting(),
                visibleStartRow = view.getVisibleStartRow(),
                visibleEndRowExclusive = view.getVisibleEndRowExclusive(),
            )
        }
        return sample ?: throw AssertionError("missing overlay viewport sample")
    }

    private fun viewBottomInRoot(view: View): Int {
        val rootLocation = IntArray(2)
        val viewLocation = IntArray(2)
        composeRule.activity.window.decorView.rootView.getLocationInWindow(rootLocation)
        view.getLocationInWindow(viewLocation)
        return viewLocation[1] - rootLocation[1] + view.height
    }

    private fun readTerminalDebugInfo(): TerminalDebugInfo? {
        var info: TerminalDebugInfo? = null
        composeRule.runOnIdle {
            val state = stateForTest()
            val snap = state.activeSnapshot
            val terminalView = findTerminalView() as? systems.pkt.lingon.terminal.TerminalGridView
            val rows = snap?.rows ?: 0
            val cols = snap?.cols ?: 0
            val row0 = snap?.let { snapshotRow(it, 0).trimEnd() } ?: ""
            val cursorLine = snap?.let {
                if (it.rows <= 0) {
                    ""
                } else {
                    snapshotRow(it, it.cursorY.coerceIn(0, it.rows - 1)).trimEnd()
                }
            }.orEmpty()
            val prevLine = snap?.let {
                if (it.rows <= 0) {
                    ""
                } else {
                    val prevRow = (it.cursorY - 1).coerceIn(0, it.rows - 1)
                    snapshotRow(it, prevRow).trimEnd()
                }
            }.orEmpty()
            val tail = snap?.let {
                if (it.rows <= 0) {
                    ""
                } else {
                    val last = it.rows - 1
                    val start = (last - 2).coerceAtLeast(0)
                    buildString {
                        for (row in start..last) {
                            if (row > start) append(" | ")
                            append(snapshotRow(it, row).trimEnd())
                        }
                    }
                }
            }.orEmpty()
            info = TerminalDebugInfo(
                rows = rows,
                cols = cols,
                state = state.connectionState.toString(),
                activeSessionId = state.activeSessionId.orEmpty(),
                lastFrameSeq = state.lastFrameSeq,
                lastFrameType = state.lastFrameType,
                scrollbackOffsetRows = state.scrollbackOffsetRows,
                hash = snapshotHash(snap),
                row0 = row0,
                prevLine = prevLine,
                tail = tail,
                cursorLine = cursorLine,
                cursorY = snap?.cursorY ?: 0,
                hasControl = state.hasControl,
                resizeEnabled = state.resizeHostEnabled,
                viewCols = terminalView?.getViewCols() ?: state.terminalCols,
                viewRows = terminalView?.getViewRows() ?: state.terminalRows,
                renderScaleX = terminalView?.getRenderScaleX() ?: 0f,
                renderScaleY = terminalView?.getRenderScaleY() ?: 0f,
                cameraOffsetYPx = terminalView?.getCameraOffsetYForTesting() ?: 0f,
                viewportHeightPx = terminalView?.getViewportHeightForTesting() ?: 0,
                physicalHeightPx = terminalView?.getPhysicalHeightForTesting() ?: 0,
                scaledCellHeightPx = terminalView?.getScaledCellHeightForTesting() ?: 0f,
                visibleStartRow = terminalView?.getVisibleStartRow() ?: 0,
                visibleEndRowExclusive = terminalView?.getVisibleEndRowExclusive() ?: 0,
                zoomFactor = state.zoomFactor,
            )
        }
        return info
    }

    private fun terminalViewCameraSnapshot(label: String): TerminalCameraSnapshot {
        var snapshot: TerminalCameraSnapshot? = null
        composeRule.runOnIdle {
            val terminalView = findTerminalView() as? TerminalGridView
                ?: throw AssertionError("missing terminal view for $label")
            snapshot = TerminalCameraSnapshot(
                cameraOffsetYPx = terminalView.getCameraOffsetYForTesting(),
                scaledCellHeightPx = terminalView.getScaledCellHeightForTesting(),
                visibleStartRow = terminalView.getVisibleStartRow(),
            )
        }
        return snapshot ?: throw AssertionError("missing terminal camera snapshot for $label")
    }

    private fun waitForTerminalViewReady() {
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) {
            val view = findTerminalView() as? TerminalGridView
            view != null &&
                view.width > 0 &&
                view.height > 0 &&
                view.getScaledCellHeightForTesting() > 0f &&
                view.getVisibleEndRowExclusive() > 0
        }
    }

    private fun waitForTerminalDebugInfo(
        timeoutMs: Long = SHORT_UI_TIMEOUT_MS,
        predicate: (TerminalDebugInfo) -> Boolean,
    ): TerminalDebugInfo? {
        var match: TerminalDebugInfo? = null
        waitUntilNoError(timeoutMs) {
            val info = readTerminalDebugInfo()
            if (info != null && predicate(info)) {
                match = info
                true
            } else {
                false
            }
        }
        return match
    }

    private fun readTerminalRenderInfo(): TerminalRenderInfo? {
        var info: TerminalRenderInfo? = null
        composeRule.runOnIdle {
            val terminalView = findTerminalView() as? systems.pkt.lingon.terminal.TerminalGridView
                ?: return@runOnIdle
            val width = terminalView.width
            val height = terminalView.height
            if (width <= 0 || height <= 0) return@runOnIdle
            val bitmap = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888)
            try {
                terminalView.draw(Canvas(bitmap))
                val bgColor = bitmap.getPixel((width - 1).coerceAtLeast(0), (height - 1).coerceAtLeast(0))
                var left = width
                var top = height
                var right = -1
                var bottom = -1
                for (y in 0 until height) {
                    for (x in 0 until width) {
                        if (bitmap.getPixel(x, y) == bgColor) continue
                        if (x < left) left = x
                        if (y < top) top = y
                        if (x > right) right = x
                        if (y > bottom) bottom = y
                    }
                }
                info = if (right >= left && bottom >= top) {
                    TerminalRenderInfo(
                        widthPx = width,
                        heightPx = height,
                        inkLeft = left,
                        inkTop = top,
                        inkRight = right,
                        inkBottom = bottom,
                    )
                } else {
                    null
                }
            } finally {
                bitmap.recycle()
            }
        }
        return info
    }

    private fun waitForTerminalRenderInfo(timeoutMs: Long = SHORT_UI_TIMEOUT_MS): TerminalRenderInfo? {
        var match: TerminalRenderInfo? = null
        waitUntilNoError(timeoutMs) {
            val info = readTerminalRenderInfo()
            if (info != null) {
                match = info
                true
            } else {
                false
            }
        }
        return match
    }

    private fun stateForTest() = appViewModel().state.value

    private fun snapshotContainsToken(token: String): Boolean {
        var found = false
        composeRule.runOnIdle {
            val snap = appViewModel().state.value.activeSnapshot
            if (snap == null || snap.rows <= 0 || snap.cols <= 0) {
                found = false
                return@runOnIdle
            }
            for (row in 0 until snap.rows) {
                if (snapshotRow(snap, row).contains(token)) {
                    found = true
                    return@runOnIdle
                }
            }
        }
        return found
    }

    private fun snapshotTokenCount(token: String): Int {
        var count = 0
        composeRule.runOnIdle {
            val snap = appViewModel().state.value.activeSnapshot ?: return@runOnIdle
            if (snap.rows <= 0 || snap.cols <= 0) return@runOnIdle
            for (row in 0 until snap.rows) {
                val text = snapshotRow(snap, row)
                var index = text.indexOf(token)
                while (index >= 0) {
                    count++
                    index = text.indexOf(token, index + token.length)
                }
            }
        }
        return count
    }

    private fun terminalEchoTail(sessionId: String): List<String> {
        var tail = emptyList<String>()
        composeRule.runOnIdle {
            val snap = appViewModel().state.value.activeSnapshot ?: return@runOnIdle
            val echo = Regex("ECHO_${Regex.escape(sessionId)} ([0-9a-f]{2})")
            tail = (0 until snap.rows).flatMap { row ->
                echo.findAll(snapshotRow(snap, row)).map { it.groupValues[1] }.toList()
            }
        }
        return tail
    }

    private fun snapshotContainsSequence(vararg tokens: String): Boolean {
        var found = false
        composeRule.runOnIdle {
            val snap = appViewModel().state.value.activeSnapshot
            if (snap == null || snap.rows <= 0 || snap.cols <= 0) {
                found = false
                return@runOnIdle
            }
            val text = buildString {
                for (row in 0 until snap.rows) {
                    if (row > 0) append('\n')
                    append(snapshotRow(snap, row))
                }
            }
            var cursor = 0
            for (token in tokens) {
                val index = text.indexOf(token, cursor)
                if (index < 0) {
                    found = false
                    return@runOnIdle
                }
                cursor = index + token.length
            }
            found = true
        }
        return found
    }

    private fun snapshotRow(snapshot: systems.pkt.lingon.terminal.TerminalSnapshot, row: Int): String {
        if (row < 0 || row >= snapshot.rows) return ""
        val sb = StringBuilder()
        var x = 0
        while (x < snapshot.cols) {
            val idx = row * snapshot.cols + x
            val grapheme = snapshot.graphemes?.getOrNull(idx).orEmpty()
            val rune = snapshot.runes.getOrElse(idx) { 0 }
            if (grapheme.isNotEmpty()) {
                sb.append(grapheme)
                x += 2
                continue
            }
            if (rune == 0) {
                sb.append(' ')
            } else {
                sb.appendCodePoint(rune)
            }
            x += 1
        }
        return sb.toString()
    }

    private fun snapshotHash(snapshot: systems.pkt.lingon.terminal.TerminalSnapshot?): Long {
        if (snapshot == null) return 0L
        val crc = CRC32()
        val buf = ByteArray(4)
        fun updateInt(value: Int) {
            buf[0] = (value shr 24).toByte()
            buf[1] = (value shr 16).toByte()
            buf[2] = (value shr 8).toByte()
            buf[3] = value.toByte()
            crc.update(buf, 0, buf.size)
        }
        for (value in snapshot.runes) {
            updateInt(value)
        }
        for (value in snapshot.modes) {
            updateInt(value)
        }
        updateInt(snapshot.cursorX)
        updateInt(snapshot.cursorY)
        updateInt(if (snapshot.cursorVisible) 1 else 0)
        snapshot.graphemes?.forEach { grapheme ->
            if (grapheme.isNotEmpty()) {
                val bytes = grapheme.toByteArray(Charsets.UTF_8)
                crc.update(bytes, 0, bytes.size)
            }
            crc.update(0)
        }
        return crc.value
    }

    private fun waitForTerminalReady(timeoutMs: Long = TERMINAL_READY_TIMEOUT_MS) {
        waitUntilNoError(timeoutMs) {
            val info = readTerminalDebugInfo()
            info != null && info.rows > 0 && info.cols > 0 && info.state == "Connected"
        }
    }

    private fun activeSessionId(timeoutMs: Long = SHORT_UI_TIMEOUT_MS): String {
        var active = ""
        waitUntilNoError(timeoutMs) {
            val info = readTerminalDebugInfo()
            active = info?.activeSessionId.orEmpty()
            active.isNotBlank()
        }
        return active
    }

    private fun readStatusBanner(): StatusInfo? {
        var status: systems.pkt.lingon.viewmodel.StatusMessage? = null
        if (!safeRunOnIdle {
            status = appViewModel().state.value.bannerStatus
        }) return null
        val current = status ?: return null
        return StatusInfo(level = current.level.name, message = current.message)
    }

    private fun readLoginError(): String? {
        var message: String? = null
        if (!safeRunOnIdle {
            message = appViewModel().state.value.loginError
        }) return null
        return message?.takeIf { it.isNotBlank() }
    }

    private fun assertTopBarSafe() {
        composeRule.waitForIdle()
        val node = composeRule.onNodeWithTag(TestTags.TopBarTitle).fetchSemanticsNode()
        val top = node.boundsInRoot.top
        val statusBarPx = statusBarHeightPx()
        if (statusBarPx > 0 && top < statusBarPx) {
            throw AssertionError("Top bar overlaps status bar (top=$top, statusBar=$statusBarPx).")
        }
        openSettings()
        composeRule.onNodeWithTag(TestTags.EndpointButton).performClick()
        waitForTag(TestTags.EndpointInput)
        composeRule.onNodeWithText("Cancel").performClick()
        waitForTagToDisappear(TestTags.EndpointInput)
    }

    private fun statusBarHeightPx(): Int {
        val resources = composeRule.activity.resources
        val resId = resources.getIdentifier("status_bar_height", "dimen", "android")
        return if (resId > 0) resources.getDimensionPixelSize(resId) else 0
    }

    private fun readSystemVisibleBottomPx(): Int {
        return readRootViewHeightPx() - maxOf(readImeBottomInsetPx(), readNavigationBottomInsetPx())
    }

    private fun enableSoftKeyboardWithHardwareKeyboard() {
        InstrumentationRegistry.getInstrumentation().uiAutomation.executeShellCommand(
            "settings put secure show_ime_with_hard_keyboard 1",
        ).close()
    }

    private fun forceSoftInputOverlay() {
        composeRule.runOnUiThread {
            composeRule.activity.window.setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_ADJUST_NOTHING)
        }
        composeRule.waitForIdle()
    }

    private fun restoreSoftInputAdjustResize() {
        composeRule.runOnUiThread {
            composeRule.activity.window.setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE)
        }
        composeRule.waitForIdle()
    }

    private fun readRootViewHeightPx(): Int {
        var height = 0
        composeRule.runOnIdle {
            height = composeRule.activity.window.decorView.rootView.height
        }
        return height
    }

    private fun readImeBottomInsetPx(): Int {
        var bottom = 0
        composeRule.runOnIdle {
            val insets = composeRule.activity.window.decorView.rootWindowInsets
            bottom = insets
                ?.let { WindowInsetsCompat.toWindowInsetsCompat(it, composeRule.activity.window.decorView) }
                ?.getInsets(WindowInsetsCompat.Type.ime())
                ?.bottom ?: 0
        }
        return bottom
    }

    private fun readNavigationBottomInsetPx(): Int {
        var bottom = 0
        composeRule.runOnIdle {
            val insets = composeRule.activity.window.decorView.rootWindowInsets
            bottom = insets
                ?.let { WindowInsetsCompat.toWindowInsetsCompat(it, composeRule.activity.window.decorView) }
                ?.getInsets(WindowInsetsCompat.Type.navigationBars())
                ?.bottom ?: 0
        }
        return bottom
    }

    private fun waitUntil(timeoutMs: Long = DEFAULT_TIMEOUT_MS, condition: () -> Boolean) {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            if (condition()) return
            safeWaitForIdle()
            Thread.sleep(POLL_INTERVAL_MS)
        }
        throw AssertionError("Timed out waiting for UI condition after ${timeoutMs}ms")
    }

    private fun waitUntilWithoutCompose(timeoutMs: Long = DEFAULT_TIMEOUT_MS, condition: () -> Boolean) {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            if (condition()) return
            Thread.sleep(POLL_INTERVAL_MS)
        }
        throw AssertionError("Timed out waiting for UI condition after ${timeoutMs}ms")
    }

    private fun waitUntilNoError(timeoutMs: Long = DEFAULT_TIMEOUT_MS, condition: () -> Boolean) {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            val loginError = readLoginError()
            if (!loginError.isNullOrBlank()) {
                throw AssertionError("login error: $loginError")
            }
            val status = readStatusBanner()
            if (status != null && status.level.equals("Error", ignoreCase = true)) {
                throw AssertionError("status error: ${status.message}")
            }
            if (condition()) return
            safeWaitForIdle()
            Thread.sleep(POLL_INTERVAL_MS)
        }
        val debug = readTerminalDebugInfo()
        val state = appViewModel().state.value
        val sessions = state.sessions.joinToString(",") { "${it.id}:${it.status}" }
        throw AssertionError(
            "Timed out waiting for UI condition after ${timeoutMs}ms " +
                "(connection=${state.connectionState}, active=${state.activeSessionId}, " +
                "shareToken=${state.shareToken}, " +
                "sessions=[${sessions}], rows=${debug?.rows ?: 0}, cols=${debug?.cols ?: 0}, " +
                "lastFrameSeq=${debug?.lastFrameSeq ?: 0}, lastFrameType=${debug?.lastFrameType}, " +
                "scrollbackOffset=${debug?.scrollbackOffsetRows ?: 0}, " +
                "viewCols=${debug?.viewCols ?: 0}, viewRows=${debug?.viewRows ?: 0}, " +
                "renderScaleX=${debug?.renderScaleX ?: 0f}, renderScaleY=${debug?.renderScaleY ?: 0f}, " +
                "visibleStartRow=${debug?.visibleStartRow ?: 0}, " +
                "visibleEndRowExclusive=${debug?.visibleEndRowExclusive ?: 0}, " +
                "zoomFactor=${debug?.zoomFactor ?: 0f}, " +
                "wallInactivityEnabled=${state.wallInactivityEnabled}, wallInactivityLabel=${state.wallInactivityLabel})",
        )
    }

    private fun safeRunOnIdle(block: () -> Unit): Boolean {
        return try {
            composeRule.runOnIdle(block)
            true
        } catch (err: RuntimeException) {
            if (!isTransientComposeIdleRace(err)) throw err
            false
        }
    }

    private fun safeWaitForIdle() {
        try {
            composeRule.waitForIdle()
        } catch (err: RuntimeException) {
            if (!isTransientComposeIdleRace(err)) throw err
        }
    }

    private fun isTransientComposeIdleRace(err: Throwable): Boolean {
        return generateSequence(err) { it.cause }.any { cause ->
            cause.message.orEmpty().contains("performMeasureAndLayout called during measure layout")
        }
    }

    private fun captureScreenshot(name: String) {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val baseDir = context.getExternalFilesDir(null) ?: context.filesDir
        val outDir = File(baseDir, "test-artifacts").apply { mkdirs() }
        val safeName = "${screenshotRunId}_${name.replace(Regex("[^a-zA-Z0-9._-]"), "_")}"
        val bitmap = InstrumentationRegistry.getInstrumentation().uiAutomation.takeScreenshot()
            ?: throw AssertionError("failed to capture screenshot for $safeName")
        writePng(File(outDir, "${safeName}.png"), bitmap)
        writeShellScreenshot("/sdcard/Download/lingon-test-artifacts/${safeName}.png")
    }

    private fun clearScreenshotArtifacts() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val clear = instrumentation.uiAutomation.executeShellCommand(
            "rm -rf /sdcard/Download/lingon-test-artifacts && mkdir -p /sdcard/Download/lingon-test-artifacts",
        )
        consumeShellCommand(clear)
    }

    private fun writePng(file: File, bitmap: Bitmap) {
        FileOutputStream(file).use { out ->
            bitmap.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }

    private fun writeShellScreenshot(path: String) {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val mkdir = instrumentation.uiAutomation.executeShellCommand(
            "mkdir -p ${File(path).parent}",
        )
        consumeShellCommand(mkdir)
        val screencap = instrumentation.uiAutomation.executeShellCommand(
            "screencap -p $path",
        )
        consumeShellCommand(screencap)
    }

    private fun consumeShellCommand(descriptor: ParcelFileDescriptor?) {
        descriptor ?: return
        descriptor.use { pfd ->
            FileInputStream(pfd.fileDescriptor).use { input ->
                while (input.read() != -1) {
                    // Drain shell output so the command completes before the test continues.
                }
            }
        }
    }

    private fun shellCommandOutput(command: String): String {
        val descriptor = InstrumentationRegistry.getInstrumentation().uiAutomation
            .executeShellCommand(command)
        return ParcelFileDescriptor.AutoCloseInputStream(descriptor)
            .bufferedReader().use { it.readText() }
    }

    private fun resetZoomPan() {
        openSettings()
        composeRule.onNodeWithTag(TestTags.ZoomResetButton).performClick()
        closeSettingsIfOpen()
    }

    private fun setZoomFactor(value: Float) {
        composeRule.runOnUiThread { appViewModel().updateZoomFactor(value) }
        composeRule.waitForIdle()
    }

    private fun exactlyMeasureSpec(size: Int): Int {
        return View.MeasureSpec.makeMeasureSpec(size, View.MeasureSpec.EXACTLY)
    }

    private fun terminalSnapshotForViewTest(
        rows: Int,
        cols: Int,
        cursorY: Int = rows - 1,
        cursorX: Int = 0,
    ): TerminalSnapshot {
        val size = rows * cols
        return TerminalSnapshot(
            cols = cols,
            rows = rows,
            runes = IntArray(size) { ' '.code },
            modes = IntArray(size),
            fg = IntArray(size),
            bg = IntArray(size),
            graphemes = null,
            cursorX = cursorX.coerceIn(0, cols - 1),
            cursorY = cursorY.coerceIn(0, rows - 1),
            cursorVisible = true,
            mode = 0,
            title = "",
        )
    }

    private fun scrollbackRowsForViewTest(rows: Int, cols: Int): List<ScrollbackRow> {
        return (0 until rows).map { row ->
            val builder = ScrollbackRow.newBuilder()
            repeat(cols) {
                builder.addRunes('A'.code + (row % 26))
                builder.addModes(0)
                builder.addFg(0)
                builder.addBg(0)
            }
            builder.build()
        }
    }

    private fun dispatchSinglePointerDrag(
        view: View,
        startX: Float,
        startY: Float,
        endX: Float,
        endY: Float,
    ) {
        val startTime = SystemClock.uptimeMillis()
        view.dispatchTouchEvent(MotionEvent.obtain(startTime, startTime, MotionEvent.ACTION_DOWN, startX, startY, 0))
        view.dispatchTouchEvent(MotionEvent.obtain(startTime, startTime + 16, MotionEvent.ACTION_MOVE, endX, endY, 0))
        view.dispatchTouchEvent(MotionEvent.obtain(startTime, startTime + 32, MotionEvent.ACTION_UP, endX, endY, 0))
    }

    private fun dispatchTerminalDragPulses(deltaYs: List<Float>): List<TerminalDebugInfo> {
        var view: View? = null
        composeRule.runOnIdle {
            view = findTerminalView()
        }
        val terminalView = view ?: throw AssertionError("missing terminal view")
        val width = terminalView.width
        val height = terminalView.height
        if (width <= 0 || height <= 0) {
            throw AssertionError("terminal view was not measured: ${width}x$height")
        }
        val samples = mutableListOf<TerminalDebugInfo>()
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val x = width / 2f
        val startY = height / 2f
        deltaYs.forEach { deltaY ->
            val startTime = SystemClock.uptimeMillis()
            val endY = (startY + deltaY).coerceIn(4f, height - 4f)
            instrumentation.runOnMainSync {
                terminalView.dispatchTouchEvent(
                    MotionEvent.obtain(startTime, startTime, MotionEvent.ACTION_DOWN, x, startY, 0),
                )
                terminalView.dispatchTouchEvent(
                    MotionEvent.obtain(startTime, startTime + 16, MotionEvent.ACTION_MOVE, x, endY, 0),
                )
                terminalView.dispatchTouchEvent(
                    MotionEvent.obtain(startTime, startTime + 32, MotionEvent.ACTION_UP, x, endY, 0),
                )
            }
            composeRule.waitForIdle()
            samples += readTerminalDebugInfo()
                ?: throw AssertionError("missing terminal debug info during drag")
        }
        return samples
    }

    private fun performPinchZoom(zoomIn: Boolean) {
        composeRule.runOnIdle {
            val view = findTerminalView() ?: return@runOnIdle
            val centerX = view.width / 2f
            val centerY = view.height / 2f
            val base = min(view.width, view.height) * 0.12f
            val startOffset = base.coerceAtLeast(12f)
            val endOffset = if (zoomIn) startOffset * 3.2f else startOffset * 0.4f
            val start1 = Offset(centerX - startOffset, centerY)
            val start2 = Offset(centerX + startOffset, centerY)
            val end1 = Offset(centerX - endOffset, centerY)
            val end2 = Offset(centerX + endOffset, centerY)
            dispatchPinch(view, start1, start2, end1, end2)
        }
        composeRule.waitForIdle()
    }

    private fun findTerminalView(): View? {
        val root = composeRule.activity.window.decorView
        return findViewWithTag(root, "terminal_view")
    }

    private fun findViewWithTag(view: View, tag: String): View? {
        if (tag == view.tag) return view
        if (view is android.view.ViewGroup) {
            for (i in 0 until view.childCount) {
                val child = view.getChildAt(i)
                val found = findViewWithTag(child, tag)
                if (found != null) return found
            }
        }
        return null
    }

    private fun dispatchPinch(view: View, start1: Offset, start2: Offset, end1: Offset, end2: Offset) {
        val startTime = SystemClock.uptimeMillis()
        var eventTime = startTime
        val props = arrayOf(
            MotionEvent.PointerProperties().apply {
                id = 0
                toolType = MotionEvent.TOOL_TYPE_FINGER
            },
            MotionEvent.PointerProperties().apply {
                id = 1
                toolType = MotionEvent.TOOL_TYPE_FINGER
            },
        )
        val coords = arrayOf(
            MotionEvent.PointerCoords().apply {
                x = clamp(start1.x, 0f, view.width.toFloat())
                y = clamp(start1.y, 0f, view.height.toFloat())
                pressure = 1f
                size = 1f
            },
            MotionEvent.PointerCoords().apply {
                x = clamp(start2.x, 0f, view.width.toFloat())
                y = clamp(start2.y, 0f, view.height.toFloat())
                pressure = 1f
                size = 1f
            },
        )
        view.dispatchTouchEvent(
            MotionEvent.obtain(
                startTime,
                eventTime,
                MotionEvent.ACTION_DOWN,
                1,
                props,
                coords,
                0,
                0,
                1f,
                1f,
                0,
                0,
                0,
                0,
            ),
        )
        eventTime += 10
        view.dispatchTouchEvent(
            MotionEvent.obtain(
                startTime,
                eventTime,
                MotionEvent.ACTION_POINTER_DOWN or (1 shl MotionEvent.ACTION_POINTER_INDEX_SHIFT),
                2,
                props,
                coords,
                0,
                0,
                1f,
                1f,
                0,
                0,
                0,
                0,
            ),
        )

        val steps = 8
        for (i in 1..steps) {
            val t = i / steps.toFloat()
            coords[0].x = clamp(lerp(start1.x, end1.x, t), 0f, view.width.toFloat())
            coords[0].y = clamp(lerp(start1.y, end1.y, t), 0f, view.height.toFloat())
            coords[1].x = clamp(lerp(start2.x, end2.x, t), 0f, view.width.toFloat())
            coords[1].y = clamp(lerp(start2.y, end2.y, t), 0f, view.height.toFloat())
            eventTime += 12
            view.dispatchTouchEvent(
                MotionEvent.obtain(
                    startTime,
                    eventTime,
                    MotionEvent.ACTION_MOVE,
                    2,
                    props,
                    coords,
                    0,
                    0,
                    1f,
                    1f,
                    0,
                    0,
                    0,
                    0,
                ),
            )
        }

        eventTime += 10
        view.dispatchTouchEvent(
            MotionEvent.obtain(
                startTime,
                eventTime,
                MotionEvent.ACTION_POINTER_UP or (1 shl MotionEvent.ACTION_POINTER_INDEX_SHIFT),
                2,
                props,
                coords,
                0,
                0,
                1f,
                1f,
                0,
                0,
                0,
                0,
            ),
        )
        eventTime += 10
        view.dispatchTouchEvent(
            MotionEvent.obtain(
                startTime,
                eventTime,
                MotionEvent.ACTION_UP,
                1,
                props,
                coords,
                0,
                0,
                1f,
                1f,
                0,
                0,
                0,
                0,
            ),
        )
    }

    private fun lerp(start: Float, end: Float, t: Float): Float = start + (end - start) * t

    private fun clamp(value: Float, minValue: Float, maxValue: Float): Float {
        return max(minValue, min(maxValue, value))
    }

    private fun assertBackendReachable(endpoint: String, caPem: String?) {
        val url = URL("${endpoint.trimEnd('/')}/sessions")
        val connection = (url.openConnection() as HttpURLConnection).apply {
            connectTimeout = 3000
            readTimeout = 3000
            instanceFollowRedirects = false
        }
        if (connection is javax.net.ssl.HttpsURLConnection && !caPem.isNullOrBlank()) {
            val sslContext = trustContextFor(caPem)
            connection.sslSocketFactory = sslContext.socketFactory
        }
        try {
            val code = connection.responseCode
            if (code != HttpURLConnection.HTTP_OK &&
                code != HttpURLConnection.HTTP_UNAUTHORIZED &&
                code != HttpURLConnection.HTTP_FORBIDDEN
            ) {
                throw AssertionError(
                    "lingon backend responded with HTTP $code at $endpoint. " +
                        "Start lingon on the host (e.g. `lingon serve`) and expose :12843.",
                )
            }
        } catch (ex: Exception) {
            throw AssertionError(
                "lingon backend not reachable at $endpoint. " +
                    "Start lingon on the host (e.g. `lingon serve`) and expose :12843.",
                ex,
            )
        } finally {
            connection.disconnect()
        }
    }

    private fun startHostViaHarness() {
        val url = URL("${testConfig.endpoint.trimEnd('/')}/__harness/start-host")
        val connection = (url.openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 5_000
            readTimeout = 5_000
            doOutput = true
        }
        if (connection is javax.net.ssl.HttpsURLConnection && !testConfig.caPem.isNullOrBlank()) {
            val sslContext = trustContextFor(testConfig.caPem)
            connection.sslSocketFactory = sslContext.socketFactory
        }
        try {
            val code = connection.responseCode
            if (code != HttpURLConnection.HTTP_OK) {
                throw AssertionError("harness start-host failed with HTTP $code")
            }
        } finally {
            connection.disconnect()
        }
    }

    private fun startHostsViaHarness(
        count: Int,
        cols: Int = 120,
        rows: Int = testConfig.hostRows,
        shell: String? = null,
        initialLines: Int = 0,
    ): List<String> {
        val shellQuery = shell?.takeIf { it.isNotBlank() }?.let { "&shell=${java.net.URLEncoder.encode(it, "UTF-8")}" }.orEmpty()
        val initialLinesQuery = if (initialLines > 0) "&initial_lines=$initialLines" else ""
        val url = URL("${testConfig.endpoint.trimEnd('/')}/__harness/start-host?count=$count&cols=$cols&rows=$rows$shellQuery$initialLinesQuery")
        val connection = (url.openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 5_000
            readTimeout = 5_000
            doOutput = true
        }
        if (connection is javax.net.ssl.HttpsURLConnection && !testConfig.caPem.isNullOrBlank()) {
            val sslContext = trustContextFor(testConfig.caPem)
            connection.sslSocketFactory = sslContext.socketFactory
        }
        try {
            val code = connection.responseCode
            if (code != HttpURLConnection.HTTP_OK) {
                throw AssertionError("harness start-host failed with HTTP $code")
            }
            val body = InputStreamReader(connection.inputStream).use { it.readText() }
            val ids = org.json.JSONObject(body).getJSONArray("ids")
            return List(ids.length()) { index -> ids.getString(index) }.also { startedHostSessions.addAll(it) }
        } finally {
            connection.disconnect()
        }
    }

    private fun stopHostsViaHarness(sessionIds: List<String>) {
        sessionIds.map { it.trim() }.filter { it.isNotBlank() }.forEach { sessionId ->
            val url = URL("${testConfig.endpoint.trimEnd('/')}/__harness/stop-host")
            val body = org.json.JSONObject()
                .put("session_id", sessionId)
                .toString()
                .toByteArray(Charsets.UTF_8)
            val connection = (url.openConnection() as HttpURLConnection).apply {
                requestMethod = "POST"
                connectTimeout = 5_000
                readTimeout = 5_000
                doOutput = true
                setRequestProperty("Content-Type", "application/json")
            }
            if (connection is javax.net.ssl.HttpsURLConnection && !testConfig.caPem.isNullOrBlank()) {
                val sslContext = trustContextFor(testConfig.caPem)
                connection.sslSocketFactory = sslContext.socketFactory
            }
            try {
                connection.outputStream.use { it.write(body) }
                val code = connection.responseCode
                if (code != HttpURLConnection.HTTP_OK && code != HttpURLConnection.HTTP_NOT_FOUND) {
                    throw AssertionError("harness stop-host failed with HTTP $code for $sessionId")
                }
            } finally {
                connection.disconnect()
            }
        }
    }

    private fun startHeadlessViaHarness(cols: Int = 120, rows: Int = 50): String {
        return startHeadlessWithShareViaHarness(cols, rows).first
    }

    private fun startHeadlessWithShareViaHarness(cols: Int = 120, rows: Int = 50): Pair<String, String> {
        val url = URL("${testConfig.endpoint.trimEnd('/')}/__harness/start-headless?cols=$cols&rows=$rows")
        val connection = (url.openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 5_000
            readTimeout = 5_000
            doOutput = true
        }
        if (connection is javax.net.ssl.HttpsURLConnection && !testConfig.caPem.isNullOrBlank()) {
            val sslContext = trustContextFor(testConfig.caPem)
            connection.sslSocketFactory = sslContext.socketFactory
        }
        try {
            val code = connection.responseCode
            if (code != HttpURLConnection.HTTP_OK) {
                throw AssertionError("harness start-headless failed with HTTP $code")
            }
            val body = InputStreamReader(connection.inputStream).use { it.readText() }
            val response = org.json.JSONObject(body)
            val id = response.getString("id")
            startedHeadlessSessions += id
            return id to response.getString("view_token")
        } finally {
            connection.disconnect()
        }
    }

    private fun detachHeadlessViaHarness(sessionId: String) {
        val url = URL("${testConfig.endpoint.trimEnd('/')}/__harness/detach-headless")
        val connection = (url.openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 5_000
            readTimeout = 5_000
            doOutput = true
            setRequestProperty("Content-Type", "application/json")
        }
        if (connection is javax.net.ssl.HttpsURLConnection && !testConfig.caPem.isNullOrBlank()) {
            val sslContext = trustContextFor(testConfig.caPem)
            connection.sslSocketFactory = sslContext.socketFactory
        }
        try {
            connection.outputStream.use { out ->
                out.write("""{"session_id":${org.json.JSONObject.quote(sessionId)}}""".toByteArray())
            }
            val code = connection.responseCode
            if (code != HttpURLConnection.HTTP_OK) {
                throw AssertionError("harness detach-headless failed with HTTP $code")
            }
            startedHeadlessSessions -= sessionId
        } finally {
            connection.disconnect()
        }
    }

    private fun detachStartedHeadlessSessions() {
        val sessionIds = startedHeadlessSessions.toList()
        startedHeadlessSessions.clear()
        sessionIds.forEach { sessionId ->
            runCatching { detachHeadlessViaHarness(sessionId) }
        }
    }

    private fun readHeadlessSizeViaHarness(sessionId: String): HeadlessSize {
        val encodedSessionId = java.net.URLEncoder.encode(sessionId, "UTF-8")
        val url = URL("${testConfig.endpoint.trimEnd('/')}/__harness/headless-size?session_id=$encodedSessionId")
        val connection = (url.openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 5_000
            readTimeout = 5_000
        }
        if (connection is javax.net.ssl.HttpsURLConnection && !testConfig.caPem.isNullOrBlank()) {
            val sslContext = trustContextFor(testConfig.caPem)
            connection.sslSocketFactory = sslContext.socketFactory
        }
        try {
            val code = connection.responseCode
            if (code != HttpURLConnection.HTTP_OK) {
                throw AssertionError("harness headless-size failed with HTTP $code")
            }
            val body = InputStreamReader(connection.inputStream).use { it.readText() }
            val json = org.json.JSONObject(body)
            return HeadlessSize(
                cols = json.getInt("cols"),
                rows = json.getInt("rows"),
            )
        } finally {
            connection.disconnect()
        }
    }

    private fun sendWallViaHarness(message: String) {
        val url = URL("${testConfig.endpoint.trimEnd('/')}/__harness/send-wall")
        val connection = (url.openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 5_000
            readTimeout = 5_000
            doOutput = true
            setRequestProperty("Content-Type", "application/json")
        }
        if (connection is javax.net.ssl.HttpsURLConnection && !testConfig.caPem.isNullOrBlank()) {
            val sslContext = trustContextFor(testConfig.caPem)
            connection.sslSocketFactory = sslContext.socketFactory
        }
        try {
            connection.outputStream.use { out ->
                out.write("""{"message":${org.json.JSONObject.quote(message)}}""".toByteArray())
            }
            val code = connection.responseCode
            if (code != HttpURLConnection.HTTP_OK) {
                throw AssertionError("harness send-wall failed with HTTP $code")
            }
        } finally {
            connection.disconnect()
        }
    }

    private fun advanceWallClockViaHarness(milliseconds: Long) {
        val url = URL("${testConfig.endpoint.trimEnd('/')}/__harness/advance-wall-clock")
        val connection = (url.openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 5_000
            readTimeout = 5_000
            doOutput = true
            setRequestProperty("Content-Type", "application/json")
        }
        if (connection is javax.net.ssl.HttpsURLConnection && !testConfig.caPem.isNullOrBlank()) {
            connection.sslSocketFactory = trustContextFor(testConfig.caPem).socketFactory
        }
        try {
            connection.outputStream.use { it.write(org.json.JSONObject().put("milliseconds", milliseconds).toString().toByteArray()) }
            if (connection.responseCode != HttpURLConnection.HTTP_OK) throw AssertionError("harness refused controlled wall-clock advancement")
        } finally {
            connection.disconnect()
        }
    }

    private fun setWallInactivityViaHarness(sessions: List<String>, enabled: Boolean) {
        val cleanSessions = sessions.map { it.trim() }.filter { it.isNotBlank() }
        if (cleanSessions.isEmpty()) {
            return
        }
        val url = URL("${testConfig.endpoint.trimEnd('/')}/__harness/wall-inactivity")
        val connection = (url.openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            connectTimeout = 5_000
            readTimeout = 5_000
            doOutput = true
            setRequestProperty("Content-Type", "application/json")
        }
        if (connection is javax.net.ssl.HttpsURLConnection && !testConfig.caPem.isNullOrBlank()) {
            val sslContext = trustContextFor(testConfig.caPem)
            connection.sslSocketFactory = sslContext.socketFactory
        }
        try {
            val payload = org.json.JSONObject()
                .put("enabled", enabled)
                .put("sessions", org.json.JSONArray(cleanSessions))
                .toString()
            connection.outputStream.use { out ->
                out.write(payload.toByteArray())
            }
            val code = connection.responseCode
            if (code != HttpURLConnection.HTTP_OK) {
                throw AssertionError("harness wall-inactivity failed with HTTP $code")
            }
        } finally {
            connection.disconnect()
        }
    }

    private fun trustContextFor(caPem: String): javax.net.ssl.SSLContext {
        val certFactory = java.security.cert.CertificateFactory.getInstance("X.509")
        val cert = certFactory.generateCertificate(java.io.ByteArrayInputStream(caPem.toByteArray()))
        val keyStore = java.security.KeyStore.getInstance(java.security.KeyStore.getDefaultType()).apply {
            load(null)
            setCertificateEntry("ca", cert)
        }
        val tmf = javax.net.ssl.TrustManagerFactory.getInstance(
            javax.net.ssl.TrustManagerFactory.getDefaultAlgorithm(),
        ).apply {
            init(keyStore)
        }
        return javax.net.ssl.SSLContext.getInstance("TLS").apply {
            init(null, tmf.trustManagers, null)
        }
    }

    private fun generateTotp(secret: String, timestampSeconds: Long = System.currentTimeMillis() / 1000L): String {
        val key = base32Decode(secret)
        val counter = timestampSeconds / 30L
        val data = ByteArray(8)
        var value = counter
        for (i in 7 downTo 0) {
            data[i] = (value and 0xFF).toByte()
            value = value shr 8
        }
        val mac = Mac.getInstance("HmacSHA1")
        mac.init(SecretKeySpec(key, "HmacSHA1"))
        val hash = mac.doFinal(data)
        val offset = hash.last().toInt() and 0x0F
        val binary =
            ((hash[offset].toInt() and 0x7F) shl 24) or
                ((hash[offset + 1].toInt() and 0xFF) shl 16) or
                ((hash[offset + 2].toInt() and 0xFF) shl 8) or
                (hash[offset + 3].toInt() and 0xFF)
        val otp = binary % 1_000_000
        return otp.toString().padStart(TOTP_DIGITS, '0')
    }

    private fun base32Decode(input: String): ByteArray {
        val cleaned = input.trim().replace("=", "").uppercase(Locale.US)
        val output = ArrayList<Byte>(cleaned.length)
        var buffer = 0
        var bitsLeft = 0
        for (ch in cleaned) {
            val value = BASE32_ALPHABET.indexOf(ch)
            if (value < 0) continue
            buffer = (buffer shl 5) or value
            bitsLeft += 5
            if (bitsLeft >= 8) {
                bitsLeft -= 8
                output.add(((buffer shr bitsLeft) and 0xFF).toByte())
            }
        }
        return output.toByteArray()
    }

    companion object {
        private const val EMULATOR_ENDPOINT = "https://localhost:12843/v1"
        private const val UNREACHABLE_ENDPOINT = "https://localhost:65534/v1"
        private const val DEFAULT_TOTP_SECRET = "JBSWY3DPEHPK3PXP"
        private const val DEFAULT_USERNAME = "admin"
        private const val DEFAULT_PASSWORD = "admin"
        private const val TOTP_DIGITS = 6
        private const val DEFAULT_TIMEOUT_MS = 30_000L
        private const val LOGIN_TIMEOUT_MS = 20_000L
        private const val TERMINAL_READY_TIMEOUT_MS = 15_000L
        private const val SHORT_UI_TIMEOUT_MS = 15_000L
        private const val BACKGROUND_WALL_NOTIFICATION_TIMEOUT_MS = 35_000L
        private const val POLL_INTERVAL_MS = 250L
        private const val BASE32_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
    }

    data class UserConfig(
        val username: String,
        val password: String,
        val totpSecret: String,
        val sessions: List<String>,
        val viewToken: String?,
    )

    data class TestConfig(
        val endpoint: String,
        val username: String,
        val password: String,
        val totpSecret: String,
        val caPem: String?,
        val sessions: List<String>,
        val viewToken: String?,
        val hostCols: Int,
        val controlToken: String?,
        val hostRows: Int,
        val username2: String?,
        val password2: String?,
        val totpSecret2: String?,
        val sessions2: List<String>,
        val viewToken2: String?,
    ) {
        fun primarySessionId(): String? = sessions.firstOrNull()
        fun primaryUser(): UserConfig = UserConfig(
            username = username,
            password = password,
            totpSecret = totpSecret,
            sessions = sessions,
            viewToken = viewToken,
        )
        fun secondaryUser(): UserConfig? {
            val user = username2?.takeIf { it.isNotBlank() } ?: return null
            val pass = password2?.takeIf { it.isNotBlank() } ?: return null
            val totp = totpSecret2?.takeIf { it.isNotBlank() } ?: return null
            return UserConfig(
                username = user,
                password = pass,
                totpSecret = totp,
                sessions = sessions2,
                viewToken = viewToken2,
            )
        }

        companion object {
            fun fromArgs(): TestConfig {
                val args = InstrumentationRegistry.getArguments()
                val endpoint = args.getString("endpoint") ?: EMULATOR_ENDPOINT
                val username = args.getString("username") ?: DEFAULT_USERNAME
                val password = args.getString("password") ?: DEFAULT_PASSWORD
                val totpSecret = args.getString("totp_secret") ?: DEFAULT_TOTP_SECRET
                val username2 = args.getString("username2")
                val password2 = args.getString("password2")
                val totpSecret2 = args.getString("totp_secret2")
                val caPem = args.getString("ca_pem_b64")?.let { decodeBase64(it) }
                val sessions = args.getString("sessions")
                    ?.split(',')
                    ?.map { it.trim() }
                    ?.filter { it.isNotBlank() }
                    .orEmpty()
                val viewToken = args.getString("view_token")?.trim()?.takeIf { it.isNotBlank() }
                val sessions2 = args.getString("sessions2")
                    ?.split(',')
                    ?.map { it.trim() }
                    ?.filter { it.isNotBlank() }
                    .orEmpty()
                val viewToken2 = args.getString("view_token2")?.trim()?.takeIf { it.isNotBlank() }
                val hostCols = args.getString("host_cols")?.toIntOrNull() ?: 120
                val hostRows = args.getString("host_rows")?.toIntOrNull() ?: 50
                return TestConfig(
                    endpoint = endpoint,
                    username = username,
                    password = password,
                    totpSecret = totpSecret,
                    caPem = caPem,
                    sessions = sessions,
                    viewToken = viewToken,
                    controlToken = args.getString("control_token")?.trim()?.takeIf { it.isNotBlank() },
                    hostCols = hostCols,
                    hostRows = hostRows,
                    username2 = username2,
                    password2 = password2,
                    totpSecret2 = totpSecret2,
                    sessions2 = sessions2,
                    viewToken2 = viewToken2,
                )
            }

            private fun decodeBase64(value: String): String? {
                return runCatching {
                    val bytes = Base64.getDecoder().decode(value)
                    String(bytes, Charsets.UTF_8)
                }.getOrNull()
            }
        }
    }

    data class StatusInfo(
        val level: String,
        val message: String,
    )

    data class HeadlessSize(
        val cols: Int,
        val rows: Int,
    )

    data class TerminalDebugInfo(
        val rows: Int,
        val cols: Int,
        val state: String,
        val activeSessionId: String,
        val lastFrameSeq: Long,
        val lastFrameType: String?,
        val scrollbackOffsetRows: Int,
        val hash: Long,
        val row0: String,
        val prevLine: String,
        val tail: String,
        val cursorLine: String,
        val cursorY: Int,
        val hasControl: Boolean,
        val resizeEnabled: Boolean,
        val viewCols: Int,
        val viewRows: Int,
        val renderScaleX: Float,
        val renderScaleY: Float,
        val cameraOffsetYPx: Float,
        val viewportHeightPx: Int,
        val physicalHeightPx: Int,
        val scaledCellHeightPx: Float,
        val visibleStartRow: Int,
        val visibleEndRowExclusive: Int,
        val zoomFactor: Float,
    ) {
        fun liveTopRows(): Float {
            if (scaledCellHeightPx <= 0f) return 0f
            return (cameraOffsetYPx / scaledCellHeightPx) - scrollbackOffsetRows.toFloat()
        }

        fun isLiveBottomAnchored(): Boolean {
            if (rows <= 0 || scaledCellHeightPx <= 0f || viewportHeightPx <= 0) return true
            val expected = maxOf(0f, (rows * scaledCellHeightPx) - viewportHeightPx.toFloat())
            return kotlin.math.abs(cameraOffsetYPx - expected) <= maxOf(1f, scaledCellHeightPx * 0.1f)
        }
    }

    data class TerminalCameraSnapshot(
        val cameraOffsetYPx: Float,
        val scaledCellHeightPx: Float,
        val visibleStartRow: Int,
    )

    data class OverlayViewportSample(
        val physicalHeightPx: Int,
        val viewportHeightPx: Int,
        val scaledCellHeightPx: Float,
        val cameraOffsetYPx: Float,
        val visibleStartRow: Int,
        val visibleEndRowExclusive: Int,
    ) {
        fun isBottomAnchored(): Boolean {
            if (scaledCellHeightPx <= 0f || viewportHeightPx <= 0) return false
            val expected = maxOf(0f, (240 * scaledCellHeightPx) - viewportHeightPx.toFloat())
            return kotlin.math.abs(cameraOffsetYPx - expected) <= maxOf(1f, scaledCellHeightPx * 0.1f) &&
                visibleEndRowExclusive == 240
        }
    }

    private fun List<TerminalDebugInfo>.describePanSamples(): String {
        return joinToString(prefix = "[", postfix = "]") { info ->
            val liveTop = if (info.scaledCellHeightPx > 0f) {
                String.format(Locale.US, "%.2f", info.liveTopRows())
            } else {
                "nan"
            }
            "off=${info.scrollbackOffsetRows},cam=${String.format(Locale.US, "%.1f", info.cameraOffsetYPx)}," +
                "cell=${String.format(Locale.US, "%.1f", info.scaledCellHeightPx)},live=$liveTop," +
                "vis=${info.visibleStartRow}-${info.visibleEndRowExclusive}"
        }
    }

    data class TerminalRenderInfo(
        val widthPx: Int,
        val heightPx: Int,
        val inkLeft: Int,
        val inkTop: Int,
        val inkRight: Int,
        val inkBottom: Int,
    )

}
