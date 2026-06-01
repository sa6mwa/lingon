package systems.pkt.lingon.ui

import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import android.os.Bundle
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import systems.pkt.lingon.MainActivity

@RunWith(AndroidJUnit4::class)
class TerminalLinkOpenerInstrumentedTest {
    @Test
    fun createIntentUsesAndroidViewResolutionForHttpsLinks() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext

        val intent = TerminalLinkOpener.createIntent(context, "https://example.test/path?q=1")

        assertEquals(Intent.ACTION_VIEW, intent.action)
        assertEquals("https://example.test/path?q=1", intent.dataString)
        assertTrue(intent.categories.orEmpty().contains(Intent.CATEGORY_BROWSABLE))
        assertTrue(intent.flags and Intent.FLAG_ACTIVITY_NEW_TASK != 0)
    }

    @Test
    fun createIntentDoesNotAddNewTaskForActivityContext() {
        val scenario = androidx.test.core.app.ActivityScenario.launch(MainActivity::class.java)
        try {
            scenario.onActivity { activity ->
                val intent = TerminalLinkOpener.createIntent(activity, "https://example.test")

                assertFalse(intent.flags and Intent.FLAG_ACTIVITY_NEW_TASK != 0)
            }
        } finally {
            scenario.close()
        }
    }

    @Test
    fun openStartsTheSystemViewIntent() {
        val base = InstrumentationRegistry.getInstrumentation().targetContext
        val context = RecordingContext(base)

        val opened = TerminalLinkOpener.open(context, "https://example.test/path")

        assertTrue(opened)
        val intent = context.startedIntent
        assertNotNull(intent)
        assertEquals(Intent.ACTION_VIEW, intent?.action)
        assertEquals("https://example.test/path", intent?.dataString)
        assertTrue(intent?.categories.orEmpty().contains(Intent.CATEGORY_BROWSABLE))
    }

    private class RecordingContext(base: Context) : ContextWrapper(base) {
        var startedIntent: Intent? = null

        override fun startActivity(intent: Intent) {
            startedIntent = intent
        }

        override fun startActivity(intent: Intent, options: Bundle?) {
            startedIntent = intent
        }
    }
}
