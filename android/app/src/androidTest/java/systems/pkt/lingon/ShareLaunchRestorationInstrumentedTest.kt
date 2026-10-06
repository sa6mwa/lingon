package systems.pkt.lingon

import android.Manifest
import android.content.Intent
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
import org.junit.Test
import org.junit.Rule
import org.junit.runner.RunWith
import systems.pkt.lingon.viewmodel.AppViewModel

@RunWith(AndroidJUnit4::class)
class ShareLaunchRestorationInstrumentedTest {
    @get:Rule
    val notificationPermissionRule: GrantPermissionRule =
        GrantPermissionRule.grant(Manifest.permission.POST_NOTIFICATIONS)

    @Test
    fun consumedShareSurvivesLossOfPendingRequestAndViewModel() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val token = "trusted-restoration-token"
        val requestId = PendingShareLaunches.put(token, "https://127.0.0.1:1")
        val intent = Intent(context, MainActivity::class.java).apply {
            action = MainActivity.ACTION_INTERNAL_SHARE
            putExtra(MainActivity.EXTRA_SHARE_REQUEST_ID, requestId)
        }
        ActivityScenario.launch<MainActivity>(intent).use { scenario ->
            scenario.onActivity { activity ->
                val model = ViewModelProvider(activity)[AppViewModel::class.java]
                assertEquals(token, model.state.value.shareToken)
                assertNull(PendingShareLaunches.take(requestId))
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
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val requestId = PendingShareLaunches.put("logged-out-restoration-token", "https://127.0.0.1:1")
        val intent = Intent(context, MainActivity::class.java).apply {
            action = MainActivity.ACTION_INTERNAL_SHARE
            putExtra(MainActivity.EXTRA_SHARE_REQUEST_ID, requestId)
        }
        ActivityScenario.launch<MainActivity>(intent).use { scenario ->
            var model: AppViewModel? = null
            scenario.onActivity { activity ->
                val current = ViewModelProvider(activity)[AppViewModel::class.java]
                model = current
                assertEquals("logged-out-restoration-token", current.state.value.shareToken)
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
