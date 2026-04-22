package systems.pkt.lingon.work

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import systems.pkt.lingon.LingonApplication
import systems.pkt.lingon.MainActivity
import systems.pkt.lingon.R
import systems.pkt.lingon.data.ApiException
import systems.pkt.lingon.viewmodel.WallNotification

class BackgroundWallForegroundService : Service() {
    private val serviceScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var pollJob: Job? = null

    override fun onCreate() {
        super.onCreate()
        ensureChannel()
        startForeground(notificationId, buildNotification())
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (pollJob?.isActive != true) {
            pollJob = serviceScope.launch {
                runPollLoop()
            }
        }
        return START_STICKY
    }

    override fun onDestroy() {
        pollJob?.cancel()
        serviceScope.cancel()
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    private suspend fun runPollLoop() {
        val app = applicationContext as? LingonApplication ?: return
        while (serviceScope.isActive) {
            val endpoint = app.repository.endpointFlow.first().trim()
            if (endpoint.isBlank()) {
                delay(pollIntervalMs)
                continue
            }
            val since = app.wallWorkStateStore.loadCursor(endpoint)
            try {
                val page = app.repository.listWallEvents(sinceId = since, limit = servicePageLimit)
                var next = since
                page.events.forEach { event ->
                    if (event.message.isBlank()) {
                        return@forEach
                    }
                    app.wallDeliveryCoordinator.deliver(
                        WallNotification(
                            endpoint = endpoint,
                            eventId = event.id,
                            sender = event.sender,
                            sourceSessionName = event.sessionName ?: "",
                            message = event.message,
                        ),
                    )
                    if (event.id > next) {
                        next = event.id
                    }
                }
                if (page.nextId > next) {
                    next = page.nextId
                }
                if (next > since) {
                    app.wallDeliveryCoordinator.advanceCursor(endpoint, next)
                }
            } catch (err: ApiException) {
                if (err.statusCode == 401) {
                    stopSelf()
                    return
                }
            } catch (_: Exception) {
            }
            delay(pollIntervalMs)
        }
    }

    private fun buildNotification(): android.app.Notification {
        val launchIntent = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
        }
        val pendingFlags = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        } else {
            PendingIntent.FLAG_UPDATE_CURRENT
        }
        val pendingIntent = PendingIntent.getActivity(this, 0, launchIntent, pendingFlags)
        return NotificationCompat.Builder(this, channelID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle("Lingon background notifications")
            .setContentText("Receiving wall notifications in the background")
            .setOngoing(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setGroup(notificationGroupID)
            .setContentIntent(pendingIntent)
            .build()
    }

    private fun ensureChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            return
        }
        val manager = getSystemService(NotificationManager::class.java) ?: return
        val existing = manager.getNotificationChannel(channelID)
        if (existing != null) {
            return
        }
        val channel = NotificationChannel(
            channelID,
            "Lingon Background Notifications",
            NotificationManager.IMPORTANCE_LOW,
        )
        channel.description = "Keeps wall notifications flowing while the app is backgrounded"
        manager.createNotificationChannel(channel)
    }

    private companion object {
        private const val channelID = "lingon_background_wall"
        private const val notificationGroupID = "lingon_background_wall_group"
        private const val notificationId = 2001
        private const val pollIntervalMs = 5_000L
        private const val servicePageLimit = 100
    }
}
