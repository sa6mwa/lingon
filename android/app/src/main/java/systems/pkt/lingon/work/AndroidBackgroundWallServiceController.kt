package systems.pkt.lingon.work

import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat
import java.util.concurrent.atomic.AtomicBoolean

class AndroidBackgroundWallServiceController(
    private val appContext: Context,
) : BackgroundWallServiceController {
    private val enabled = AtomicBoolean(false)

    override fun setEnabled(enabled: Boolean) {
        if (this.enabled.getAndSet(enabled) == enabled) {
            return
        }
        val intent = Intent(appContext, BackgroundWallForegroundService::class.java)
        if (enabled) {
            ContextCompat.startForegroundService(appContext, intent)
        } else {
            intent.action = BackgroundWallForegroundService.actionStop
            ContextCompat.startForegroundService(appContext, intent)
        }
    }
}
