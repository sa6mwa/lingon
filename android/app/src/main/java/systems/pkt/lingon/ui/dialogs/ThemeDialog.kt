package systems.pkt.lingon.ui.dialogs

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import systems.pkt.lingon.ui.TestTags

private data class ThemeOption(val value: String, val label: String)

private val themeOptions = listOf(
    ThemeOption("outrun", "Outrun (default)"),
    ThemeOption("gruvbox", "Gruvbox"),
    ThemeOption("tokyo-midnight", "Tokyo Midnight"),
)

@Composable
fun ThemeDialog(
    currentTheme: String?,
    onDismiss: () -> Unit,
    onSave: (String) -> Unit,
) {
    val initial = currentTheme?.ifBlank { "outrun" } ?: "outrun"
    var selected by rememberSaveable { mutableStateOf(initial) }

    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            TextButton(onClick = { onSave(selected) }) {
                Text(text = "Apply")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(text = "Cancel")
            }
        },
        title = { Text(text = "Select theme") },
        text = {
            Column(modifier = Modifier.testTag(TestTags.ThemeDialog)) {
                themeOptions.forEach { option ->
                    val isSelected = option.value == selected
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable { selected = option.value }
                            .padding(vertical = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        RadioButton(
                            selected = isSelected,
                            onClick = { selected = option.value },
                        )
                        Spacer(modifier = Modifier.width(8.dp))
                        Text(
                            text = option.label,
                            style = if (isSelected) {
                                MaterialTheme.typography.bodyMedium.copy(fontWeight = FontWeight.SemiBold)
                            } else {
                                MaterialTheme.typography.bodyMedium
                            },
                        )
                    }
                }
                Spacer(modifier = Modifier.height(4.dp))
            }
        },
    )
}
