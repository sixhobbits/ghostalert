# ghostalert

Your Ghostty tabs, mirrored onto an Android phone as a grid of coloured tiles.
Each tile is one tab: it shows the tab's name, what the agent in it is doing,
and a short message. Tap a tile and the Mac raises that window and switches to
that tab.

The point is a second screen for long-running terminal agents. Glance at the
phone to see which of nine Claude Code sessions is waiting on you, tap it, and
you are in the right tab without hunting through a tab bar.

```
┌──────────────┬──────────────┐
│ UNSILOED     │ BRYNTUM      │
│ tests green  │ needs approv │
│ DONE         │ WAITING ◀────┼── pulses orange, buzzes the phone
├──────────────┼──────────────┤
│ SPEAKEASY    │ RITZA        │
│ building     │              │
│ WORKING      │ IDLE         │
└──────────────┴──────────────┘
```

## Parts

| Part | What it is |
| --- | --- |
| `ghostalert` | A single Go binary: the daemon, the CLI that pushes states, and the web UI |
| `android/` | The phone app (Kotlin + Compose), talks to the daemon over your LAN |
| `hooks/claude-code/` | A hook script that wires Claude Code's lifecycle to tile states |

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

Colours are a guess: Ghostty keeps a tab's background to itself, so the palette
in `~/.config/ghostalert/config.json` is handed out by position, which matches
the usual rainbow-window script only while the tabs are in the order it made
them. Correct a tile by name, or read the true colour from the tab itself:

```sh
ghostalert color SPEAKEASY yellow    # yellow blue red purple orange green
ghostalert color 3 '#f7f3de'         # white black pink cyan, or any hex
ghostalert color --detect            # run in the tab: asks the terminal
```

`--detect` is exact. It asks the terminal for its background colour with an OSC
11 query, which only works from a shell prompt in the tab you are reading — the
answer arrives through that terminal's input, so a full-screen program running
there would swallow it. Colours stick to the tab and survive a refresh.

Nothing watches the tab bar, so run `refresh` again after closing, renaming,
reordering or opening tabs — or press ⟳ on the phone or in the web UI, which
does the same thing. Tabs that are still open keep their tile, including its
state, its colour and the shell bound to it; tiles whose tab has gone disappear,
and everything below shifts up. A tile you renamed with `--name` keeps that
name.

To let a tab push its own status, run this **in that tab**:

```sh
ghostalert register             # binds this tab to its tile
ghostalert register --name CI   # …and relabels the tile
```

Registration reads which tab is currently showing in that Ghostty window, so run
it from the tab you mean — or skip it entirely and let the Claude Code hooks
below claim each tab the first time you use it. Once bound, the tile is tied to
the tab's terminal device and any process in it can update the tile with no
further lookups:

```sh
ghostalert set working "running tests"
ghostalert set waiting "needs approval"
ghostalert set done
```

States are `idle`, `working`, `waiting`, `done` and `error`. `waiting` and
`error` pulse on the phone and raise a notification; tapping the notification
focuses the tab.

## Claude Code hooks

Copy the fragment in `hooks/claude-code/settings.example.json` into
`~/.claude/settings.json` to get tiles that follow every session:

- `UserPromptSubmit` → `working`, and claims a tile for the tab if it has none
- `Notification` → `waiting`, with Claude's own reason as the message
- `Stop` → `done`

That first hook is where tabs register themselves: submitting a prompt means you
were looking at that tab, which is exactly the condition tab lookup needs. Use
sessions as usual and the grid fills in.

`make install` puts `ghostalert-claude-hook` on your `PATH`. It exits 0 no matter
what, so a stopped daemon never breaks a turn.

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

Otherwise tap the gear and paste the `web:` URL from `ghostalert url` — the app
pulls the token out of the `#t=` fragment, so there is no need to type it
separately. A `ghostalert://192.168.1.71:7337#t=token` link does the same thing
from a QR code or a message. The grid size is whatever the daemon serves and can
be changed from the same dialog.

The app runs a foreground service so tiles keep updating and alerts still arrive
with the app in the background. It reconnects on its own after the network drops
or the phone wakes.

## How a tap finds the right tab

Ghostty publishes its tab bar to the macOS accessibility API as a group of radio
buttons, one per tab, each named after the tab. Focusing means raising the window
and clicking one — better than sending ⌘1…⌘9, which cannot reach a tenth tab and
depends on which window is frontmost.

Two details this has to get right:

- **More than one Ghostty can be running.** A hotkey window launched with its own
  `--config-file` is a separate process, and `tell process "Ghostty"` only ever
  reaches one of them. Every lookup here walks all of them, and tiles remember
  which instance their tab came from.
- **Tab titles you set by hand are permanent.** Ghostty's "change title" prompt
  makes a tab ignore the escape sequences programs use to rename it, so a tab
  cannot be identified by writing a marker to its terminal and reading the tab
  bar back. Those hand-set titles are stable and unique, which makes them a good
  key instead: that is what tiles match on, falling back to tab position.

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
