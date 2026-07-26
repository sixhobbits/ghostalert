package com.sixhobbits.ghostalert

import android.app.Application

class GhostAlertApp : Application() {
    override fun onCreate() {
        super.onCreate()
        Repo.init(this)
        Notifications.ensureChannels(this)
        AlertService.start(this)
    }
}
