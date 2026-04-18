package systems.pkt.lingon.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.wrapContentSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Alarm
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex

@Composable
fun TopBar(
    title: String,
    username: String?,
    loggedIn: Boolean,
    menuExpanded: Boolean,
    onToggleMenu: () -> Unit,
    onDismissMenu: () -> Unit,
    onShowSettings: () -> Unit,
    onShowTheme: () -> Unit,
    onShowAppLock: () -> Unit,
    onResetZoomPan: () -> Unit,
    wallInactivityEnabled: Boolean,
    wallInactivityLabel: String?,
    wallInactivityAvailable: Boolean,
    onToggleWallInactivity: () -> Unit,
    onReload: () -> Unit,
    onShowShareToken: () -> Unit,
    onShowCertificates: () -> Unit,
    backgroundWallEnabled: Boolean,
    onToggleBackgroundWall: (Boolean) -> Unit,
    onLogout: () -> Unit,
    compact: Boolean,
    vertical: Boolean,
    modifier: Modifier = Modifier,
) {
    val horizontalPadding = if (compact) 8.dp else 12.dp
    val verticalPadding = if (compact) 4.dp else 6.dp
    val titleStyle = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.SemiBold)
    val verticalTitleStyle = if (compact) {
        MaterialTheme.typography.labelMedium.copy(fontWeight = FontWeight.SemiBold)
    } else {
        MaterialTheme.typography.labelLarge.copy(fontWeight = FontWeight.SemiBold)
    }
    val menuMaxHeight = (LocalConfiguration.current.screenHeightDp * 0.7f).dp

    @Composable
    fun MenuActionButton(compactVertical: Boolean) {
        val buttonSize = if (compactVertical) 28.dp else 40.dp
        Box(
            modifier = Modifier.wrapContentSize(Alignment.TopEnd),
            contentAlignment = Alignment.TopEnd,
        ) {
            IconButton(
                onClick = {
                    if (menuExpanded) {
                        onDismissMenu()
                    } else {
                        onToggleMenu()
                    }
                },
                modifier = Modifier
                    .size(buttonSize)
                    .zIndex(2f)
                    .semantics { stateDescription = if (menuExpanded) "open" else "closed" }
                    .testTag(TestTags.TopBarMenuButton),
            ) {
                Icon(
                    imageVector = if (menuExpanded) Icons.Filled.Close else Icons.Filled.Menu,
                    contentDescription = if (menuExpanded) "Close menu" else "Menu",
                    tint = MaterialTheme.colorScheme.onSurface,
                )
            }
            DropdownMenu(
                expanded = menuExpanded,
                onDismissRequest = onDismissMenu,
                modifier = Modifier
                    .testTag(TestTags.TopBarMenu),
            ) {
                Column(
                    modifier = Modifier
                        .heightIn(max = menuMaxHeight)
                        .verticalScroll(rememberScrollState()),
                ) {
                    if (!username.isNullOrBlank()) {
                        DropdownMenuItem(
                            text = { Text("Signed in as $username") },
                            onClick = {},
                            enabled = false,
                        )
                    }
                    DropdownMenuItem(
                        text = { Text("Endpoint") },
                        onClick = {
                            onShowSettings()
                            onDismissMenu()
                        },
                        modifier = Modifier.testTag(TestTags.EndpointButton),
                    )
                    DropdownMenuItem(
                        text = { Text("Attach via token") },
                        onClick = {
                            onShowShareToken()
                            onDismissMenu()
                        },
                        modifier = Modifier.testTag(TestTags.ShareTokenButton),
                    )
                    DropdownMenuItem(
                        text = { Text("Manage certificates") },
                        onClick = {
                            onShowCertificates()
                            onDismissMenu()
                        },
                        modifier = Modifier.testTag(TestTags.CertificatesButton),
                    )
                    DropdownMenuItem(
                        text = { Text("Background wall notifications") },
                        onClick = {
                            onToggleBackgroundWall(!backgroundWallEnabled)
                            onDismissMenu()
                        },
                        trailingIcon = {
                            Switch(
                                checked = backgroundWallEnabled,
                                onCheckedChange = { checked ->
                                    onToggleBackgroundWall(checked)
                                    onDismissMenu()
                                },
                                modifier = Modifier.testTag(TestTags.BackgroundWallToggle),
                            )
                        },
                        modifier = Modifier.testTag(TestTags.BackgroundWallMenuItem),
                    )
                    DropdownMenuItem(
                        text = { Text("Select theme") },
                        onClick = {
                            onShowTheme()
                            onDismissMenu()
                        },
                        modifier = Modifier.testTag(TestTags.ThemeButton),
                    )
                    DropdownMenuItem(
                        text = { Text("App lock timeout") },
                        onClick = {
                            onShowAppLock()
                            onDismissMenu()
                        },
                        modifier = Modifier.testTag(TestTags.AppLockTimeoutButton),
                    )
                    DropdownMenuItem(
                        text = { Text("Reset zoom/pan") },
                        onClick = {
                            onResetZoomPan()
                            onDismissMenu()
                        },
                        modifier = Modifier.testTag(TestTags.ZoomResetButton),
                    )
                    if (loggedIn) {
                        DropdownMenuItem(
                            text = { Text("Logout") },
                            onClick = {
                                onLogout()
                                onDismissMenu()
                            },
                            modifier = Modifier.testTag(TestTags.LogoutButton),
                        )
                    }
                }
            }
        }
    }

    @Composable
    fun ReloadActionButton(compactVertical: Boolean) {
        val buttonSize = if (compactVertical) 28.dp else 40.dp
        IconButton(
            onClick = onReload,
            modifier = Modifier
                .size(buttonSize)
                .testTag(TestTags.ReloadButton),
        ) {
            Icon(
                imageVector = Icons.Filled.Refresh,
                contentDescription = "Reload",
                tint = MaterialTheme.colorScheme.onSurface,
            )
        }
    }

    @Composable
    fun WallInactivityActionButton(compactVertical: Boolean) {
        if (!wallInactivityAvailable) {
            return
        }
        val buttonSize = if (compactVertical) 28.dp else 40.dp
        val label = wallInactivityLabel?.takeIf { it.isNotBlank() }
        val description = if (wallInactivityEnabled) {
            "Wall inactivity ${label ?: "on"}"
        } else {
            "Wall inactivity off"
        }
        IconButton(
            onClick = onToggleWallInactivity,
            modifier = Modifier
                .size(buttonSize)
                .testTag(TestTags.WallInactivityButton),
        ) {
            Icon(
                imageVector = Icons.Filled.Alarm,
                contentDescription = description,
                tint = MaterialTheme.colorScheme.onSurface.copy(alpha = if (wallInactivityEnabled) 1f else 0.55f),
            )
        }
    }

    @Composable
    fun TopBarActions(compactVertical: Boolean) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(if (compactVertical) 2.dp else 4.dp),
        ) {
            WallInactivityActionButton(compactVertical = compactVertical)
            ReloadActionButton(compactVertical = compactVertical)
            MenuActionButton(compactVertical = compactVertical)
        }
    }

    if (vertical) {
        Column(
            modifier = modifier
                .statusBarsPadding()
                .padding(horizontal = horizontalPadding, vertical = verticalPadding),
            verticalArrangement = Arrangement.spacedBy(6.dp),
            horizontalAlignment = Alignment.Start,
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                TopBarActions(true)
                Text(
                    text = title,
                    style = verticalTitleStyle,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.testTag(TestTags.TopBarTitle),
                )
            }
        }
    } else {
        Row(
            modifier = modifier
                .fillMaxWidth()
                .statusBarsPadding()
                .padding(horizontal = horizontalPadding, vertical = verticalPadding),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = title,
                style = titleStyle,
                modifier = Modifier.testTag(TestTags.TopBarTitle),
            )
            TopBarActions(false)
        }
    }
}
