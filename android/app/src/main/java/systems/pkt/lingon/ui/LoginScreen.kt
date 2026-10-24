package systems.pkt.lingon.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.sizeIn
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.Modifier
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import systems.pkt.lingon.BuildConfig
import systems.pkt.lingon.R
import systems.pkt.lingon.viewmodel.AppViewModel
import systems.pkt.lingon.viewmodel.UiState

@Composable
fun LoginScreen(state: UiState, viewModel: AppViewModel) {
    var username by rememberSaveable { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var totp by remember { mutableStateOf("") }
    val config = LocalConfiguration.current
    val isCompact = config.screenWidthDp < 360 || config.screenHeightDp < 700
    val outerPadding = if (isCompact) 12.dp else 16.dp
    val innerPadding = if (isCompact) 12.dp else 16.dp
    val spacing = if (isCompact) 8.dp else 12.dp

    val logoMaxWidth = if (isCompact) 110.dp else 140.dp
    val logoMaxHeight = if (isCompact) 60.dp else 80.dp
    val logoBottomPadding = if (isCompact) 4.dp else 8.dp
    val topOffset = if (isCompact) 12.dp else 24.dp

    Box(
        modifier = Modifier.fillMaxSize().imePadding(),
        contentAlignment = Alignment.TopCenter,
    ) {
        Surface(
            modifier = Modifier
                .padding(horizontal = outerPadding, vertical = topOffset)
                .fillMaxWidth(),
            shape = RoundedCornerShape(12.dp),
            color = MaterialTheme.colorScheme.surface,
            border = BorderStroke(1.dp, MaterialTheme.colorScheme.onSurface.copy(alpha = 0.2f)),
        ) {
            Column(
                modifier = Modifier
                    .verticalScroll(rememberScrollState())
                    .padding(innerPadding),
                verticalArrangement = Arrangement.spacedBy(spacing),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Image(
                    painter = painterResource(id = R.drawable.lingon_logo),
                    contentDescription = "Lingon",
                    contentScale = ContentScale.Fit,
                    modifier = Modifier
                        .padding(bottom = logoBottomPadding)
                        .sizeIn(maxWidth = logoMaxWidth, maxHeight = logoMaxHeight),
                )
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = "Login",
                        style = if (isCompact) MaterialTheme.typography.titleSmall else MaterialTheme.typography.titleMedium,
                    )
                    Spacer(modifier = Modifier.weight(1f))
                    Text(
                        text = BuildConfig.VERSION_NAME,
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurface.copy(alpha = 0.7f),
                    )
                }
                OutlinedTextField(
                    value = username,
                    onValueChange = { username = it },
                    label = { Text("Username") },
                    singleLine = true,
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(TestTags.LoginUsername),
                )
                OutlinedTextField(
                    value = password,
                    onValueChange = { password = it },
                    label = { Text("Password") },
                    singleLine = true,
                    keyboardOptions = loginPasswordKeyboardOptions(),
                    visualTransformation = PasswordVisualTransformation(),
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(TestTags.LoginPassword),
                )
                OutlinedTextField(
                    value = totp,
                    onValueChange = { totp = it },
                    label = { Text("TOTP") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Number),
                    visualTransformation = VisualTransformation.None,
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(TestTags.LoginTotp),
                )
                Button(
                    onClick = { viewModel.login(username.trim(), password, totp.trim()) },
                    modifier = Modifier
                        .align(Alignment.End)
                        .testTag(TestTags.LoginSubmit),
                    enabled = !state.isBusy,
                ) {
                    Text(text = "Sign in")
                }
                if (!state.loginError.isNullOrBlank()) {
                    Text(
                        text = requireNotNull(state.loginError),
                        color = MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.bodyMedium,
                        modifier = Modifier.testTag(TestTags.LoginError),
                    )
                }
            }
        }
    }
}
