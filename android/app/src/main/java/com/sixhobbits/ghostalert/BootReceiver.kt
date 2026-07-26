package com.sixhobbits.ghostalert

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** Reconnect after a reboot so the phone is useful without opening the app. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == Intent.ACTION_BOOT_COMPLETED) {
            Repo.init(context)
            AlertService.start(context)
        }
    }
}
