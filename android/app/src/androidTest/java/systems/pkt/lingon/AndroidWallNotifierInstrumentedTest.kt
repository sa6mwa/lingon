package systems.pkt.lingon

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.os.Build
import android.os.ParcelFileDescriptor
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.io.FileInputStream
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import systems.pkt.lingon.notifications.AndroidWallNotifier
import systems.pkt.lingon.notifications.isWallNotificationStatusBarEntry
import systems.pkt.lingon.notifications.wallNotificationId
import systems.pkt.lingon.notifications.wallNotificationTag
import systems.pkt.lingon.viewmodel.WallNotification

@RunWith(AndroidJUnit4::class)
class AndroidWallNotifierInstrumentedTest {
    private val context = InstrumentationRegistry.getInstrumentation().targetContext
    private val notificationManager: NotificationManager =
        context.getSystemService(NotificationManager::class.java)

    @Before
    fun setUp() {
        ensureNotificationDeliveryEnabled()
        resetWallChannel()
    }

    @After
    fun tearDown() {
        notificationManager.cancelAll()
        resetWallChannel()
        ensureNotificationDeliveryEnabled()
    }

    @Test
    fun disabledWallChannelDoesNotReportDelivered() {
        notificationManager.createNotificationChannel(
            NotificationChannel(
                disabledTestChannelId,
                "Lingon Broadcasts",
                NotificationManager.IMPORTANCE_NONE,
            ),
        )

        assertEquals(
            NotificationManager.IMPORTANCE_NONE,
            notificationManager.getNotificationChannel(disabledTestChannelId).importance,
        )
        assertFalse(
            AndroidWallNotifier(context, disabledTestChannelId)
                .notifyWall(wallNotification(message = "blocked by channel")),
        )
        assertFalse(
            notificationManager.activeNotifications.any {
                it.notification.channelId == disabledTestChannelId
            },
        )
    }

    @Test
    fun visibleWallPostReportsDeliveredAfterExpectedNotificationIsActive() {
        val notification = wallNotification(message = "visible wall")
        val tag = wallNotificationTag(notification)
        val id = wallNotificationId(notification)

        assertTrue(AndroidWallNotifier(context, visibleTestChannelId).notifyWall(notification))
        assertTrue(
            notificationManager.activeNotifications.any {
                isWallNotificationStatusBarEntry(
                    channelId = it.notification.channelId,
                    tag = it.tag,
                    id = it.id,
                    expectedChannelId = visibleTestChannelId,
                    expectedTag = tag,
                    expectedId = id,
                )
            },
        )
    }

    private fun resetWallChannel() {
        notificationManager.cancelAll()
        notificationManager.deleteNotificationChannel(disabledTestChannelId)
        notificationManager.deleteNotificationChannel(visibleTestChannelId)
    }

    private fun ensureNotificationDeliveryEnabled() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            consumeShellCommand(
                instrumentation.uiAutomation.executeShellCommand(
                    "pm grant ${context.packageName} ${Manifest.permission.POST_NOTIFICATIONS}",
                ),
            )
        }
        consumeShellCommand(
            instrumentation.uiAutomation.executeShellCommand(
                "cmd appops set --uid ${context.packageName} POST_NOTIFICATION allow",
            ),
        )
    }

    private fun consumeShellCommand(descriptor: ParcelFileDescriptor) {
        FileInputStream(descriptor.fileDescriptor).use { input ->
            val buffer = ByteArray(DEFAULT_BUFFER_SIZE)
            while (input.read(buffer) >= 0) {
                // Drain the shell command output so the command completes.
            }
        }
        descriptor.close()
    }

    private fun wallNotification(message: String): WallNotification {
        return WallNotification(
            endpoint = "https://relay.example/v1",
            eventId = System.nanoTime(),
            sender = "alice@10.0.0.1",
            sourceSessionName = "build-host",
            message = message,
        )
    }

    private companion object {
        const val disabledTestChannelId = "lingon_wall_instrumented_disabled"
        const val visibleTestChannelId = "lingon_wall_instrumented_visible"
    }
}
