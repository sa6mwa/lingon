package systems.pkt.lingon.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import systems.pkt.lingon.data.LingonClient
import systems.pkt.lingon.data.relay.RelayWebSocketClient
import systems.pkt.lingon.work.WallWorkScheduler

class AppViewModelFactory(
    private val repository: LingonClient,
    private val wsClient: RelayWebSocketClient,
    private val wallNotifier: WallNotifier,
    private val wallWorkScheduler: WallWorkScheduler,
) : ViewModelProvider.Factory {
    override fun <T : ViewModel> create(modelClass: Class<T>): T {
        if (modelClass.isAssignableFrom(AppViewModel::class.java)) {
            @Suppress("UNCHECKED_CAST")
            return AppViewModel(repository, wsClient, wallNotifier, wallWorkScheduler) as T
        }
        throw IllegalArgumentException("Unknown ViewModel class")
    }
}
