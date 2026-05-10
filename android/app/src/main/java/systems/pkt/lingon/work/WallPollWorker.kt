package systems.pkt.lingon.work

import android.content.Context
import android.util.Log
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import kotlinx.coroutines.flow.first
import systems.pkt.lingon.LingonApplication
import systems.pkt.lingon.data.ApiException
import systems.pkt.lingon.notifications.WallPollStatus

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
        return try {
            val result = app.wallDeliveryCoordinator.pollOnce(endpoint, workerPageLimit) { since, limit ->
                app.repository.listWallEvents(sinceId = since, limit = limit)
            }
            if (result.status == WallPollStatus.Reset) {
                Log.w(logTag, "worker cursor reset detected endpoint=$endpoint since=${result.since}")
                return Result.retry()
            }
            Result.success()
        } catch (err: ApiException) {
            Log.w(logTag, "worker api failed endpoint=$endpoint status=${err.statusCode}", err)
            // Keep periodic cadence stable; auth/logout state will toggle scheduling.
            if (err.statusCode == 401) {
                app.wallWorkScheduler.setEnabled(false)
                return Result.success()
            }
            Result.success()
        } catch (err: Exception) {
            Log.w(logTag, "worker failed endpoint=$endpoint", err)
            Result.success()
        }
    }

    private companion object {
        private const val workerPageLimit = 100
        private const val logTag = "lingon-wall-worker"
    }
}
