package systems.pkt.lingon.ui.dialogs

import androidx.compose.material3.AlertDialog
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import systems.pkt.lingon.ui.TestTags

internal fun isValidSessionName(name: String): Boolean {
    val trimmed = name.trim()
    return trimmed.isNotEmpty() && trimmed.codePointCount(0, trimmed.length) <= 80 &&
        trimmed.codePoints().noneMatch { Character.isISOControl(it) || Character.getType(it) == Character.FORMAT.toInt() }
}

@Composable
fun RenameSessionDialog(currentName: String, onDismiss: () -> Unit, onSave: (String) -> Unit) {
    var name by remember { mutableStateOf(currentName) }
    val valid = isValidSessionName(name)
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Rename session") },
        text = { OutlinedTextField(value = name, onValueChange = { name = it }, label = { Text("Session name") }, singleLine = true, modifier = Modifier.testTag(TestTags.RenameSessionInput)) },
        confirmButton = { TextButton(onClick = { onSave(name.trim()) }, enabled = valid, modifier = Modifier.testTag(TestTags.RenameSessionSave)) { Text("Save") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}
