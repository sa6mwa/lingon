package systems.pkt.lingon.ui.dialogs

import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import systems.pkt.lingon.DefaultTerminalZoom
import systems.pkt.lingon.MaxTerminalZoom
import systems.pkt.lingon.MinTerminalZoom
import systems.pkt.lingon.ui.TestTags

@Composable
fun ZoomDialog(
    zoomFactor: Float,
    onDismiss: () -> Unit,
    onSave: (Float) -> Unit,
) {
    val minZoom = MinTerminalZoom
    val maxZoom = MaxTerminalZoom
    val initial = zoomFactor.coerceIn(minZoom, maxZoom)
    var value by rememberSaveable { mutableStateOf(initial) }
    val normalized = value.coerceIn(minZoom, maxZoom)
    val label = if (kotlin.math.abs(normalized - DefaultTerminalZoom) < 0.001f) {
        "1.0x"
    } else {
        String.format("%.2fx", normalized)
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            TextButton(
                onClick = { onSave(normalized) },
                modifier = Modifier.testTag(TestTags.ZoomSave),
            ) {
                Text(text = "Save")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(text = "Cancel")
            }
        },
        title = { Text(text = "Zoom") },
        text = {
            androidx.compose.foundation.layout.Column {
                Text(
                    text = label,
                    style = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.SemiBold),
                    modifier = Modifier.testTag(TestTags.ZoomValue),
                )
                Slider(
                    value = value,
                    onValueChange = { next -> value = next.coerceIn(minZoom, maxZoom) },
                    valueRange = minZoom..maxZoom,
                    steps = 0,
                    modifier = Modifier.testTag(TestTags.ZoomSlider),
                )
            }
        },
    )
}
