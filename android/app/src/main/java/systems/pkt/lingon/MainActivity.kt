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
        AppViewModelFactory(app.repository, app.wsClient, app.wallNotifier, app.wallWorkScheduler)
    }
    private var unlockPromptInFlight = false
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
        intent?.let { incoming ->
            val endpointOverride = incoming.getStringExtra(EXTRA_ENDPOINT_OVERRIDE)
            val shareToken = incoming.getStringExtra(EXTRA_SHARE_TOKEN)
            if (!shareToken.isNullOrBlank()) {
                viewModel.handleSharedToken(shareToken, endpointOverride)
            } else if (!endpointOverride.isNullOrBlank()) {
                viewModel.updateEndpoint(endpointOverride)
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
        val endpointOverride = intent.getStringExtra(EXTRA_ENDPOINT_OVERRIDE)
        val shareToken = intent.getStringExtra(EXTRA_SHARE_TOKEN)
        if (!shareToken.isNullOrBlank()) {
            viewModel.handleSharedToken(shareToken, endpointOverride)
        } else if (!endpointOverride.isNullOrBlank()) {
            viewModel.updateEndpoint(endpointOverride)
        }
    }

    override fun onStart() {
        super.onStart()
        viewModel.onAppForeground()
    }

    override fun onStop() {
        super.onStop()
        if (!isChangingConfigurations) {
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
        const val EXTRA_SHARE_TOKEN = "share_token"
        const val EXTRA_ENDPOINT_OVERRIDE = "endpoint_override"
    }
}
