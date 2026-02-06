package systems.pkt.lingon

import android.app.Application
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import systems.pkt.lingon.data.AuthClient
import systems.pkt.lingon.data.HttpClientProvider
import systems.pkt.lingon.data.EndpointStore
import systems.pkt.lingon.data.FontSizeStore
import systems.pkt.lingon.data.LingonRepository
import systems.pkt.lingon.data.PersistentCookieJar
import systems.pkt.lingon.data.TerminalResizeStore
import systems.pkt.lingon.data.AppLockStore
import systems.pkt.lingon.data.WallWorkStateStore
import systems.pkt.lingon.data.ZoomStore
import systems.pkt.lingon.data.certs.CertificateStore
import systems.pkt.lingon.data.relay.RelaySessionsClient
import systems.pkt.lingon.data.relay.RelayWebSocketClient
import systems.pkt.lingon.notifications.AndroidWallNotifier
import systems.pkt.lingon.viewmodel.WallNotifier
import systems.pkt.lingon.work.WallWorkScheduler
import systems.pkt.lingon.work.WorkManagerWallWorkScheduler

private val Application.dataStore by preferencesDataStore(name = "lingon")

class LingonApplication : Application() {
    lateinit var repository: LingonRepository
        private set
    lateinit var wsClient: RelayWebSocketClient
        private set
    lateinit var certificateStore: CertificateStore
        private set
    lateinit var wallNotifier: WallNotifier
        private set
    lateinit var wallWorkStateStore: WallWorkStateStore
        private set
    lateinit var wallWorkScheduler: WallWorkScheduler
        private set

    private val appScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    override fun onCreate() {
        super.onCreate()
        val endpointStore = EndpointStore(dataStore, appScope)
        val fontSizeStore = FontSizeStore(dataStore, appScope)
        val zoomStore = ZoomStore(dataStore, appScope)
        val terminalResizeStore = TerminalResizeStore(dataStore, appScope)
        val appLockStore = AppLockStore(dataStore, appScope)
        val cookieJar = PersistentCookieJar(dataStore, appScope)
        wallWorkStateStore = WallWorkStateStore(dataStore)
        certificateStore = CertificateStore(dataStore)
        val httpClientProvider = HttpClientProvider(certificateStore, cookieJar)
        val authClient = AuthClient(httpClientProvider, endpointStore)
        val sessionsClient = RelaySessionsClient(httpClientProvider, endpointStore)
        wsClient = RelayWebSocketClient(httpClientProvider)
        wallNotifier = AndroidWallNotifier(this)
        wallWorkScheduler = WorkManagerWallWorkScheduler(this, wallWorkStateStore, appScope)
        repository = LingonRepository(
            authClient,
            sessionsClient,
            certificateStore,
            endpointStore,
            fontSizeStore,
            zoomStore,
            terminalResizeStore,
            appLockStore,
        )
    }
}
