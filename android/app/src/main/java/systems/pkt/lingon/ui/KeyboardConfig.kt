package systems.pkt.lingon.ui

import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PlatformImeOptions

internal fun loginPasswordKeyboardOptions(): KeyboardOptions {
    return KeyboardOptions(
        keyboardType = KeyboardType.Password,
        autoCorrectEnabled = false,
        capitalization = KeyboardCapitalization.None,
        imeAction = ImeAction.Done,
    )
}

internal fun terminalInputKeyboardOptions(): KeyboardOptions {
    return KeyboardOptions.Default.copy(
        imeAction = ImeAction.Done,
        capitalization = KeyboardCapitalization.None,
        // Keep text keyboard so IME voice input remains available.
        keyboardType = KeyboardType.Text,
        autoCorrectEnabled = false,
        // Best-effort hints for Gboard to suppress suggestions while keeping the header/mic row.
        platformImeOptions = PlatformImeOptions(
            privateImeOptions = "com.google.android.inputmethod.latin.noSuggestions=true,com.google.android.inputmethod.latin.noPersonalizedLearning=true",
        ),
    )
}
