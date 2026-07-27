# ghostalert

Your Ghostty tabs, mirrored onto an Android phone as a grid of coloured tiles.
Each tile is one tab: it shows the tab's name, what the agent in it is doing,
and a short message. Tap a tile and the Mac raises that window and switches to
that tab.

The point is a second screen for long-running terminal work — coding agents,
builds, deploys, anything you start and then wait on. Glance at the phone to see
which of nine tabs needs you, tap it, and you are there without hunting through
a tab bar.

```
┌──────────────┬──────────────┐
│ 🟨 PROJECT1  │ 🟦 BRYNTUM   │
│ tests green  │ needs approv │
│ DONE (green) │ WAITING ◀────┼── amber, pulsing, buzzes the phone
├──────────────┼──────────────┤
│ 🟩 SPEAKEASY │ 🟪 RITZA     │
│ building     │              │
│ WORKING(blue)│ IDLE (grey)  │
└──────────────┴──────────────┘
```

## Parts

| Part | What it is |
| --- | --- |
| `ghostalert` | A single Go binary: the daemon, the CLI that pushes states, and the web UI |
| `android/` | The phone app (Kotlin + Compose), talks to the daemon over your LAN |
| `hooks/` | An agent-neutral script for reporting state from a tab |

The daemon holds the grid, streams it to every client over server-sent events,
and is the only thing that touches Ghostty. Nothing leaves your network.

## Install

Requires Go 1.23+ and macOS for the Ghostty control (the daemon builds and runs
elsewhere, but focusing a tab is macOS-only).

```sh
make install            # builds and installs to ~/.local/bin
```

Then start the daemon **from a Ghostty tab**:

```sh
ghostalert serve
```

Running it from a terminal matters: driving Ghostty goes through macOS
accessibility, and a process inherits that permission from the app that started
it. Ghostty already has the grant if any of your window-management scripts work.
Launching the daemon from a launchd job instead means granting Accessibility to
the binary separately.

Pair the phone:

```sh
ghostalert url
# host:  http://192.168.1.71:7337
# token: yb2x5dchsxge
# web:   http://192.168.1.71:7337/#t=yb2x5dchsxge
```

Open the `web:` URL in any browser, on the Mac or on a phone. It is the same
grid with the same tap-to-focus behaviour, so the Android app is optional: use
it for notifications when a tab starts waiting, and the browser for everything
else. Chrome only offers a real installed-app experience over HTTPS, so on a
plain LAN address "Add to Home Screen" gives a shortcut that opens in a tab.

## Set up the grid

```sh
ghostalert tabs            # every window of every running Ghostty instance
ghostalert refresh         # make tiles from the window with the most tabs
ghostalert grid 2 6        # 2 wide, 6 down (up to 6 x 12)
ghostalert status
```

`refresh` fills the tiles with your tab names. Tapping a tile works from this
point on.

## Colours

Tiles are coloured by state, not by tab: grey idle, blue working, amber
waiting, green done, red error. The grid reads as a status board from across
the room, which is the only thing worth colouring for.

A tab's own colour goes in its title as an emoji, since Ghostty has no per-tab
colour setting and will not tell anyone what colour a tab is. The tile shows
the title verbatim, so the marker appears there exactly as it does in the tab
bar:

```
🟨 SPEAKEASY      🟦 BRYNTUM      🟩 KA
```

`ghostalert mark RITZA red` writes the marker for you, through Ghostty's
scripting interface. That only answers for one instance, and no other route is
safe: renaming through the menu means taking over the keyboard, which is not
something a background tool should do while you are working.


To let a tab push its own status, run this **in that tab**:

```sh
ghostalert register             # binds this tab to its tile
ghostalert register --name CI   # …and relabels the tile
```

Registration reads which tab is currently showing in that Ghostty window, so run
it from the tab you mean — or skip it and let the hook below claim each tab the
first time it is used. Once bound, the tile is tied to the tab's terminal device
and any process in it can update the tile with no further lookups:

```sh
ghostalert set working "running tests"
ghostalert set waiting "needs approval"
ghostalert set done
```

States are `idle`, `working`, `waiting`, `done` and `error`. `waiting` and
`error` pulse on the phone and raise a notification; tapping the notification
focuses the tab.

## Reporting state

Tiles only show `idle` until something tells them otherwise. `ghostalert-hook`
is the adapter, and knows nothing about any particular agent:

```sh
ghostalert-hook --bind working        # started, and claim this tab's tile
ghostalert-hook waiting "approve?"    # blocked on a human
ghostalert-hook done
```

`--bind` is what saves registering each tab by hand, so use it on an event that
means the human just acted in this tab. See `hooks/` for wiring it to an agent
with JSON hook config, to a Makefile, or to a shell function that reports on
anything slow you run.

`make install` puts it on your `PATH`. It always exits 0, so a stopped daemon
cannot break the thing it is reporting on.

## The phone app

```sh
make apk                 # android/app/build/outputs/apk/release/app-release.apk
make install-apk         # …and adb install it
```

Tested on a OnePlus 5T running LineageOS 22.2 (Android 15).

Building needs JDK 17 and an Android SDK with `platforms;android-35` and
`build-tools;35.0.0`; override `JAVA_HOME_17` and `ANDROID_SDK` if yours live
elsewhere. The release APK is signed with the debug key so it sideloads without
any keystore setup.

With the phone on USB, set it up without touching the screen:

```sh
ghostalert pair          # sends this machine's address and token to the app
```

Without a cable, open the `app:` line from `ghostalert url` in the phone's
browser. That page serves the APK the daemon has on hand and, because the link
carries the token, a second tap points the freshly installed app at this
machine. `make apk` puts the built APK where the daemon can find it.

Otherwise tap the gear and paste the `web:` URL from `ghostalert url` — the app
pulls the token out of the `#t=` fragment, so there is no need to type it
separately. A `ghostalert://192.168.1.71:7337#t=token` link does the same thing
from a QR code or a message. The grid size is whatever the daemon serves and can
be changed from the same dialog.

The app runs a foreground service so tiles keep updating and alerts still arrive
with the app in the background. It reconnects on its own after the network drops
or the phone wakes.

## How a tap finds the right tab

Ghostty ships an AppleScript dictionary — windows, tabs, terminals, `select
tab`, and `perform action` for any Ghostty action string. That is the first
thing tried: it needs no permission, names tabs by id, and is how `mark` sets a
title.

It cannot see everything, though. Apple events are addressed to a bundle
identifier, so when several Ghostty processes are running — a hotkey window
started with its own `--config-file` is one — only the instance the system has
registered answers, and it does not follow which one is frontmost. So the
fallback is the accessibility API, where Ghostty publishes its tab bar as a
group of radio buttons, one per tab, each named after the tab. Clicking one
switches tab, and unlike ⌘1…⌘9 it reaches a tenth tab and does not care which
window is frontmost.

Renaming a tab there takes a third route. The tab bar is read-only to
accessibility — `AXTitle` reports `settable=false` and writing it is silently
ignored — and Ghostty draws the "Change Tab Title" prompt inside its GPU
surface, so it adds nothing to the accessibility tree to type into. What works
is driving the menu: select the tab, open the prompt, and paste through the
Edit menu. Typing is no good because System Events drops anything outside the
basic multilingual plane, which is every coloured square, and the prompt
ignores ⌘V. The clipboard is saved and put back, and the result is confirmed by
reading the tab bar, since nothing can inspect the prompt itself.

Two details this has to get right:

- **More than one Ghostty can be running.** Every accessibility lookup walks all
  processes named Ghostty, and tiles remember which instance their tab came
  from. `tell process "Ghostty"` reaches only one, and holding onto element
  references across processes silently returns the wrong process's answers, so
  each pass re-runs the query and works on the loop variable.
- **Tab titles you set by hand are permanent.** Ghostty's "change title" prompt
  makes a tab ignore the escape sequences programs use to rename it, so a tab
  cannot be identified by writing a marker to its terminal and reading the tab
  bar back. Those hand-set titles are stable and unique, which makes them a good
  key instead: that is what tiles match on, falling back to tab position. It is
  also why the title is where a tab's colour has to come from.

## Troubleshooting

`ghostalert doctor` prints the config path, the phone URL, which tab you are in,
what Ghostty is showing, and whether the daemon is up.

**"no Ghostty windows" or a doctor line saying nothing resolved.** Accessibility
returns an empty window list for every app while the screen is locked. Unlock and
try again. If it persists with the screen awake, the process running the daemon
lacks the Accessibility grant — start it from a Ghostty tab, or add it under
System Settings → Privacy & Security → Accessibility.

**Focus lands on the wrong tab, or a tile is stale.** The tab bar changed since
the tiles were built. Run `ghostalert refresh`, or press ⟳ on the phone.

**The phone shows "offline".** Check that the phone is on the same network, and
that `curl http://<mac>:7337/health` answers from another machine. macOS may
prompt to allow incoming connections the first time the daemon starts.

## Configuration

`~/.config/ghostalert/config.json`:

```json
{
  "addr": ":7337",
  "token": "yb2x5dchsxge",
  "cols": 2,
  "rows": 5,
  "palette": ["#f7f3de", "#dee7f7", "#f7dede", "..."]
}
```

Tiles live in `state.json` beside it. `GHOSTALERT_HOME` moves both.

## API

Every route under `/api` needs the token, as `X-Ghostalert-Token` or `?token=`.

| Route | Purpose |
| --- | --- |
| `GET /api/state` | the grid right now |
| `GET /api/events` | server-sent events, one `state` event per change |
| `POST /api/tile` | create or patch a tile |
| `POST /api/focus` | `{"slot":3}` — raise that tab |
| `POST /api/grid` | `{"cols":3,"rows":8}` |
| `POST /api/clear` | `{"slot":3}` or `{"all":true}` |
| `GET /api/tabs` | every Ghostty window and its tabs |
| `POST /api/refresh` | rebuild the tiles from a window's tabs |

## Licence

MIT
