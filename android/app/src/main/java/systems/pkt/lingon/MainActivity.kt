package systems.pkt.lingon

import android.Manifest
import android.content.Intent
import android.app.KeyguardManager
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import systems.pkt.lingon.ui.LingonApp
import systems.pkt.lingon.viewmodel.AppViewModel
import systems.pkt.lingon.viewmodel.AppViewModelFactory

class MainActivity : ComponentActivity() {
    private val viewModel: AppViewModel by viewModels {
        val app = application as LingonApplication
        AppViewModelFactory(
            app.repository,
            app.wsClient,
            app.wallDeliveryCoordinator,
            app.wallWorkScheduler,
            app.backgroundWallServiceController,
        )
    }
    private var unlockPromptInFlight = false
    private var trustedShareLaunch: PendingShareLaunch? = null
    private val unlockLauncher =
        registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
            unlockPromptInFlight = false
            if (result.resultCode == RESULT_OK) {
                viewModel.onAppUnlockSucceeded()
            } else {
                viewModel.onAppUnlockCancelled()
            }
        }
    private val notificationPermissionLauncher =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { _ -> }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (!handleIncomingIntent(intent)) {
            val token = savedInstanceState?.getString(STATE_TRUSTED_SHARE_TOKEN)
            if (!token.isNullOrBlank()) {
                val restored = PendingShareLaunch(token, savedInstanceState.getString(STATE_TRUSTED_SHARE_ENDPOINT))
                trustedShareLaunch = restored
                // Ordinary configuration changes retain this ViewModel. Only
                // replay the trusted context when the replacement lost it.
                if (viewModel.state.value.shareToken != token) {
                    applyShareLaunch(restored)
                }
            }
        }
        setContent {
            LingonApp(viewModel)
        }
        maybeRequestNotificationPermission()
        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                viewModel.state.collect { state ->
                    maybeLaunchAppUnlock(state.requiresAppUnlock, state.unlockPromptPending)
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleIncomingIntent(intent)
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        outState.remove(STATE_TRUSTED_SHARE_TOKEN)
        outState.remove(STATE_TRUSTED_SHARE_ENDPOINT)
        val token = trustedShareLaunch?.shareToken
        val current = viewModel.state.value
        // Logout or leaving the share must not resurrect a consumed launch.
        if (!token.isNullOrBlank() && current.shareToken == token) {
            outState.putString(STATE_TRUSTED_SHARE_TOKEN, token)
            outState.putString(STATE_TRUSTED_SHARE_ENDPOINT, trustedShareLaunch?.endpointOverride ?: current.endpoint)
        }
    }

    private fun handleIncomingIntent(intent: Intent?): Boolean {
        if (intent?.action != ACTION_INTERNAL_SHARE) return false
        val pending = PendingShareLaunches.take(intent.getStringExtra(EXTRA_SHARE_REQUEST_ID)) ?: return false
        trustedShareLaunch = pending
        applyShareLaunch(pending)
        return true
    }

    private fun applyShareLaunch(pending: PendingShareLaunch) {
        val endpointOverride = pending.endpointOverride
        val shareToken = pending.shareToken
        if (!shareToken.isNullOrBlank()) {
            viewModel.handleSharedToken(shareToken, endpointOverride)
        } else if (!endpointOverride.isNullOrBlank()) {
            viewModel.updateEndpoint(endpointOverride)
        }
    }

    override fun onStart() {
        super.onStart()
        (application as LingonApplication).setAppInForeground(true)
        viewModel.onAppForeground()
    }

    override fun onStop() {
        super.onStop()
        if (!isChangingConfigurations) {
            (application as LingonApplication).setAppInForeground(false)
            viewModel.onAppBackground()
        }
    }

    private fun maybeLaunchAppUnlock(requiresUnlock: Boolean, promptPending: Boolean) {
        if (!requiresUnlock || !promptPending || unlockPromptInFlight) return
        viewModel.onUnlockPromptLaunched()
        val keyguard = getSystemService(KeyguardManager::class.java)
        if (keyguard == null || !keyguard.isDeviceSecure) {
            viewModel.onAppUnlockSucceeded()
            return
        }
        @Suppress("DEPRECATION")
        val intent = keyguard.createConfirmDeviceCredentialIntent(
            "Unlock Lingon",
            "Unlock to continue",
        )
        if (intent == null) {
            viewModel.onAppUnlockSucceeded()
            return
        }
        unlockPromptInFlight = true
        unlockLauncher.launch(intent)
    }

    private fun maybeRequestNotificationPermission() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) {
            return
        }
        if (checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED) {
            return
        }
        notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
    }

    companion object {
        const val ACTION_INTERNAL_SHARE = "systems.pkt.lingon.action.INTERNAL_SHARE"
        const val EXTRA_SHARE_REQUEST_ID = "share_request_id"
        private const val STATE_TRUSTED_SHARE_TOKEN = "trusted_share_token"
        private const val STATE_TRUSTED_SHARE_ENDPOINT = "trusted_share_endpoint"
    }
}
