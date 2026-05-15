package systems.pkt.lingon.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Alarm
import androidx.compose.material.icons.filled.OpenInFull
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

@Composable
fun TopBar(
    title: String,
    onOpenSettings: () -> Unit,
    wallInactivityEnabled: Boolean,
    wallInactivityLabel: String?,
    wallInactivityAvailable: Boolean,
    onToggleWallInactivity: () -> Unit,
    headlessResizeAvailable: Boolean,
    headlessResizeEnabled: Boolean,
    onResizeHeadlessNow: () -> Unit,
    onReload: () -> Unit,
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

    @Composable
    fun SettingsActionButton(compactVertical: Boolean) {
        val buttonSize = if (compactVertical) 28.dp else 40.dp
        IconButton(
            onClick = onOpenSettings,
            modifier = Modifier
                .size(buttonSize)
                .testTag(TestTags.TopBarSettingsButton),
        ) {
            Icon(
                imageVector = Icons.Filled.Settings,
                contentDescription = "Settings",
                tint = MaterialTheme.colorScheme.onSurface,
            )
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
    fun HeadlessResizeActionButton(compactVertical: Boolean) {
        if (!headlessResizeAvailable) {
            return
        }
        val buttonSize = if (compactVertical) 28.dp else 40.dp
        val description = if (headlessResizeEnabled) {
            "Resize headless session"
        } else {
            "Resize unavailable for non-headless session"
        }
        IconButton(
            onClick = onResizeHeadlessNow,
            enabled = headlessResizeEnabled,
            modifier = Modifier
                .size(buttonSize)
                .testTag(TestTags.HeadlessResizeButton),
        ) {
            Icon(
                imageVector = Icons.Filled.OpenInFull,
                contentDescription = description,
                tint = MaterialTheme.colorScheme.onSurface.copy(alpha = if (headlessResizeEnabled) 1f else 0.35f),
            )
        }
    }

    @Composable
    fun TopBarActions(compactVertical: Boolean) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(if (compactVertical) 2.dp else 4.dp),
        ) {
            HeadlessResizeActionButton(compactVertical = compactVertical)
            WallInactivityActionButton(compactVertical = compactVertical)
            ReloadActionButton(compactVertical = compactVertical)
            SettingsActionButton(compactVertical = compactVertical)
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
