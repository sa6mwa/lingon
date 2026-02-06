package systems.pkt.lingon.ui.dialogs

import androidx.compose.foundation.layout.Column
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import systems.pkt.lingon.ui.TestTags

@Composable
fun ShareTokenDialog(
    token: String?,
    errorMessage: String?,
    onDismiss: () -> Unit,
    onAttach: (String) -> Unit,
    onError: (String?) -> Unit,
) {
    var value by rememberSaveable { mutableStateOf(token.orEmpty()) }

    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            TextButton(
                onClick = {
                    val trimmed = value.trim()
                    if (trimmed.isEmpty()) {
                        onError("share token is required")
                        return@TextButton
                    }
                    onAttach(trimmed)
                },
                modifier = Modifier.testTag(TestTags.ShareTokenAttach),
            ) {
                Text(text = "Attach")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(text = "Cancel")
            }
        },
        title = { Text(text = "Attach via token") },
        text = {
            Column {
                OutlinedTextField(
                    value = value,
                    onValueChange = {
                        value = it
                        onError(null)
                    },
                    label = { Text("Share token") },
                    singleLine = false,
                    modifier = Modifier.testTag(TestTags.ShareTokenInput),
                )
                if (!errorMessage.isNullOrBlank()) {
                    Text(
                        text = errorMessage,
                        color = androidx.compose.material3.MaterialTheme.colorScheme.error,
                        modifier = Modifier.testTag(TestTags.ShareTokenError),
                    )
                }
            }
        },
    )
}
