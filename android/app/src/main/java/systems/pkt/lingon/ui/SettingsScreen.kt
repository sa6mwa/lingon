package systems.pkt.lingon.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

@Composable
fun SettingsScreen(
    username: String?,
    loggedIn: Boolean,
    wallInactivityEnabled: Boolean,
    wallInactivityLabel: String?,
    wallInactivityAvailable: Boolean,
    onToggleWallInactivity: () -> Unit,
    headlessResizeAvailable: Boolean,
    headlessResizeEnabled: Boolean,
    onResizeHeadlessNow: () -> Unit,
    backgroundWallEnabled: Boolean,
    onToggleBackgroundWall: (Boolean) -> Unit,
    followOnReadEnabled: Boolean,
    onToggleFollowOnRead: (Boolean) -> Unit,
    onShowEndpoint: () -> Unit,
    onShowShareToken: () -> Unit,
    onShowCertificates: () -> Unit,
    onShowTheme: () -> Unit,
    onShowAppLock: () -> Unit,
    onResetZoomPan: () -> Unit,
    onLogout: () -> Unit,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    BackHandler(onBack = onBack)

    Surface(
        modifier = modifier
            .fillMaxSize()
            .testTag(TestTags.SettingsScreen),
        color = MaterialTheme.colorScheme.background,
    ) {
        Column(modifier = Modifier.fillMaxSize()) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .statusBarsPadding()
                    .padding(horizontal = 8.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                IconButton(
                    onClick = onBack,
                    modifier = Modifier
                        .size(40.dp)
                        .testTag(TestTags.SettingsBackButton),
                ) {
                    Icon(
                        imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                        contentDescription = "Back",
                        tint = MaterialTheme.colorScheme.onSurface,
                    )
                }
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "Settings",
                        style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.SemiBold),
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    if (!username.isNullOrBlank()) {
                        Text(
                            text = "Signed in as $username",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
            }

            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(
                    start = 16.dp,
                    end = 16.dp,
                    bottom = 24.dp,
                ),
            ) {
                item {
                    SettingsSectionTitle("Connection")
                    SettingsActionRow("Endpoint", onShowEndpoint, Modifier.testTag(TestTags.EndpointButton))
                    SettingsActionRow("Attach via token", onShowShareToken, Modifier.testTag(TestTags.ShareTokenButton))
                    SettingsActionRow("Manage certificates", onShowCertificates, Modifier.testTag(TestTags.CertificatesButton))
                }

                item {
                    SettingsSectionTitle("Terminal")
                    SettingsSwitchRow(
                        title = "Follow on read",
                        checked = followOnReadEnabled,
                        onCheckedChange = onToggleFollowOnRead,
                        modifier = Modifier.testTag(TestTags.FollowOnReadMenuItem),
                        switchModifier = Modifier.testTag(TestTags.FollowOnReadToggle),
                    )
                    SettingsActionRow("Reset zoom/pan", onResetZoomPan, Modifier.testTag(TestTags.ZoomResetButton))
                    if (headlessResizeAvailable) {
                        SettingsActionRow(
                            title = if (headlessResizeEnabled) {
                                "Resize headless session"
                            } else {
                                "Resize unavailable for non-headless session"
                            },
                            onClick = onResizeHeadlessNow,
                            modifier = Modifier.testTag(TestTags.HeadlessResizeButton),
                            enabled = headlessResizeEnabled,
                        )
                    }
                }

                item {
                    SettingsSectionTitle("Notifications")
                    SettingsSwitchRow(
                        title = "Background wall notifications",
                        checked = backgroundWallEnabled,
                        onCheckedChange = onToggleBackgroundWall,
                        modifier = Modifier.testTag(TestTags.BackgroundWallMenuItem),
                        switchModifier = Modifier.testTag(TestTags.BackgroundWallToggle),
                    )
                    if (wallInactivityAvailable) {
                        val label = wallInactivityLabel?.takeIf { it.isNotBlank() }
                        SettingsActionRow(
                            title = if (wallInactivityEnabled) {
                                "Wall inactivity ${label ?: "on"}"
                            } else {
                                "Wall inactivity off"
                            },
                            onClick = onToggleWallInactivity,
                            modifier = Modifier.testTag(TestTags.WallInactivityButton),
                        )
                    }
                }

                item {
                    SettingsSectionTitle("Appearance")
                    SettingsActionRow("Select theme", onShowTheme, Modifier.testTag(TestTags.ThemeButton))
                }

                item {
                    SettingsSectionTitle("Security")
                    SettingsActionRow("App lock timeout", onShowAppLock, Modifier.testTag(TestTags.AppLockTimeoutButton))
                    if (loggedIn) {
                        SettingsActionRow("Logout", onLogout, Modifier.testTag(TestTags.LogoutButton))
                    }
                }
            }
        }
    }
}

@Composable
private fun SettingsSectionTitle(title: String) {
    Spacer(modifier = Modifier.height(18.dp))
    Text(
        text = title,
        style = MaterialTheme.typography.labelLarge.copy(fontWeight = FontWeight.SemiBold),
        color = MaterialTheme.colorScheme.primary,
        modifier = Modifier.padding(bottom = 6.dp),
    )
}

@Composable
private fun SettingsActionRow(
    title: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    Surface(
        modifier = modifier
            .fillMaxWidth()
            .clickable(enabled = enabled, onClick = onClick),
        color = MaterialTheme.colorScheme.surface,
        contentColor = MaterialTheme.colorScheme.onSurface.copy(alpha = if (enabled) 1f else 0.38f),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 14.dp, vertical = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = title,
                style = MaterialTheme.typography.bodyLarge,
                modifier = Modifier.weight(1f),
            )
            Icon(
                imageVector = Icons.Filled.ChevronRight,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = if (enabled) 1f else 0.38f),
            )
        }
    }
    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
}

@Composable
private fun SettingsSwitchRow(
    title: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
    switchModifier: Modifier = Modifier,
) {
    Surface(
        modifier = modifier
            .fillMaxWidth()
            .clickable { onCheckedChange(!checked) },
        color = MaterialTheme.colorScheme.surface,
        contentColor = MaterialTheme.colorScheme.onSurface,
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 14.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = title,
                style = MaterialTheme.typography.bodyLarge,
                modifier = Modifier.weight(1f),
            )
            Switch(
                checked = checked,
                onCheckedChange = onCheckedChange,
                modifier = switchModifier,
            )
        }
    }
    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
}
