package com.sixhobbits.ghostalert

import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import com.sixhobbits.ghostalert.ui.GhostAlertScreen

class MainActivity : ComponentActivity() {

    private val requestNotifications =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        Repo.init(this)
        applyPairing(intent)

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            requestNotifications.launch(android.Manifest.permission.POST_NOTIFICATIONS)
        }

        // A wall-mounted or bedside phone showing the grid should not keep
        // dimming out while you watch a build.
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)

        setContent { GhostAlertScreen() }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        applyPairing(intent)
    }

    override fun onStart() {
        super.onStart()
        Repo.start()
        AlertService.start(this)
    }

    /**
     * Accepts the address and token from outside the app, so `ghostalert pair`
     * can set the phone up over USB and a `ghostalert://` link works from a QR
     * code or a message. Typing a token on a phone keyboard is nobody's idea of
     * a good time.
     */
    private fun applyPairing(intent: Intent?) {
        val raw = intent?.getStringExtra(EXTRA_URL) ?: intent?.data?.toString() ?: return
        val (address, token) = Repo.parseAddress(raw)
        if (address.isBlank()) return
        Repo.host = address
        if (token.isNotBlank()) Repo.token = token
        Repo.restart()
        AlertService.start(this)
    }

    companion object {
        const val EXTRA_URL = "url"
    }
}
