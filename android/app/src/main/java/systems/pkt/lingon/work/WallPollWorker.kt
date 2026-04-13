package systems.pkt.lingon.work

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import kotlinx.coroutines.flow.first
import systems.pkt.lingon.LingonApplication
import systems.pkt.lingon.data.ApiException
import systems.pkt.lingon.viewmodel.WallNotification

class WallPollWorker(
    appContext: Context,
    params: WorkerParameters,
) : CoroutineWorker(appContext, params) {
    override suspend fun doWork(): Result {
        val app = applicationContext as? LingonApplication ?: return Result.success()
        val endpoint = app.repository.endpointFlow.first().trim()
        if (endpoint.isBlank()) {
            return Result.success()
        }
        val since = app.wallWorkStateStore.loadCursor(endpoint)
        return try {
            // Keep worker runs lightweight: one wall events page per cadence.
            val page = app.repository.listWallEvents(sinceId = since, limit = workerPageLimit)
            var next = since
            page.events.forEach { event ->
                if (event.message.isBlank()) {
                    return@forEach
                }
                app.wallNotifier.notifyWall(
                    WallNotification(
                        eventId = event.id,
                        sender = event.sender,
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
                app.wallWorkStateStore.saveCursor(endpoint, next)
            }
            Result.success()
        } catch (err: ApiException) {
            // Keep periodic cadence stable; auth/logout state will toggle scheduling.
            if (err.statusCode == 401) {
                app.wallWorkScheduler.setEnabled(false)
                return Result.success()
            }
            Result.success()
        } catch (_: Exception) {
            Result.success()
        }
    }

    private companion object {
        private const val workerPageLimit = 100
    }
}
