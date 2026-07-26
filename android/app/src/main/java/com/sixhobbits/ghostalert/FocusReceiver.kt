package com.sixhobbits.ghostalert

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** Tapping an alert jumps straight to that tab on the Mac. */
class FocusReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val slot = intent.getIntExtra(EXTRA_SLOT, 0)
        if (slot <= 0) return
        Repo.init(context)
        // The request leaves on a background thread, so hold the broadcast open
        // until it lands or the process may be killed mid-flight.
        val pending = goAsync()
        Repo.focus(slot) { pending.finish() }
    }

    companion object {
        const val EXTRA_SLOT = "slot"
    }
}
