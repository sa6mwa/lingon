package systems.pkt.lingon.notifications

import android.Manifest
import android.app.PendingIntent
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.SystemClock
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import java.security.MessageDigest
import systems.pkt.lingon.MainActivity
import systems.pkt.lingon.R
import systems.pkt.lingon.viewmodel.WallNotification
import systems.pkt.lingon.viewmodel.WallNotifier

class AndroidWallNotifier(
    private val context: Context,
    private val channelId: String = wallNotificationChannelId,
) : WallNotifier {
    override fun notifyWall(notification: WallNotification): Boolean {
        val notificationManager = NotificationManagerCompat.from(context)
        if (!notificationManager.areNotificationsEnabled()) {
            return false
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            val granted = context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED
            if (!granted) {
                return false
            }
        }
        ensureChannel()
        if (!isWallChannelPostable()) {
            return false
        }
        val body = notification.message.trim()
        val source = formatWallSource(notification.sender, notification.sourceSessionName)
        val title = source.ifBlank { "Broadcast" }
        val content = formatWallContent(notification.sender, notification.sourceSessionName, body)
        val launchIntent = Intent(context, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
        }
        val pendingFlags = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        } else {
            PendingIntent.FLAG_UPDATE_CURRENT
        }
        val pendingIntent = PendingIntent.getActivity(
            context,
            0,
            launchIntent,
            pendingFlags,
        )
        val androidNotification = NotificationCompat.Builder(context, channelId)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(title)
            .setContentText(content)
            .setStyle(NotificationCompat.BigTextStyle().bigText(content))
            .setAutoCancel(true)
            .setOnlyAlertOnce(true)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setVisibility(NotificationCompat.VISIBILITY_PRIVATE)
            .setContentIntent(pendingIntent)
            .build()
        val notificationTag = wallNotificationTag(notification)
        val notificationId = wallNotificationId(notification)
        return try {
            notificationManager.notify(
                notificationTag,
                notificationId,
                androidNotification,
            )
            isWallNotificationVisible(notificationTag, notificationId)
        } catch (_: SecurityException) {
            false
        }
    }

    private fun isWallChannelPostable(): Boolean {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            return true
        }
        val manager = context.getSystemService(NotificationManager::class.java) ?: return false
        val channel = manager.getNotificationChannel(channelId) ?: return false
        return wallNotificationChannelCanPost(channel.importance)
    }

    private fun isWallNotificationVisible(notificationTag: String, notificationId: Int): Boolean {
        val deadline = SystemClock.uptimeMillis() + wallNotificationVisibilityTimeoutMs
        while (true) {
            if (hasActiveWallNotification(notificationTag, notificationId)) {
                return true
            }
            if (SystemClock.uptimeMillis() >= deadline) {
                return false
            }
            SystemClock.sleep(wallNotificationVisibilityPollIntervalMs)
        }
    }

    private fun hasActiveWallNotification(notificationTag: String, notificationId: Int): Boolean {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return false
        return manager.activeNotifications.any {
            isWallNotificationStatusBarEntry(
                channelId = it.notification.channelId,
                tag = it.tag,
                id = it.id,
                expectedChannelId = channelId,
                expectedTag = notificationTag,
                expectedId = notificationId,
            )
        }
    }

    private fun ensureChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            return
        }
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        val existing = manager.getNotificationChannel(channelId)
        if (existing != null) {
            return
        }
        val channel = NotificationChannel(
            channelId,
            "Lingon Broadcasts",
            NotificationManager.IMPORTANCE_HIGH,
        )
        channel.description = "Broadcast notifications from relay wall messages"
        manager.createNotificationChannel(channel)
    }
}

internal const val wallNotificationChannelId = "lingon_wall"
private const val wallNotificationVisibilityTimeoutMs = 750L
private const val wallNotificationVisibilityPollIntervalMs = 25L
private const val wallNotificationFallbackIdBase = 300_000_000
private const val wallNotificationIdMask = 0x0fffffff

internal fun wallNotificationId(notification: WallNotification): Int {
    val folded = wallNotificationKey(notification).hashCode() and wallNotificationIdMask
    return wallNotificationFallbackIdBase + folded
}

internal fun wallNotificationTag(notification: WallNotification): String {
    return "wall:${sha256Hex(wallNotificationKey(notification))}"
}

internal fun wallNotificationChannelCanPost(importance: Int): Boolean {
    return importance != NotificationManager.IMPORTANCE_NONE
}

internal fun isWallNotificationStatusBarEntry(
    channelId: String?,
    tag: String?,
    id: Int,
    expectedChannelId: String,
    expectedTag: String,
    expectedId: Int,
): Boolean {
    return channelId == expectedChannelId && tag == expectedTag && id == expectedId
}

private fun wallNotificationKey(notification: WallNotification): String {
    val cleanEndpoint = notification.endpoint.trim()
    if (notification.eventId > 0L) {
        return listOf(cleanEndpoint, notification.eventId.toString()).joinToString(separator = "\u001f")
    }
    return listOf(
        cleanEndpoint,
        "fallback",
        notification.sender.trim(),
        notification.sourceSessionName.trim(),
        notification.message.trim(),
    ).joinToString(separator = "\u001f")
}

private fun sha256Hex(input: String): String {
    val digest = MessageDigest.getInstance("SHA-256")
        .digest(input.toByteArray(Charsets.UTF_8))
    return digest.joinToString(separator = "") { byte -> "%02x".format(byte) }
}

internal fun formatWallSource(sender: String, sourceSessionName: String): String {
    val cleanSender = sender.trim()
    val cleanSession = sourceSessionName.trim()
    if (cleanSender.isEmpty()) return cleanSession
    if (cleanSession.isEmpty()) return cleanSender
    return "$cleanSender#$cleanSession"
}

internal fun formatWallBody(message: String): String = message.trim()

internal fun formatWallContent(sender: String, sourceSessionName: String, message: String): String {
    val source = formatWallSource(sender, sourceSessionName)
    val body = formatWallBody(message)
    if (body.isNotEmpty()) return body
    if (source.isNotEmpty()) return source
    return body
}
