package systems.pkt.lingon.ui.dialogs

import androidx.compose.foundation.layout.Column
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
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
fun RotateSSHKeyDialog(
    errorMessage: String?,
    busy: Boolean,
    onDismiss: () -> Unit,
    onConfirm: (String) -> Unit,
    onError: (String?) -> Unit,
) {
    var confirmation by rememberSaveable { mutableStateOf("") }

    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            TextButton(
                onClick = { onConfirm(confirmation) },
                enabled = !busy,
                modifier = Modifier.testTag(TestTags.RotateSSHKeyConfirm),
            ) {
                Text(text = "Rotate")
            }
        },
        dismissButton = {
            TextButton(
                onClick = onDismiss,
                enabled = !busy,
                modifier = Modifier.testTag(TestTags.RotateSSHKeyCancel),
            ) {
                Text(text = "Cancel")
            }
        },
        title = { Text(text = "Rotate SSH key") },
        text = {
            Column {
                Text(text = "Type YES to rotate your SSH key.")
                OutlinedTextField(
                    value = confirmation,
                    onValueChange = {
                        confirmation = it
                        onError(null)
                    },
                    label = { Text("Confirmation") },
                    singleLine = true,
                    modifier = Modifier.testTag(TestTags.RotateSSHKeyInput),
                )
                if (!errorMessage.isNullOrBlank()) {
                    Text(
                        text = errorMessage,
                        color = MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.bodyMedium,
                    )
                }
            }
        },
    )
}
