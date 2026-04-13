package systems.pkt.lingon.work

import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat

class AndroidBackgroundWallServiceController(
    private val appContext: Context,
) : BackgroundWallServiceController {
    override fun setEnabled(enabled: Boolean) {
        val intent = Intent(appContext, BackgroundWallForegroundService::class.java)
        if (enabled) {
            ContextCompat.startForegroundService(appContext, intent)
        } else {
            appContext.stopService(intent)
        }
    }
}
