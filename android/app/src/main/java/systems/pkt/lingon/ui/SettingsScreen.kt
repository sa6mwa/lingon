package systems.pkt.lingon.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Logout
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AccountCircle
import androidx.compose.material.icons.filled.Campaign
import androidx.compose.material.icons.filled.ChevronRight
import androidx.compose.material.icons.filled.ColorLens
import androidx.compose.material.icons.filled.Link
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.ResetTv
import androidx.compose.material.icons.filled.Security
import androidx.compose.material.icons.filled.SwapHoriz
import androidx.compose.material.icons.filled.Terminal
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.Typography
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

internal const val SettingsGroupCornerRadiusDp = 24
internal const val SettingsRowMinHeightDp = 72
internal const val SettingsIconContainerSizeDp = 40
internal const val SettingsHorizontalPaddingDp = 20

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

    val colorScheme = MaterialTheme.colorScheme
    val systemTypography = settingsSystemTypography()

    MaterialTheme(
        colorScheme = colorScheme,
        typography = systemTypography,
    ) {
        SettingsScreenContent(
            username = username,
            loggedIn = loggedIn,
            wallInactivityEnabled = wallInactivityEnabled,
            wallInactivityLabel = wallInactivityLabel,
            wallInactivityAvailable = wallInactivityAvailable,
            onToggleWallInactivity = onToggleWallInactivity,
            headlessResizeAvailable = headlessResizeAvailable,
            headlessResizeEnabled = headlessResizeEnabled,
            onResizeHeadlessNow = onResizeHeadlessNow,
            backgroundWallEnabled = backgroundWallEnabled,
            onToggleBackgroundWall = onToggleBackgroundWall,
            followOnReadEnabled = followOnReadEnabled,
            onToggleFollowOnRead = onToggleFollowOnRead,
            onShowEndpoint = onShowEndpoint,
            onShowShareToken = onShowShareToken,
            onShowCertificates = onShowCertificates,
            onShowTheme = onShowTheme,
            onShowAppLock = onShowAppLock,
            onResetZoomPan = onResetZoomPan,
            onLogout = onLogout,
            onBack = onBack,
            modifier = modifier,
        )
    }
}

internal fun settingsSystemTypography(): Typography = Typography()

@Composable
private fun SettingsScreenContent(
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
                    .padding(horizontal = 12.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                IconButton(
                    onClick = onBack,
                    modifier = Modifier
                        .size(48.dp)
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
                        style = MaterialTheme.typography.headlineSmall.copy(fontWeight = FontWeight.SemiBold),
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
                    start = SettingsHorizontalPaddingDp.dp,
                    end = SettingsHorizontalPaddingDp.dp,
                    top = 4.dp,
                    bottom = 32.dp,
                ),
                verticalArrangement = Arrangement.spacedBy(18.dp),
            ) {
                item {
                    SettingsSectionTitle("Connection")
                    SettingsGroup {
                        SettingsActionRow(
                            title = "Endpoint",
                            summary = "Relay server and API endpoint",
                            icon = Icons.Filled.Link,
                            onClick = onShowEndpoint,
                            modifier = Modifier.testTag(TestTags.EndpointButton),
                        )
                        SettingsInsetDivider()
                        SettingsActionRow(
                            title = "Attach via token",
                            summary = "Join a shared Lingon session",
                            icon = Icons.Filled.SwapHoriz,
                            onClick = onShowShareToken,
                            modifier = Modifier.testTag(TestTags.ShareTokenButton),
                        )
                        SettingsInsetDivider()
                        SettingsActionRow(
                            title = "Certificates",
                            summary = "Manage trusted relay certificates",
                            icon = Icons.Filled.Security,
                            onClick = onShowCertificates,
                            modifier = Modifier.testTag(TestTags.CertificatesButton),
                        )
                    }
                }

                item {
                    SettingsSectionTitle("Terminal")
                    SettingsGroup {
                        SettingsSwitchRow(
                            title = "Follow on read",
                            summary = if (followOnReadEnabled) {
                                "Passive output may move the camera"
                            } else {
                                "Only keyboard input follows the cursor"
                            },
                            icon = Icons.Filled.Terminal,
                            checked = followOnReadEnabled,
                            onCheckedChange = onToggleFollowOnRead,
                            modifier = Modifier.testTag(TestTags.FollowOnReadMenuItem),
                            switchModifier = Modifier.testTag(TestTags.FollowOnReadToggle),
                        )
                        SettingsInsetDivider()
                        SettingsActionRow(
                            title = "Reset zoom and pan",
                            summary = "Return the terminal camera to default",
                            icon = Icons.Filled.ResetTv,
                            onClick = onResetZoomPan,
                            modifier = Modifier.testTag(TestTags.ZoomResetButton),
                        )
                        if (headlessResizeAvailable) {
                            SettingsInsetDivider()
                            SettingsActionRow(
                                title = "Resize headless session",
                                summary = if (headlessResizeEnabled) {
                                    "Resize the remote terminal to this view"
                                } else {
                                    "Available for headless sessions only"
                                },
                                icon = Icons.Filled.Terminal,
                                onClick = onResizeHeadlessNow,
                                modifier = Modifier.testTag(TestTags.HeadlessResizeButton),
                                enabled = headlessResizeEnabled,
                            )
                        }
                    }
                }

                item {
                    SettingsSectionTitle("Notifications")
                    SettingsGroup {
                        SettingsSwitchRow(
                            title = "Background wall notifications",
                            summary = if (backgroundWallEnabled) {
                                "Notify when wall messages arrive in background"
                            } else {
                                "Only show wall messages while Lingon is open"
                            },
                            icon = Icons.Filled.Notifications,
                            checked = backgroundWallEnabled,
                            onCheckedChange = onToggleBackgroundWall,
                            modifier = Modifier.testTag(TestTags.BackgroundWallMenuItem),
                            switchModifier = Modifier.testTag(TestTags.BackgroundWallToggle),
                        )
                        if (wallInactivityAvailable) {
                            SettingsInsetDivider()
                            val label = wallInactivityLabel?.takeIf { it.isNotBlank() }
                            SettingsActionRow(
                                title = "Wall inactivity",
                                summary = if (wallInactivityEnabled) {
                                    label ?: "On"
                                } else {
                                    "Off"
                                },
                                icon = Icons.Filled.Campaign,
                                onClick = onToggleWallInactivity,
                                modifier = Modifier.testTag(TestTags.WallInactivityButton),
                            )
                        }
                    }
                }

                item {
                    SettingsSectionTitle("Appearance")
                    SettingsGroup {
                        SettingsActionRow(
                            title = "Theme",
                            summary = "Choose the Lingon color palette",
                            icon = Icons.Filled.ColorLens,
                            onClick = onShowTheme,
                            modifier = Modifier.testTag(TestTags.ThemeButton),
                        )
                    }
                }

                item {
                    SettingsSectionTitle("Security")
                    SettingsGroup {
                        SettingsActionRow(
                            title = "App lock timeout",
                            summary = "Require unlock after background idle time",
                            icon = Icons.Filled.Lock,
                            onClick = onShowAppLock,
                            modifier = Modifier.testTag(TestTags.AppLockTimeoutButton),
                        )
                        if (loggedIn) {
                            SettingsInsetDivider()
                            SettingsActionRow(
                                title = "Logout",
                                summary = "Sign out of ${username?.takeIf { it.isNotBlank() } ?: "Lingon"}",
                                icon = Icons.AutoMirrored.Filled.Logout,
                                onClick = onLogout,
                                modifier = Modifier.testTag(TestTags.LogoutButton),
                            )
                        } else {
                            SettingsInsetDivider()
                            SettingsActionRow(
                                title = "Account",
                                summary = "Not signed in",
                                icon = Icons.Filled.AccountCircle,
                                onClick = {},
                                enabled = false,
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SettingsGroup(content: @Composable ColumnScope.() -> Unit) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(SettingsGroupCornerRadiusDp.dp),
        color = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.36f),
        contentColor = MaterialTheme.colorScheme.onSurface,
    ) {
        Column(content = content)
    }
}

@Composable
private fun SettingsSectionTitle(title: String) {
    Text(
        text = title,
        style = MaterialTheme.typography.labelLarge.copy(fontWeight = FontWeight.SemiBold),
        color = MaterialTheme.colorScheme.primary,
        modifier = Modifier.padding(start = 4.dp, bottom = 8.dp),
    )
}

@Composable
private fun SettingsIconContainer(
    icon: ImageVector,
    enabled: Boolean,
) {
    val alpha = if (enabled) 1f else 0.38f
    Box(
        modifier = Modifier
            .size(SettingsIconContainerSizeDp.dp)
            .background(
                color = MaterialTheme.colorScheme.primary.copy(alpha = 0.12f * alpha),
                shape = RoundedCornerShape(14.dp),
            ),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.primary.copy(alpha = alpha),
            modifier = Modifier.size(22.dp),
        )
    }
}

@Composable
private fun SettingsInsetDivider() {
    HorizontalDivider(
        modifier = Modifier.padding(start = 72.dp),
        color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.55f),
    )
}

@Composable
private fun SettingsActionRow(
    title: String,
    summary: String,
    icon: ImageVector,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    Surface(
        modifier = modifier
            .fillMaxWidth()
            .clickable(enabled = enabled, onClick = onClick),
        color = Color.Transparent,
        contentColor = MaterialTheme.colorScheme.onSurface.copy(alpha = if (enabled) 1f else 0.38f),
    ) {
        Row(
            modifier = Modifier
                .defaultMinSize(minHeight = SettingsRowMinHeightDp.dp)
                .padding(horizontal = 16.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            SettingsIconContainer(icon = icon, enabled = enabled)
            Spacer(modifier = Modifier.width(16.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = title,
                    style = MaterialTheme.typography.bodyLarge,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = summary,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = if (enabled) 1f else 0.6f),
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Icon(
                imageVector = Icons.Filled.ChevronRight,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = if (enabled) 1f else 0.38f),
            )
        }
    }
}

@Composable
private fun SettingsSwitchRow(
    title: String,
    summary: String,
    icon: ImageVector,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
    switchModifier: Modifier = Modifier,
) {
    Surface(
        modifier = modifier
            .fillMaxWidth()
            .clickable { onCheckedChange(!checked) },
        color = Color.Transparent,
        contentColor = MaterialTheme.colorScheme.onSurface,
    ) {
        Row(
            modifier = Modifier
                .defaultMinSize(minHeight = SettingsRowMinHeightDp.dp)
                .padding(horizontal = 16.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            SettingsIconContainer(icon = icon, enabled = true)
            Spacer(modifier = Modifier.width(16.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = title,
                    style = MaterialTheme.typography.bodyLarge,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    text = summary,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Switch(
                checked = checked,
                onCheckedChange = onCheckedChange,
                modifier = switchModifier,
            )
        }
    }
}
