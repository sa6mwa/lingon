package systems.pkt.lingon.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.Button
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import systems.pkt.lingon.share.ShareTokens
import systems.pkt.lingon.ui.dialogs.AppLockTimeoutDialog
import systems.pkt.lingon.ui.dialogs.EndpointDialog
import systems.pkt.lingon.ui.dialogs.ShareTokenDialog
import systems.pkt.lingon.ui.dialogs.ThemeDialog
import systems.pkt.lingon.ui.theme.LingonTheme
import systems.pkt.lingon.viewmodel.AppViewModel
import systems.pkt.lingon.viewmodel.StatusLevel

@Composable
fun LingonApp(viewModel: AppViewModel) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val title = if (state.canAttach && !state.showCertificates) {
        endpointHost(state.endpoint) ?: "Lingon"
    } else {
        "Lingon"
    }
    var menuExpanded by remember { mutableStateOf(false) }

    LingonTheme(themeName = state.theme) {
        Surface(color = MaterialTheme.colorScheme.background) {
            Column(modifier = Modifier.fillMaxSize()) {
                if (!state.canAttach || state.showCertificates) {
                    TopBar(
                        title = title,
                        username = state.username,
                        loggedIn = state.loggedIn,
                        menuExpanded = menuExpanded,
                        onToggleMenu = { menuExpanded = true },
                        onDismissMenu = { menuExpanded = false },
                        onShowSettings = { viewModel.showSettings(true) },
                        onShowTheme = { viewModel.showThemePicker(true) },
                        onShowAppLock = { viewModel.showAppLockTimeoutDialog(true) },
                        onResetZoomPan = { viewModel.resetZoomAndPan() },
                        wallInactivityEnabled = state.wallInactivityEnabled,
                        wallInactivityLabel = state.wallInactivityLabel,
                        wallInactivityAvailable = false,
                        onToggleWallInactivity = { viewModel.toggleWallInactivity() },
                        onReload = { viewModel.manualRefresh() },
                        onShowShareToken = { viewModel.showShareToken(true, state.shareToken) },
                        onShowCertificates = { viewModel.showCertificates(true) },
                        resizeHostEnabled = state.resizeHostEnabled,
                        resizeHostAvailable = state.hasControl,
                        onToggleResizeHost = { enabled -> viewModel.setResizeHostEnabled(enabled) },
                        backgroundWallEnabled = state.backgroundWallEnabled,
                        onToggleBackgroundWall = { enabled -> viewModel.setBackgroundWallEnabled(enabled) },
                        onLogout = { viewModel.logout() },
                        compact = false,
                        vertical = false,
                    )
                }
                if (state.requiresAppUnlock) {
                    LockedScreen(onUnlock = { viewModel.requestAppUnlockPrompt() })
                } else if (state.showCertificates) {
                    ManageCertificatesScreen(state = state, viewModel = viewModel)
                } else if (state.canAttach) {
                    TerminalScreen(
                        title = title,
                        state = state,
                        viewModel = viewModel,
                        menuExpanded = menuExpanded,
                        onToggleMenu = { menuExpanded = true },
                        onDismissMenu = { menuExpanded = false },
                    )
                } else {
                    LoginScreen(state = state, viewModel = viewModel)
                }
            }
        }

        if (state.showSettings) {
            EndpointDialog(
                endpoint = state.endpoint,
                onDismiss = { viewModel.showSettings(false) },
                onSave = { value ->
                    viewModel.updateEndpoint(value)
                    viewModel.showSettings(false)
                },
            )
        }

        if (state.showThemePicker) {
            ThemeDialog(
                currentTheme = state.theme,
                onDismiss = { viewModel.showThemePicker(false) },
                onSave = { theme ->
                    viewModel.setTheme(theme)
                    viewModel.showThemePicker(false)
                },
            )
        }

        if (state.showAppLockTimeoutDialog) {
            AppLockTimeoutDialog(
                currentMinutes = state.appLockTimeoutMinutes,
                onDismiss = { viewModel.showAppLockTimeoutDialog(false) },
                onSave = { minutes ->
                    viewModel.setAppLockTimeoutMinutes(minutes)
                    viewModel.showAppLockTimeoutDialog(false)
                },
            )
        }

        if (state.showShareToken) {
            ShareTokenDialog(
                token = state.shareToken,
                errorMessage = state.shareTokenError,
                onDismiss = { viewModel.showShareToken(false) },
                onAttach = { raw ->
                    val parsed = ShareTokens.parse(raw)
                    if (parsed == null) {
                        viewModel.setShareTokenError("invalid share token")
                        return@ShareTokenDialog
                    }
                    val bare = ShareTokens.bareToken(parsed)
                    if (bare == null) {
                        viewModel.setShareTokenError("invalid share token")
                        return@ShareTokenDialog
                    }
                    viewModel.showShareToken(false, bare)
                    viewModel.handleSharedToken(bare, parsed.endpoint)
                    viewModel.setShareTokenError(null)
                    viewModel.showStatus("share token set", StatusLevel.Info)
                },
                onError = { message -> viewModel.setShareTokenError(message) },
            )
        }
    }
}

@Composable
private fun LockedScreen(onUnlock: () -> Unit) {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(
                text = "App locked",
                style = MaterialTheme.typography.headlineSmall,
            )
            Text(
                text = "Unlock to continue",
                style = MaterialTheme.typography.bodyMedium,
            )
            Button(onClick = onUnlock) {
                Text("Unlock")
            }
        }
    }
}


private fun endpointHost(endpoint: String): String? {
    val trimmed = endpoint.trim()
    if (trimmed.isBlank()) return null
    val withScheme = if (trimmed.contains("://")) trimmed else "https://$trimmed"
    return withScheme.toHttpUrlOrNull()?.host
}
