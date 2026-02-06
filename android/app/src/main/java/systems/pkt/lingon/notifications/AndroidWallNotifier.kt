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
import java.util.concurrent.atomic.AtomicInteger
import systems.pkt.lingon.MainActivity
import systems.pkt.lingon.R
import systems.pkt.lingon.viewmodel.WallNotifier

class AndroidWallNotifier(private val context: Context) : WallNotifier {
    override fun notifyWall(sender: String, message: String) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            val granted = context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED
            if (!granted) {
                return
            }
        }
        ensureChannel()
        val title = if (sender.isNotBlank()) "Broadcast from $sender" else "Broadcast"
        val body = message.trim()
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
        val notification = NotificationCompat.Builder(context, channelID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(NotificationCompat.BigTextStyle().bigText(body))
            .setAutoCancel(true)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setContentIntent(pendingIntent)
            .build()
        NotificationManagerCompat.from(context).notify(nextID.incrementAndGet(), notification)
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
        val nextID = AtomicInteger(1000)
    }
}
