package systems.pkt.lingon.notifications

import android.Manifest
import android.app.PendingIntent
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import systems.pkt.lingon.MainActivity
import systems.pkt.lingon.R
import systems.pkt.lingon.viewmodel.WallNotification
import systems.pkt.lingon.viewmodel.WallNotifier

class AndroidWallNotifier(private val context: Context) : WallNotifier {
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
        val androidNotification = NotificationCompat.Builder(context, channelID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(title)
            .setContentText(content)
            .setStyle(NotificationCompat.BigTextStyle().bigText(content))
            .setAutoCancel(true)
            .setOnlyAlertOnce(true)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
            .setContentIntent(pendingIntent)
            .build()
        return try {
            notificationManager.notify(
                wallNotificationTag(notification),
                wallNotificationId(notification),
                androidNotification,
            )
            true
        } catch (_: SecurityException) {
            false
        }
    }

    private fun ensureChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            return
        }
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        val existing = manager.getNotificationChannel(channelID)
        if (existing != null) {
            return
        }
        val channel = NotificationChannel(
            channelID,
            "Lingon Broadcasts",
            NotificationManager.IMPORTANCE_HIGH,
        )
        channel.description = "Broadcast notifications from relay wall messages"
        manager.createNotificationChannel(channel)
    }

    private companion object {
        const val channelID = "lingon_wall"
    }
}

private const val wallNotificationFallbackIdBase = 300_000_000
private const val wallNotificationIdMask = 0x0fffffff

internal fun wallNotificationId(notification: WallNotification): Int {
    val folded = wallNotificationKey(notification).hashCode() and wallNotificationIdMask
    return wallNotificationFallbackIdBase + folded
}

internal fun wallNotificationTag(notification: WallNotification): String {
    return "wall:${wallNotificationId(notification)}"
}

private fun wallNotificationKey(notification: WallNotification): String {
    return listOf(
        notification.endpoint.trim(),
        notification.eventId.coerceAtLeast(0L).toString(),
        notification.sender.trim(),
        notification.sourceSessionName.trim(),
        notification.message.trim(),
    ).joinToString(separator = "\u001f")
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
