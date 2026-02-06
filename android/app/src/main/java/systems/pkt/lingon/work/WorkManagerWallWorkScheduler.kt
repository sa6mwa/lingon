package systems.pkt.lingon.work

import android.content.Context
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import systems.pkt.lingon.data.WallWorkStateStore

class WorkManagerWallWorkScheduler(
    private val appContext: Context,
    private val stateStore: WallWorkStateStore,
    private val scope: CoroutineScope,
) : WallWorkScheduler {
    override fun setEnabled(enabled: Boolean) {
        val manager = WorkManager.getInstance(appContext)
        if (!enabled) {
            manager.cancelUniqueWork(uniqueWorkName)
            return
        }
        val constraints = Constraints.Builder()
            .setRequiredNetworkType(NetworkType.CONNECTED)
            .build()
        val request = PeriodicWorkRequestBuilder<WallPollWorker>(15, TimeUnit.MINUTES)
            .setConstraints(constraints)
            .build()
        manager.enqueueUniquePeriodicWork(
            uniqueWorkName,
            ExistingPeriodicWorkPolicy.UPDATE,
            request,
        )
    }

    override fun resetCursor() {
        scope.launch(Dispatchers.IO) {
            stateStore.clear()
        }
    }

    companion object {
        const val uniqueWorkName = "lingon_wall_poll_worker"
    }
}
