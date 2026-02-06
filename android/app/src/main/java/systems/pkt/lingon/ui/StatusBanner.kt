package systems.pkt.lingon.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import systems.pkt.lingon.ui.theme.LocalLingonExtraColors
import systems.pkt.lingon.viewmodel.StatusLevel
import systems.pkt.lingon.viewmodel.StatusMessage
import systems.pkt.lingon.ui.TestTags
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close

@Composable
fun StatusBanner(
    status: StatusMessage?,
    onDismiss: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    if (status == null || status.message.isBlank()) return
    val extras = LocalLingonExtraColors.current
    val (border, textColor) = when (status.level) {
        StatusLevel.Error -> MaterialTheme.colorScheme.error to MaterialTheme.colorScheme.error
        StatusLevel.Warn -> MaterialTheme.colorScheme.tertiary to MaterialTheme.colorScheme.tertiary
        StatusLevel.Info -> extras.border to extras.muted
    }

    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = RoundedCornerShape(8.dp),
        color = extras.inputBg,
        border = BorderStroke(1.dp, border.copy(alpha = 0.6f)),
    ) {
        Row(
            modifier = Modifier
                .padding(horizontal = 10.dp, vertical = 6.dp)
                .semantics { contentDescription = "level=${status.level} message=${status.message}" }
                .testTag(TestTags.StatusBanner),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(
                text = status.message,
                color = textColor,
                style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.weight(1f, fill = false),
            )
            if (onDismiss != null) {
                IconButton(
                    onClick = onDismiss,
                    modifier = Modifier.testTag(TestTags.StatusDismiss),
                ) {
                    Icon(
                        imageVector = Icons.Filled.Close,
                        contentDescription = "Dismiss",
                        tint = textColor,
                    )
                }
            }
        }
    }
}
