package com.sixhobbits.ghostalert

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.os.Build
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat

/**
 * Notifications turn the tiles into something you can react to with the phone
 * in your pocket: a tab that starts waiting for input rings once, and tapping
 * the notification jumps straight to that tab on the Mac.
 */
object Notifications {
    const val CHANNEL_STATUS = "status"
    const val CHANNEL_SERVICE = "service"
    const val SERVICE_NOTIFICATION_ID = 1

    /** States worth interrupting for. */
    private val ALERTING = setOf("waiting", "error")

    fun ensureChannels(context: Context) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val manager = context.getSystemService(NotificationManager::class.java)

        val status = NotificationChannel(
            CHANNEL_STATUS,
            context.getString(R.string.channel_status_name),
            NotificationManager.IMPORTANCE_HIGH,
        ).apply {
            description = context.getString(R.string.channel_status_desc)
            enableVibration(true)
        }

        val service = NotificationChannel(
            CHANNEL_SERVICE,
            context.getString(R.string.channel_service_name),
            NotificationManager.IMPORTANCE_MIN,
        ).apply {
            description = context.getString(R.string.channel_service_desc)
            setShowBadge(false)
        }

        manager.createNotificationChannel(status)
        manager.createNotificationChannel(service)
    }

    /** Ring for tiles that have just entered a state worth looking at. */
    fun onSnapshot(context: Context, previous: Snapshot?, current: Snapshot) {
        val before = previous?.tiles?.associateBy { it.slot } ?: emptyMap()
        for (tile in current.tiles) {
            val was = before[tile.slot]?.state
            if (tile.state in ALERTING && was != tile.state) {
                alert(context, tile)
            } else if (tile.state !in ALERTING && was in ALERTING) {
                cancel(context, tile.slot)
            }
        }
    }

    private fun alert(context: Context, tile: Tile) {
        if (!canPost(context)) return

        val focus = PendingIntent.getBroadcast(
            context,
            tile.slot,
            Intent(context, FocusReceiver::class.java).putExtra(FocusReceiver.EXTRA_SLOT, tile.slot),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

        val text = tile.message.ifBlank {
            if (tile.state == "error") "hit an error" else "waiting for you"
        }
        val notification = Notification.Builder(context, CHANNEL_STATUS)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(tile.name)
            .setContentText(text)
            .setStyle(Notification.BigTextStyle().bigText(text))
            .setColor(runCatching { Color.parseColor(tile.color) }.getOrDefault(Color.WHITE))
            .setCategory(Notification.CATEGORY_STATUS)
            .setAutoCancel(true)
            .setContentIntent(focus)
            .build()

        NotificationManagerCompat.from(context).notify(tile.slot + 100, notification)
    }

    private fun cancel(context: Context, slot: Int) {
        NotificationManagerCompat.from(context).cancel(slot + 100)
    }

    private fun canPost(context: Context): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
            ContextCompat.checkSelfPermission(
                context,
                android.Manifest.permission.POST_NOTIFICATIONS,
            ) == PackageManager.PERMISSION_GRANTED

    /** The quiet, permanent notification that keeps the service alive. */
    fun serviceNotification(context: Context, link: Link, host: String): Notification {
        val open = PendingIntent.getActivity(
            context,
            0,
            Intent(context, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val text = when (link) {
            Link.LIVE -> host.ifBlank { "connected" }
            Link.CONNECTING -> "connecting…"
            Link.OFFLINE -> "offline"
        }
        return Notification.Builder(context, CHANNEL_SERVICE)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle("ghostalert")
            .setContentText(text)
            .setOngoing(true)
            .setContentIntent(open)
            .build()
    }
}
