package systems.pkt.lingon

import android.Manifest
import android.app.Notification
import android.app.NotificationManager
import android.service.notification.StatusBarNotification
import android.graphics.Bitmap
import android.graphics.Canvas
import android.os.ParcelFileDescriptor
import android.view.KeyEvent
import androidx.core.app.NotificationManagerCompat
import androidx.test.rule.GrantPermissionRule
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextReplacement
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModelProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import android.os.SystemClock
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import java.net.HttpURLConnection
import java.net.URL
import java.util.Base64
import java.util.Locale
import java.util.zip.CRC32
import java.io.File
import java.io.FileInputStream
import java.io.InputStreamReader
import systems.pkt.lingon.viewmodel.ConnectionState
import java.io.FileOutputStream
import java.util.UUID
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec
import kotlinx.coroutines.runBlocking
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.junit.rules.RuleChain
import systems.pkt.lingon.test.FailureCaptureRule
import systems.pkt.lingon.ui.TestTags
import systems.pkt.lingon.viewmodel.AppViewModel
import systems.pkt.lingon.viewmodel.AppViewModelFactory
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

    @Before
    fun ensureBackendReachable() {
        configureEndpointAndCerts()
        assertBackendReachable(testConfig.endpoint, testConfig.caPem)
    }

    @Test
    fun top_bar_menu_is_accessible() {
        assertTopBarSafe()
    }

    @Test
    fun menu_overlay_does_not_shift_login() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        waitForTag(TestTags.LoginUsername)
        val before = nodeBounds(TestTags.LoginUsername)
        composeRule.onNodeWithTag(TestTags.TopBarMenuButton).performClick()
        waitForTag(TestTags.TopBarMenu)
        val after = nodeBounds(TestTags.LoginUsername)
        val topDelta = kotlin.math.abs(before.top - after.top)
        val leftDelta = kotlin.math.abs(before.left - after.left)
        if (topDelta > 1f || leftDelta > 1f) {
            throw AssertionError("login layout shifted when menu opened: topΔ=${topDelta}, leftΔ=${leftDelta}")
        }
        composeRule.onNodeWithTag(TestTags.TopBarMenuButton).performClick()
        waitForTagToDisappear(TestTags.TopBarMenu)
    }

    @Test
    fun menu_toggle_closes_and_reload_triggers() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)

        composeRule.onNodeWithTag(TestTags.TopBarMenuButton).performClick()
        waitForTag(TestTags.TopBarMenu)
        composeRule.onNodeWithTag(TestTags.TopBarMenuButton).performClick()
        waitForTagToDisappear(TestTags.TopBarMenu)
        composeRule.onNodeWithContentDescription("Menu")

        val refreshBefore = appViewModel().state.value.lastManualRefreshAtMs
        composeRule.onNodeWithTag(TestTags.ReloadButton, useUnmergedTree = true).performClick()
        waitUntilNoError(5_000) { appViewModel().state.value.lastManualRefreshAtMs > refreshBefore }
        waitUntilNoError(10_000) { !appViewModel().state.value.isRefreshing }
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
    fun terminal_updates_live() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
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
        resetZoomPan()
        val baseline = waitForTerminalDebugInfo { info ->
            info.viewCols > 0 && info.viewRows > 0 && info.renderScaleX > 0f
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
                info.hash != zoomed.hash &&
                kotlin.math.abs(info.renderScaleX - zoomed.renderScaleX) <= 0.03f &&
                kotlin.math.abs(info.viewCols - zoomed.viewCols) <= 2 &&
                kotlin.math.abs(info.viewRows - zoomed.viewRows) <= 2
        } ?: throw AssertionError("zoomed viewport changed after a new terminal frame")

        backgroundAndResumeActivity()
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = SHORT_UI_TIMEOUT_MS)
        waitForTerminalDebugInfo(timeoutMs = SHORT_UI_TIMEOUT_MS) { info ->
            info.activeSessionId == afterFrame.activeSessionId &&
                kotlin.math.abs(info.renderScaleX - afterFrame.renderScaleX) <= 0.03f &&
                kotlin.math.abs(info.viewCols - afterFrame.viewCols) <= 2 &&
                kotlin.math.abs(info.viewRows - afterFrame.viewRows) <= 2 &&
                info.visibleStartRow == afterFrame.visibleStartRow &&
                info.visibleEndRowExclusive == afterFrame.visibleEndRowExclusive
        } ?: throw AssertionError("zoomed viewport reset after background/foreground cycle")
    }

    @Test
    fun terminal_fills_from_top_before_live_scrolling() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        assertTerminalResponsive()
        resetZoomPan()
        val baseline = waitForTerminalDebugInfo(TERMINAL_READY_TIMEOUT_MS) { info ->
            info.viewRows > 0 &&
                info.visibleStartRow == 0 &&
                info.visibleEndRowExclusive > info.visibleStartRow &&
                info.renderScaleX > 0f
        } ?: throw AssertionError("missing baseline terminal debug info")
        var zoomed = baseline
        for (attempt in 0 until 3) {
            setZoomFactor(zoomed.zoomFactor + 0.35f)
            zoomed = waitForTerminalDebugInfo(SHORT_UI_TIMEOUT_MS) { info ->
                info.renderScaleX > zoomed.renderScaleX + 0.03f &&
                    info.visibleStartRow == 0 &&
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
    fun resize_menu_absent_and_input_remains_blocked_without_control() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        setResizeHostEnabled(false)

        loginWithConfiguredUser()
        openMenu()
        composeRule.onAllNodesWithTag(TestTags.ResizeHostMenuItem, useUnmergedTree = true).assertCountEquals(0)
        composeRule.runOnIdle {
            appViewModel().setHasControlForTesting(false)
        }
        assertTerminalInputBlocked()
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
        openMenu()
        composeRule.onAllNodesWithTag(TestTags.ResizeHostMenuItem, useUnmergedTree = true).assertCountEquals(0)
        val info = readTerminalDebugInfo()
            ?: throw AssertionError("missing terminal debug info")
        assertTrue("view cols=${info.viewCols}", info.viewCols > 0)
        assertTrue("view rows=${info.viewRows}", info.viewRows > 0)
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
        ensureWallInactivityOff()

        val baselineWalls = wallEventCount()
        composeRule.onNodeWithTag(TestTags.WallInactivityButton).performClick()
        waitUntilNoError(5_000L) {
            appViewModel().state.value.wallInactivityEnabled &&
                appViewModel().state.value.wallInactivityLabel == "250ms"
        }
        waitUntilNoError(5_000L) { wallEventCount() == baselineWalls + 1 }

        selectSessionTab(second, timeoutMs = 10_000L)
        waitUntilNoError(10_000L) { activeSessionId() == second }
        selectSessionTab(first, timeoutMs = 10_000L)
        waitUntilNoError(10_000L) { activeSessionId() == first }
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)

        SystemClock.sleep(1500L)
        assertEquals(baselineWalls + 1, wallEventCount())
    }

    @Test
    fun wall_inactivity_banner_auto_dismisses_without_tab_switch() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        ensureWallInactivityOff()

        composeRule.onNodeWithTag(TestTags.WallInactivityButton).performClick()
        waitUntilNoError(5_000L) {
            readStatusBanner()?.message == "wall 250ms"
        }
        waitUntilNoError(6_000L) {
            readStatusBanner() == null
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
        captureScreenshot("keyboard-precheck")

        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        waitUntilNoError(10_000L) { hasTextNode("CTRL") }
        captureScreenshot("keyboard-visible-initial")

        InstrumentationRegistry.getInstrumentation().sendKeyDownUpSync(KeyEvent.KEYCODE_BACK)
        waitUntilNoError(10_000L) { !hasTextNode("CTRL") }
        captureScreenshot("keyboard-hidden")

        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        waitUntilNoError(10_000L) { hasTextNode("CTRL") }
        captureScreenshot("keyboard-visible-restored")
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
    fun background_foreground_resumes_input_without_tab_switch() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        val sessionId = activeSessionId()
        assertTerminalResponsive(sessionId)

        backgroundAndResumeActivity()
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = SHORT_UI_TIMEOUT_MS)
        waitUntilNoError(SHORT_UI_TIMEOUT_MS) {
            val info = readTerminalDebugInfo()
            info != null && info.state == "Connected" && info.activeSessionId == sessionId
        }
        assertTerminalResponsive(sessionId)
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
        ensureWallInactivityOff()
        syncWallCursorToLatest()

        composeRule.activity.runOnUiThread {
            appViewModel().setBackgroundWallEnabled(true)
        }
        composeRule.waitForIdle()
        waitUntilNoError(5_000L) { appViewModel().state.value.backgroundWallEnabled }

        composeRule.onNodeWithTag(TestTags.WallInactivityButton).performClick()
        waitUntilNoError(5_000L) {
            appViewModel().state.value.wallInactivityEnabled &&
                appViewModel().state.value.wallInactivityLabel == "250ms"
        }

        backgroundActivity()
        waitUntilNoError(10_000L) {
            activeNotifications().any { it.notification.channelId == "lingon_background_wall" }
        }
        waitUntilNoError(15_000L) {
            wallNotifications().isNotEmpty()
        }
        assertNoWallNotificationAutogroupSummary()
        val wallNotification = wallNotifications()
            .firstOrNull()
            ?: throw AssertionError("missing lingon_wall notification")
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
        assertEquals("$title: ${activeSessionId()} inactive", text)
        resumeActivity()
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = SHORT_UI_TIMEOUT_MS)
    }

    @Test
    fun headless_resize_button_only_enables_for_headless_sessions() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)
        composeRule.onNodeWithTag(TestTags.HeadlessResizeButton, useUnmergedTree = true).assertIsNotEnabled()

        val headlessId = startHeadlessViaHarness()
        waitUntilNoError(20_000L) { appViewModel().state.value.sessions.any { it.id == headlessId } }
        composeRule.onNodeWithTag(TestTags.tabTag(headlessId)).performClick()
        waitUntilNoError(10_000L) { activeSessionId() == headlessId && !stateForTest().sessionSyncing }

        composeRule.onNodeWithTag(TestTags.HeadlessResizeButton, useUnmergedTree = true).assertIsEnabled()
    }

    @Test
    fun headless_resize_button_resizes_remote_headless_session() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()

        loginWithConfiguredUser()
        waitForTagNoError(TestTags.TerminalInput)

        val headlessId = startHeadlessViaHarness()
        waitUntilNoError(20_000L) { appViewModel().state.value.sessions.any { it.id == headlessId } }
        composeRule.onNodeWithTag(TestTags.tabTag(headlessId)).performClick()
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
        composeRule.onNodeWithTag(TestTags.HeadlessResizeButton, useUnmergedTree = true).performClick()
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
        composeRule.onNodeWithTag(TestTags.tabTag(headlessId)).performClick()
        waitUntilNoError(10_000L) { activeSessionId() == headlessId && !stateForTest().sessionSyncing }

        detachHeadlessViaHarness(headlessId)

        waitUntilNoError(20_000L) {
            val state = stateForTest()
            state.sessions.none { it.id == headlessId } &&
                state.activeSessionId != headlessId &&
                !state.sessionSyncing
        }
        val banner = readStatusBanner()?.message.orEmpty()
        assertFalse("unexpected reconnect/not-connected banner: $banner", banner.contains("Not connected", ignoreCase = true))
        assertFalse("unexpected reconnect/not-connected banner: $banner", banner.contains("connection lost", ignoreCase = true))
    }

    @Test
    fun manual_wall_delivery_posts_system_notification() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        clearAppNotifications()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        composeRule.activity.runOnUiThread {
            appViewModel().setBackgroundWallEnabled(false)
        }
        composeRule.waitForIdle()
        waitUntilNoError(5_000L) { !appViewModel().state.value.backgroundWallEnabled }
        ensureWallInactivityOff()
        syncWallCursorToLatest()

        val message = "manual wall ${System.currentTimeMillis()}"
        sendWallViaHarness(message)

        waitUntilNoError(10_000L) {
            wallNotifications().any { wallNotificationText(it) == message }
        }
        assertNoWallNotificationAutogroupSummary()
        val wallNotification = wallNotifications()
            .firstOrNull { wallNotificationText(it) == message }
            ?: throw AssertionError("missing lingon_wall notification for message=$message")
        val title = wallNotificationTitle(wallNotification)
        val text = wallNotificationFullText(wallNotification)
        assertTrue("notification title missing username: $title", title.startsWith("${testConfig.username}@"))
        assertFalse("manual wall title should not include blank session suffix: $title", title.endsWith("#"))
        assertEquals("$title: $message", text)
    }

    @Test
    fun background_manual_wall_delivery_posts_system_notification() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        clearAppNotifications()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        ensureWallInactivityOff()
        syncWallCursorToLatest()

        composeRule.activity.runOnUiThread {
            appViewModel().setBackgroundWallEnabled(true)
        }
        composeRule.waitForIdle()
        waitUntilNoError(5_000L) { appViewModel().state.value.backgroundWallEnabled }

        backgroundActivity()
        waitUntilNoError(10_000L) {
            activeNotifications().any { it.notification.channelId == "lingon_background_wall" }
        }

        val message = "background manual wall ${System.currentTimeMillis()}"
        sendWallViaHarness(message)

        waitUntilNoError(15_000L) {
            wallNotifications().any { wallNotificationText(it) == message }
        }
        assertNoWallNotificationAutogroupSummary()
        val wallNotification = wallNotifications()
            .firstOrNull { wallNotificationText(it) == message }
            ?: throw AssertionError("missing lingon_wall notification for message=$message")
        val title = wallNotificationTitle(wallNotification)
        val text = wallNotificationFullText(wallNotification)
        assertTrue("notification title missing username: $title", title.startsWith("${testConfig.username}@"))
        assertFalse("manual wall title should not include blank session suffix: $title", title.endsWith("#"))
        assertEquals("$title: $message", text)

        resumeActivity()
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = SHORT_UI_TIMEOUT_MS)
    }

    @Test
    fun background_wall_delivery_stops_without_notification_permission_and_recovers_after_grant() {
        setEndpoint(testConfig.endpoint)
        ensureLoggedOut()
        clearAppNotifications()

        loginWithConfiguredUser()
        waitForTerminalReady(timeoutMs = TERMINAL_READY_TIMEOUT_MS)
        ensureWallInactivityOff()
        syncWallCursorToLatest()

        composeRule.activity.runOnUiThread {
            appViewModel().setBackgroundWallEnabled(true)
        }
        composeRule.waitForIdle()
        waitUntilNoError(5_000L) { appViewModel().state.value.backgroundWallEnabled }

        backgroundActivity()
        waitUntilNoError(10_000L) {
            activeNotifications().any { it.notification.channelId == "lingon_background_wall" }
        }

        assumeTrue(
            "runtime notification gating toggle unsupported on this device",
            setNotificationDeliveryEnabled(false),
        )
        clearWallNotifications()

        val blockedMessage = "permission blocked wall ${System.currentTimeMillis()}"
        sendWallViaHarness(blockedMessage)
        Thread.sleep(3_000L)
        assertFalse(
            "wall notification should be suppressed when POST_NOTIFICATIONS is revoked",
            activeNotifications().any { it.notification.channelId == "lingon_wall" },
        )

        assertTrue("failed to re-enable notifications", setNotificationDeliveryEnabled(true))
        val recoveredMessage = "permission restored wall ${System.currentTimeMillis()}"
        sendWallViaHarness(recoveredMessage)
        waitUntilNoError(15_000L) {
            wallNotifications().isNotEmpty()
        }
        assertNoWallNotificationAutogroupSummary()
        val wallNotification = wallNotifications()
            .firstOrNull()
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
        assertEquals("$title: $recoveredMessage", text)

        resumeActivity()
        waitForTagNoError(TestTags.TerminalInput, timeoutMs = SHORT_UI_TIMEOUT_MS)
    }

    @Test
    fun share_token_dialog_rejects_invalid_token() {
        openMenu()
        composeRule.onNodeWithTag(TestTags.ShareTokenButton).performClick()
        waitForTag(TestTags.ShareTokenInput)
        composeRule.onNodeWithTag(TestTags.ShareTokenInput).performTextReplacement("not-a-token")
        composeRule.onNodeWithTag(TestTags.ShareTokenAttach).performClick()
        waitForTag(TestTags.ShareTokenError)
        composeRule.onNodeWithText("Cancel").performClick()
        waitForTagToDisappear(TestTags.ShareTokenInput)
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

    private fun waitForSessionTick(sessionId: String, timeoutMs: Long = 10_000L) {
        waitForTerminalReady(timeoutMs)
        waitUntilNoError(timeoutMs) { snapshotContainsToken("TICK_$sessionId") }
    }

    private fun openMenu() {
        composeRule.onNodeWithTag(TestTags.TopBarMenuButton).performClick()
        waitForTag(TestTags.TopBarMenu)
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
        composeRule.activityRule.scenario.moveToState(Lifecycle.State.CREATED)
        composeRule.waitForIdle()
    }

    private fun resumeActivity() {
        composeRule.activityRule.scenario.moveToState(Lifecycle.State.RESUMED)
        composeRule.waitForIdle()
    }

    private fun attachViaShareToken(token: String) {
        openMenu()
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
            app.repository.listWallEvents(sinceId = 0, limit = 32).events.size
        }
    }

    private fun clearAppNotifications() {
        composeRule.activity.runOnUiThread {
            composeRule.activity.getSystemService(NotificationManager::class.java)?.cancelAll()
        }
        composeRule.waitForIdle()
    }

    private fun clearWallNotifications() {
        composeRule.activity.runOnUiThread {
            val manager = composeRule.activity.getSystemService(NotificationManager::class.java) ?: return@runOnUiThread
            manager.activeNotifications
                .filter {
                    it.notification.channelId == "lingon_wall" ||
                        it.notification.channelId == "lingon_background_wall"
                }
                .forEach { manager.cancel(it.id) }
        }
        composeRule.waitForIdle()
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
            app.wallDeliveryCoordinator.advanceCursor(endpoint, latest)
        }
    }

    private fun setNotificationDeliveryEnabled(enabled: Boolean): Boolean {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        consumeShellCommand(
            instrumentation.uiAutomation.executeShellCommand(
                if (enabled) {
                    "cmd appops set --uid systems.pkt.lingon POST_NOTIFICATION allow"
                } else {
                    "cmd appops set --uid systems.pkt.lingon POST_NOTIFICATION deny"
                },
            ),
        )
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

    private fun notificationsEnabled(): Boolean {
        return NotificationManagerCompat.from(composeRule.activity).areNotificationsEnabled()
    }

    private fun ensureWallInactivityOff() {
        waitUntilNoError(5_000L) { appViewModel().state.value.connectionState == ConnectionState.Connected }
        repeat(4) {
            val state = appViewModel().state.value
            if (!state.wallInactivityEnabled) {
                return
            }
            val previousLabel = state.wallInactivityLabel
            composeRule.onNodeWithTag(TestTags.WallInactivityButton).performClick()
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

    private fun activeNotifications(): Array<StatusBarNotification> {
        return composeRule.activity.getSystemService(NotificationManager::class.java)
            ?.activeNotifications
            ?: emptyArray()
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

    private fun wallNotificationText(notification: StatusBarNotification): String {
        return notification.notification.extras
            .getCharSequence(Notification.EXTRA_TEXT)
            ?.toString()
            ?.trim()
            .orEmpty()
            .substringAfter(": ", "")
    }

    private fun wallNotificationFullText(notification: StatusBarNotification): String {
        return notification.notification.extras
            .getCharSequence(Notification.EXTRA_TEXT)
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
            throw AssertionError("unexpected lingon_wall auto-group summary title=$title text=$text")
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
        val echoChar = "Z"
        val echoHex = echoChar[0].code.toString(16).padStart(2, '0')
        composeRule.onNodeWithTag(TestTags.TerminalFocus).performClick()
        sendTerminalInput(echoChar)
        sendTerminalEnter()
        val deadline = System.currentTimeMillis() + 1_000
        while (System.currentTimeMillis() < deadline) {
            val info = readTerminalDebugInfo()
            if (info != null) {
                val lines = listOf(info.row0, info.prevLine, info.tail, info.cursorLine)
                if (lines.any { it.contains("ECHO_") && it.contains(echoHex) }) {
                    throw AssertionError("unexpected terminal echo while input is disabled")
                }
            }
            composeRule.waitForIdle()
            Thread.sleep(POLL_INTERVAL_MS)
        }
    }

    private fun setResizeHostEnabled(enabled: Boolean) {
        composeRule.activity.runOnUiThread {
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
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        if (shift) {
            instrumentation.sendKeySync(KeyEvent(KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_SHIFT_LEFT))
        }
        instrumentation.sendKeySync(KeyEvent(KeyEvent.ACTION_DOWN, keyCode))
        instrumentation.sendKeySync(KeyEvent(KeyEvent.ACTION_UP, keyCode))
        if (shift) {
            instrumentation.sendKeySync(KeyEvent(KeyEvent.ACTION_UP, KeyEvent.KEYCODE_SHIFT_LEFT))
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
        composeRule.onNodeWithTag(TestTags.TerminalInput, useUnmergedTree = true).performClick()
        composeRule.waitForIdle()
    }

    private fun setEndpointViaUi(endpoint: String) {
        composeRule.onNodeWithTag(TestTags.TopBarMenuButton).performClick()
        composeRule.onNodeWithTag(TestTags.EndpointButton).performClick()
        waitForTag(TestTags.EndpointInput)
        composeRule.onNodeWithTag(TestTags.EndpointInput).performTextReplacement(endpoint)
        composeRule.onNodeWithTag(TestTags.EndpointSave).performClick()
        waitForTagToDisappear(TestTags.EndpointInput)
    }

    private fun configureEndpointAndCerts() {
        setEndpoint(testConfig.endpoint)
    }

    private fun ensureLoggedOut() {
        if (hasTag(TestTags.LoginUsername)) return
        composeRule.activity.runOnUiThread {
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

    private fun clickNodeCenter(tag: String) {
        val node = composeRule.onNodeWithTag(tag).fetchSemanticsNode()
        val bounds = node.boundsInRoot
        composeRule.onNodeWithTag(tag).performTouchInput {
            down(0, Offset(bounds.center.x, bounds.center.y))
            up(0)
        }
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
                hasControl = state.hasControl,
                resizeEnabled = state.resizeHostEnabled,
                viewCols = terminalView?.getViewCols() ?: state.terminalCols,
                viewRows = terminalView?.getViewRows() ?: state.terminalRows,
                renderScaleX = terminalView?.getRenderScaleX() ?: 0f,
                renderScaleY = terminalView?.getRenderScaleY() ?: 0f,
                visibleStartRow = terminalView?.getVisibleStartRow() ?: 0,
                visibleEndRowExclusive = terminalView?.getVisibleEndRowExclusive() ?: 0,
                zoomFactor = state.zoomFactor,
            )
        }
        return info
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
        composeRule.runOnIdle {
            status = appViewModel().state.value.bannerStatus
        }
        val current = status ?: return null
        return StatusInfo(level = current.level.name, message = current.message)
    }

    private fun readLoginError(): String? {
        var message: String? = null
        composeRule.runOnIdle {
            message = appViewModel().state.value.loginError
        }
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
        composeRule.onNodeWithTag(TestTags.TopBarMenuButton).performClick()
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

    private fun waitUntil(timeoutMs: Long = DEFAULT_TIMEOUT_MS, condition: () -> Boolean) {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            if (condition()) return
            composeRule.waitForIdle()
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
            composeRule.waitForIdle()
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
                "scrollbackOffset=${debug?.scrollbackOffsetRows ?: 0})",
        )
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

    private fun resetZoomPan() {
        openMenu()
        composeRule.onNodeWithTag(TestTags.ZoomResetButton).performClick()
    }

    private fun setZoomFactor(value: Float) {
        composeRule.activity.runOnUiThread { appViewModel().updateZoomFactor(value) }
        composeRule.waitForIdle()
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

    private fun startHeadlessViaHarness(): String {
        val url = URL("${testConfig.endpoint.trimEnd('/')}/__harness/start-headless")
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
            return org.json.JSONObject(body).getString("id")
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
        } finally {
            connection.disconnect()
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
                val hostCols = args.getString("host_cols")?.toIntOrNull() ?: 80
                val hostRows = args.getString("host_rows")?.toIntOrNull() ?: 24
                return TestConfig(
                    endpoint = endpoint,
                    username = username,
                    password = password,
                    totpSecret = totpSecret,
                    caPem = caPem,
                    sessions = sessions,
                    viewToken = viewToken,
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
        val hasControl: Boolean,
        val resizeEnabled: Boolean,
        val viewCols: Int,
        val viewRows: Int,
        val renderScaleX: Float,
        val renderScaleY: Float,
        val visibleStartRow: Int,
        val visibleEndRowExclusive: Int,
        val zoomFactor: Float,
    )

    data class TerminalRenderInfo(
        val widthPx: Int,
        val heightPx: Int,
        val inkLeft: Int,
        val inkTop: Int,
        val inkRight: Int,
        val inkBottom: Int,
    )
}
