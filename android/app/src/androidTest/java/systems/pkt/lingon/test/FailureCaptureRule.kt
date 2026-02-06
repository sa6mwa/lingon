package systems.pkt.lingon.test

import android.graphics.Bitmap
import android.util.Log
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.junit4.ComposeTestRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.printToLog
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.io.FileOutputStream
import org.junit.rules.TestWatcher
import org.junit.runner.Description
import systems.pkt.lingon.ui.TestTags

class FailureCaptureRule(
    private val composeRule: ComposeTestRule,
) : TestWatcher() {
    override fun failed(e: Throwable?, description: Description) {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val baseDir = context.getExternalFilesDir(null) ?: context.filesDir
        val outDir = File(baseDir, "test-artifacts").apply { mkdirs() }
        val name = description.methodName.replace(Regex("[^a-zA-Z0-9._-]"), "_")

        runCatching {
            composeRule.waitForIdle()
        }

        runCatching {
            val bitmap = InstrumentationRegistry.getInstrumentation().uiAutomation.takeScreenshot()
            if (bitmap != null) {
                writePng(File(outDir, "${name}.png"), bitmap)
            }
        }

        runCatching {
            val node = composeRule.onAllNodesWithTag(
                TestTags.TerminalDebug,
                useUnmergedTree = true,
            ).fetchSemanticsNodes().firstOrNull()
            val desc = node?.config
                ?.getOrElse(SemanticsProperties.ContentDescription) { emptyList() }
                ?.joinToString(separator = " ")
                .orEmpty()
            if (desc.isNotBlank()) {
                File(outDir, "${name}.debug.txt").writeText(desc)
                Log.e("lingon-test", "terminal_debug: $desc")
            }
        }

        runCatching {
            val node = composeRule.onAllNodesWithTag(TestTags.StatusBanner).fetchSemanticsNodes().firstOrNull()
            val desc = node?.config
                ?.getOrElse(SemanticsProperties.ContentDescription) { emptyList() }
                ?.joinToString(separator = " ")
                .orEmpty()
            if (desc.isNotBlank()) {
                File(outDir, "${name}.status.txt").writeText(desc)
                Log.e("lingon-test", "status_banner: $desc")
            }
        }

        runCatching {
            val node = composeRule.onAllNodesWithTag(TestTags.LoginError).fetchSemanticsNodes().firstOrNull()
            val text = node?.config
                ?.getOrElse(SemanticsProperties.Text) { emptyList() }
                ?.joinToString(separator = " ") { it.text }
                .orEmpty()
            if (text.isNotBlank()) {
                File(outDir, "${name}.login-error.txt").writeText(text)
                Log.e("lingon-test", "login_error: $text")
            }
        }

        runCatching {
            composeRule.onRoot().printToLog("lingon-test")
        }
    }

    private fun writePng(file: File, bitmap: Bitmap) {
        FileOutputStream(file).use { out ->
            bitmap.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }
}
