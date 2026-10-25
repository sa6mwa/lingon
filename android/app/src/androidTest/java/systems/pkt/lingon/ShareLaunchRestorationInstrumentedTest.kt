package systems.pkt.lingon

import android.Manifest
import android.content.Intent
import android.util.Base64
import androidx.lifecycle.ViewModelProvider
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.rule.GrantPermissionRule
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.Rule
import org.junit.Before
import org.junit.runner.RunWith
import systems.pkt.lingon.viewmodel.AppViewModel
import systems.pkt.lingon.viewmodel.ConnectionState

@RunWith(AndroidJUnit4::class)
class ShareLaunchRestorationInstrumentedTest {
    @get:Rule
    val testTimeout: org.junit.rules.Timeout = org.junit.rules.Timeout.seconds(30)

    private val endpoint: String
        get() = requireNotNull(InstrumentationRegistry.getArguments().getString("endpoint"))

    private val shareToken: String
        get() = requireNotNull(InstrumentationRegistry.getArguments().getString("view_token"))

    @Before
    fun configureOwnedRelay() = runBlocking {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val app = instrumentation.targetContext.applicationContext as LingonApplication
        val ca = requireNotNull(InstrumentationRegistry.getArguments().getString("ca_pem_b64"))
        app.certificateStore.addCertificates(endpoint, String(Base64.decode(ca, Base64.DEFAULT), Charsets.UTF_8))
        // Launch a real shared session. Permanent connection errors keep the
        // syncing animation running while ActivityScenario waits for idle.
        app.repository.clearAuth()
        app.repository.clearLastActiveSession()
        app.repository.setEndpoint(endpoint)
    }

    @get:Rule
    val notificationPermissionRule: GrantPermissionRule =
        GrantPermissionRule.grant(Manifest.permission.POST_NOTIFICATIONS)

    @Test
    fun trustedNewIntentKeepsLaunchIdentityAndRestoresShare() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val token = shareToken
        val requestId = PendingShareLaunches.put(token, endpoint)
        ActivityScenario.launch(MainActivity::class.java).use { scenario ->
            scenario.onActivity { activity ->
                val launchIdentity = Intent(activity.intent)
                val incoming = Intent(activity, MainActivity::class.java).apply {
                    action = MainActivity.ACTION_INTERNAL_SHARE
                    putExtra(MainActivity.EXTRA_SHARE_REQUEST_ID, requestId)
                }
                try {
                    instrumentation.callActivityOnNewIntent(activity, incoming)
                    assertEquals(token, ViewModelProvider(activity)[AppViewModel::class.java].state.value.shareToken)
                    assertTrue("new share must retain activity launch identity", activity.intent.filterEquals(launchIdentity))
                } finally {
                    // Preserve test-owned lifecycle cleanup even on the pre-fix
                    // failure; the identity assertion above remains mandatory.
                    activity.intent = launchIdentity
                }
                activity.viewModelStore.clear()
            }
            scenario.recreate()
            scenario.onActivity { activity ->
                assertEquals(token, ViewModelProvider(activity)[AppViewModel::class.java].state.value.shareToken)
            }
        }
    }

    @Test
    fun consumedShareSurvivesLossOfPendingRequestAndViewModel() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val token = shareToken
        val requestId = PendingShareLaunches.put(token, endpoint)
        val intent = Intent(context, MainActivity::class.java).apply {
            action = MainActivity.ACTION_INTERNAL_SHARE
            putExtra(MainActivity.EXTRA_SHARE_REQUEST_ID, requestId)
        }
        ActivityScenario.launch<MainActivity>(intent).use { scenario ->
            var initialModel: AppViewModel? = null
            scenario.onActivity { activity ->
                val model = ViewModelProvider(activity)[AppViewModel::class.java]
                initialModel = model
                assertEquals(token, model.state.value.shareToken)
                assertNull(PendingShareLaunches.take(requestId))
            }
            runBlocking {
                withTimeout(15_000) {
                    requireNotNull(initialModel).state.first {
                        it.connectionState == ConnectionState.Connected && it.activeSnapshot != null
                    }
                }
            }
            scenario.recreate()
            scenario.onActivity { activity ->
                assertEquals(token, ViewModelProvider(activity)[AppViewModel::class.java].state.value.shareToken)
                // Drop the process-resident ViewModel before recreation. The
                // consumed request is already absent, so only saved state can
                // restore this launch into the replacement ViewModel.
                activity.viewModelStore.clear()
            }
            scenario.recreate()
            scenario.onActivity { activity ->
                val model = ViewModelProvider(activity)[AppViewModel::class.java]
                assertEquals(token, model.state.value.shareToken)
            }
        }
    }

    @Test
    fun loggedOutShareIsNotRestored() {
        // Exercise fresh launches against application state retained by prior
        // logout/recreation cycles, rather than only the first clean launch.
        repeat(8) { assertLoggedOutShareIsNotRestored() }
    }

    private fun assertLoggedOutShareIsNotRestored() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val token = shareToken
        val requestId = PendingShareLaunches.put(token, endpoint)
        val intent = Intent(context, MainActivity::class.java).apply {
            action = MainActivity.ACTION_INTERNAL_SHARE
            putExtra(MainActivity.EXTRA_SHARE_REQUEST_ID, requestId)
        }
        ActivityScenario.launch<MainActivity>(intent).use { scenario ->
            var model: AppViewModel? = null
            scenario.onActivity { activity ->
                val current = ViewModelProvider(activity)[AppViewModel::class.java]
                model = current
                assertEquals(token, current.state.value.shareToken)
                current.logout()
            }
            runBlocking {
                withTimeout(15_000) {
                    requireNotNull(model).state.first { it.shareToken == null }
                }
            }
            scenario.onActivity { it.viewModelStore.clear() }
            scenario.recreate()
            scenario.onActivity { activity ->
                assertNull(ViewModelProvider(activity)[AppViewModel::class.java].state.value.shareToken)
            }
        }
    }

    @Test
    fun forgedIntentExtrasDoNotAuthorizeRestoration() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val intent = Intent(context, MainActivity::class.java).apply {
            action = MainActivity.ACTION_INTERNAL_SHARE
            putExtra(MainActivity.EXTRA_SHARE_REQUEST_ID, "unknown-request")
            putExtra("share_token", "forged-token")
            putExtra("trusted_share_token", "forged-token")
        }
        ActivityScenario.launch<MainActivity>(intent).use { scenario ->
            scenario.onActivity { activity ->
                assertNull(ViewModelProvider(activity)[AppViewModel::class.java].state.value.shareToken)
                activity.viewModelStore.clear()
            }
            scenario.recreate()
            scenario.onActivity { activity ->
                assertNull(ViewModelProvider(activity)[AppViewModel::class.java].state.value.shareToken)
            }
        }
    }
}
