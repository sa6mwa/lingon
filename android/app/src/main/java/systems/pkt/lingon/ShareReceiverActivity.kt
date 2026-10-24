package systems.pkt.lingon

import android.content.Intent
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import systems.pkt.lingon.share.ShareTokens

class ShareReceiverActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val sharedText = if (intent?.action == Intent.ACTION_SEND && intent.type == "text/plain") {
            intent.getStringExtra(Intent.EXTRA_TEXT)
        } else {
            null
        }
        val parsed = sharedText?.let { ShareTokens.findInText(it) }
        val launchIntent = Intent(this, MainActivity::class.java).apply {
            addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
            if (parsed != null) {
                val bare = ShareTokens.bareToken(parsed)
                val requestId = PendingShareLaunches.put(bare, parsed.endpoint)
                action = MainActivity.ACTION_INTERNAL_SHARE
                putExtra(MainActivity.EXTRA_SHARE_REQUEST_ID, requestId)
            }
        }
        if (parsed == null) {
            Toast.makeText(this, "No Lingon token found", Toast.LENGTH_SHORT).show()
        }
        startActivity(launchIntent)
        finish()
    }
}
