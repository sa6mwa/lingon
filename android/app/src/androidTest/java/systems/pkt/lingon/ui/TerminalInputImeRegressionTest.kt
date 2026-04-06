package systems.pkt.lingon.ui

import android.view.View
import android.view.ViewGroup
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import systems.pkt.lingon.MainActivity
import kotlin.Function1

@RunWith(AndroidJUnit4::class)
class TerminalInputImeRegressionTest {
    @get:Rule
    val composeRule = createAndroidComposeRule<MainActivity>()

    @Test
    fun composingDeleteVariants_doNotEmit_terminal_backspace() {
        composeRule.activityRule.scenario.onActivity { activity ->
            val variants = listOf(
                "deleteSurroundingText" to { connection: InputConnection ->
                    connection.deleteSurroundingText(1, 0)
                },
                "deleteSurroundingTextInCodePoints" to { connection: InputConnection ->
                    connection.deleteSurroundingTextInCodePoints(1, 0)
                },
            )

            variants.forEach { (label, delete) ->
                val recorder = Recorder()
                val view = newTerminalInputView(activity)
                activity.findViewById<ViewGroup>(android.R.id.content).addView(view)
                view.setFocusHooks(recorder)
                view.requestFocus()

                val connection = view.onCreateInputConnection(EditorInfo())
                    ?: throw AssertionError("missing input connection for $label")

                assertTrue("$label should accept composing text", connection.setComposingText("ab", 1))
                assertTrue("$label should accept delete", delete(connection))
                assertTrue("$label should finish composing", connection.finishComposingText())

                assertEquals("$label should keep composing delete out of terminal backspace", 0, recorder.backspaces.size)
                assertEquals("$label should commit the remaining composing text", listOf("a"), recorder.commits)
            }
        }
    }

    @Test
    fun emptyBufferDeleteVariants_forward_terminal_backspace() {
        composeRule.activityRule.scenario.onActivity { activity ->
            val variants = listOf(
                "deleteSurroundingText" to { connection: InputConnection ->
                    connection.deleteSurroundingText(1, 0)
                },
                "deleteSurroundingTextInCodePoints" to { connection: InputConnection ->
                    connection.deleteSurroundingTextInCodePoints(1, 0)
                },
            )

            variants.forEach { (label, delete) ->
                val recorder = Recorder()
                val view = newTerminalInputView(activity)
                activity.findViewById<ViewGroup>(android.R.id.content).addView(view)
                view.setFocusHooks(recorder)
                view.requestFocus()

                val connection = view.onCreateInputConnection(EditorInfo())
                    ?: throw AssertionError("missing input connection for $label")

                delete(connection)
                assertEquals("$label should forward empty-buffer delete as terminal backspace", listOf(1), recorder.backspaces)
                assertEquals("$label should not commit text on plain backspace", emptyList<String>(), recorder.commits)
            }
        }
    }

    private data class Recorder(
        val commits: MutableList<String> = mutableListOf(),
        val backspaces: MutableList<Int> = mutableListOf(),
    )

    private fun newTerminalInputView(activity: MainActivity): View {
        val clazz = Class.forName("systems.pkt.lingon.ui.TerminalInputView")
        val ctor = clazz.getDeclaredConstructor(android.content.Context::class.java)
        ctor.isAccessible = true
        return ctor.newInstance(activity) as View
    }

    private fun View.setFocusHooks(recorder: Recorder) {
        invokeSetter(
            name = "setOnCommitText",
            listener = { text: String ->
                recorder.commits.add(text)
            },
        )
        invokeSetter(
            name = "setOnBackspace",
            listener = { count: Int ->
                recorder.backspaces.add(count)
            },
        )
    }

    private fun View.invokeSetter(name: String, listener: Any) {
        val method = javaClass.getDeclaredMethod(name, Function1::class.java)
        method.isAccessible = true
        method.invoke(this, listener)
    }
}
