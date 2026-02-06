package systems.pkt.lingon.ui.dialogs

import android.content.Context
import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext
import java.io.IOException

@Composable
fun CodexAuthDialog(
    errorMessage: String?,
    busy: Boolean,
    onDismiss: () -> Unit,
    onUpload: (String) -> Unit,
    onError: (String?) -> Unit,
) {
    val context = LocalContext.current
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        onError(null)
        val name = queryDisplayName(context, uri)
        if (!name.isNullOrBlank() && !name.lowercase().endsWith(".json")) {
            onError("auth.json must be a .json file")
            return@rememberLauncherForActivityResult
        }
        val text = try {
            readText(context, uri)
        } catch (err: IOException) {
            onError(err.message ?: "failed to read auth.json")
            return@rememberLauncherForActivityResult
        }
        if (text.trim().isEmpty()) {
            onError("auth.json file is empty")
            return@rememberLauncherForActivityResult
        }
        onUpload(text)
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            TextButton(onClick = { launcher.launch(arrayOf("application/json")) }, enabled = !busy) {
                Text(text = "Choose file")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss, enabled = !busy) {
                Text(text = "Cancel")
            }
        },
        title = { Text(text = "Upload Codex auth.json") },
        text = {
            Column {
                Text(text = "Select the auth.json file to upload.")
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

private fun queryDisplayName(context: Context, uri: Uri): String? {
    val cursor = context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)
    cursor?.use {
        if (it.moveToFirst()) {
            val index = it.getColumnIndex(OpenableColumns.DISPLAY_NAME)
            if (index >= 0) {
                return it.getString(index)
            }
        }
    }
    return null
}

@Throws(IOException::class)
private fun readText(context: Context, uri: Uri): String {
    context.contentResolver.openInputStream(uri).use { stream ->
        if (stream == null) throw IOException("unable to open auth.json")
        return stream.bufferedReader().use { it.readText() }
    }
}
