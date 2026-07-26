package com.sixhobbits.ghostalert

import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.IBinder
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch

/**
 * A foreground service so the tiles keep updating, and a tab that starts
 * waiting still reaches you, while the app is in the background.
 */
class AlertService : Service() {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)

    override fun onCreate() {
        super.onCreate()
        Notifications.ensureChannels(this)
        startForeground(
            Notifications.SERVICE_NOTIFICATION_ID,
            Notifications.serviceNotification(this, Link.CONNECTING, Repo.host),
        )
        Repo.start()
        scope.launch {
            Repo.link.collectLatest { link ->
                startForeground(
                    Notifications.SERVICE_NOTIFICATION_ID,
                    Notifications.serviceNotification(this@AlertService, link, Repo.host),
                )
            }
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int = START_STICKY

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    companion object {
        fun start(context: Context) {
            if (!Repo.configured) return
            ContextCompat.startForegroundService(context, Intent(context, AlertService::class.java))
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, AlertService::class.java))
        }
    }
}
