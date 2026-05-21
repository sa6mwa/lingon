package systems.pkt.lingon.ui

import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuAnchorType
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import systems.pkt.lingon.data.certs.TrustedCert
import systems.pkt.lingon.ui.theme.LocalLingonExtraColors
import systems.pkt.lingon.viewmodel.AppViewModel
import systems.pkt.lingon.viewmodel.UiState

@Composable
fun ManageCertificatesScreen(state: UiState, viewModel: AppViewModel) {
    BackHandler { viewModel.showCertificates(false) }
    val colorScheme = MaterialTheme.colorScheme
    val systemTypography = settingsSystemTypography()

    MaterialTheme(
        colorScheme = colorScheme,
        typography = systemTypography,
    ) {
        ManageCertificatesScreenContent(state = state, viewModel = viewModel)
    }
}

@Composable
private fun ManageCertificatesScreenContent(state: UiState, viewModel: AppViewModel) {
    val endpoints = state.savedEndpoints
    val selectedEndpoint = state.selectedCertEndpoint
    val context = LocalContext.current
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        val endpoint = selectedEndpoint
        if (uri == null || endpoint.isNullOrBlank()) {
            viewModel.setCertificateError("select an endpoint before adding a certificate")
            return@rememberLauncherForActivityResult
        }
        val content = runCatching {
            context.contentResolver.openInputStream(uri)?.use { stream ->
                stream.readBytes().toString(Charsets.UTF_8)
            }
        }.getOrNull()
        if (content.isNullOrBlank()) {
            viewModel.setCertificateError("failed to read certificate file")
            return@rememberLauncherForActivityResult
        }
        viewModel.addTrustedCertificates(endpoint, content)
    }

    Box(
        modifier = Modifier
            .fillMaxSize()
            .testTag(TestTags.SettingsScreen),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .statusBarsPadding()
                .padding(horizontal = SettingsHorizontalPaddingDp.dp, vertical = 12.dp)
                .testTag(TestTags.SettingsCertificatesScreen),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(
                    text = "Manage certificates",
                    style = MaterialTheme.typography.headlineSmall.copy(fontWeight = FontWeight.SemiBold),
                )
                TextButton(onClick = { viewModel.showCertificates(false) }) {
                    Text("Done")
                }
            }

            EndpointPicker(
                endpoints = endpoints,
                selected = selectedEndpoint,
                onSelected = viewModel::selectCertEndpoint,
            )

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.End,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Button(
                    onClick = {
                        viewModel.setCertificateError(null)
                        launcher.launch(arrayOf(
                            "application/x-pem-file",
                            "application/pem-certificate-chain",
                            "application/x-x509-ca-cert",
                            "application/pkix-cert",
                            "text/plain",
                        ))
                    },
                    enabled = !selectedEndpoint.isNullOrBlank(),
                ) {
                    Text("Add certificate")
                }
            }

            if (!state.certificateError.isNullOrBlank()) {
                Text(
                    text = state.certificateError.orEmpty(),
                    color = MaterialTheme.colorScheme.error,
                    style = MaterialTheme.typography.bodySmall,
                )
            }

            if (state.trustedCerts.isEmpty()) {
                Text(
                    text = if (selectedEndpoint.isNullOrBlank()) {
                        "Select an endpoint to manage certificates."
                    } else {
                        "No trusted certificates for this endpoint."
                    },
                    style = MaterialTheme.typography.bodyMedium,
                )
            } else {
                LazyColumn(
                    modifier = Modifier.fillMaxWidth(),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    items(state.trustedCerts, key = { it.id }) { cert ->
                        CertificateCard(
                            cert = cert,
                            onRemove = {
                                val endpoint = selectedEndpoint ?: return@CertificateCard
                                viewModel.removeTrustedCertificate(endpoint, cert.id)
                            },
                        )
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun EndpointPicker(
    endpoints: List<String>,
    selected: String?,
    onSelected: (String) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    val current = selected ?: endpoints.firstOrNull().orEmpty()
    ExposedDropdownMenuBox(
        expanded = expanded,
        onExpandedChange = { expanded = it },
    ) {
        OutlinedTextField(
            value = current,
            onValueChange = {},
            readOnly = true,
            label = { Text("Endpoint") },
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = expanded) },
            modifier = Modifier
                .menuAnchor(ExposedDropdownMenuAnchorType.PrimaryNotEditable)
                .fillMaxWidth(),
        )
        ExposedDropdownMenu(
            expanded = expanded,
            onDismissRequest = { expanded = false },
        ) {
            if (endpoints.isEmpty()) {
                DropdownMenuItem(
                    text = { Text("No endpoints saved") },
                    onClick = { expanded = false },
                    enabled = false,
                )
            } else {
                endpoints.forEach { endpoint ->
                    DropdownMenuItem(
                        text = { Text(endpoint, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                        onClick = {
                            expanded = false
                            onSelected(endpoint)
                        },
                    )
                }
            }
        }
    }
}

@Composable
private fun CertificateCard(
    cert: TrustedCert,
    onRemove: () -> Unit,
) {
    val extras = LocalLingonExtraColors.current
    Surface(
        shape = MaterialTheme.shapes.medium,
        border = BorderStroke(1.dp, extras.border),
        color = extras.inputBg,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text(
                text = cert.subject.ifBlank { "(no subject)" },
                style = MaterialTheme.typography.bodyMedium.copy(fontWeight = FontWeight.SemiBold),
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = "Issuer: ${cert.issuer.ifBlank { "(unknown)" }}",
                style = MaterialTheme.typography.bodySmall,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = "Fingerprint: ${formatFingerprint(cert.fingerprint)}",
                style = MaterialTheme.typography.bodySmall,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = "Added: ${cert.addedAt}",
                style = MaterialTheme.typography.bodySmall,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.End,
            ) {
                TextButton(onClick = onRemove) {
                    Text("Remove")
                }
            }
        }
    }
}

private fun formatFingerprint(value: String): String {
    if (value.isBlank()) return ""
    return value.chunked(2).joinToString(":").uppercase()
}
