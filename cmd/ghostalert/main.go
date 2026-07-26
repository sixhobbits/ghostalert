// Command ghostalert mirrors Ghostty tabs onto a phone as tappable tiles.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sixhobbits/ghostalert/internal/client"
	"github.com/sixhobbits/ghostalert/internal/config"
	"github.com/sixhobbits/ghostalert/internal/ghostty"
	"github.com/sixhobbits/ghostalert/internal/server"
	"github.com/sixhobbits/ghostalert/internal/state"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `ghostalert - mirror Ghostty tabs onto your phone as tappable tiles

Usage:
  ghostalert serve [--addr :7337]        run the daemon (also serves the web UI)
  ghostalert url                         print the URL and token to pair a phone
  ghostalert pair                        push those to a USB-attached phone
  ghostalert status                      print the tile grid
  ghostalert tabs                        list Ghostty windows and tabs

  ghostalert set <state> [message]       update the tile for the current tab
  ghostalert set --bind <state>          …claiming this tab's tile if it is new
  ghostalert register [--name X]         bind the current tab to a tile
  ghostalert refresh [--window 1]        rebuild the tiles from a window's tabs,
                                         keeping state and bindings for tabs
                                         that are still open (alias: adopt)
  ghostalert focus <slot|name>           raise a tab on this machine
  ghostalert color <slot|name> <colour>  recolour a tile (name or #hex)
  ghostalert color --detect              …read it from the tab you are in
  ghostalert grid <cols> <rows>          resize the phone grid
  ghostalert clear [<slot>|--all]        remove tiles
  ghostalert doctor                      check the setup

States: idle working waiting done error

Flags common to client commands:
  --url    daemon base URL (default http://127.0.0.1:<configured port>)
  --token  access token (default: from the config file)
`

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]

	var err error
	switch cmd {
	case "serve":
		err = cmdServe(args)
	case "set":
		err = cmdSet(args)
	case "register":
		err = cmdRegister(args)
	case "focus":
		err = cmdFocus(args)
	case "color", "colour":
		err = cmdColor(args)
	case "status":
		err = cmdStatus(args)
	case "url":
		err = cmdURL(args)
	case "pair":
		err = cmdPair(args)
	case "grid":
		err = cmdGrid(args)
	case "clear":
		err = cmdClear(args)
	case "tabs":
		err = cmdTabs(args)
	case "adopt", "refresh":
		err = cmdRefresh(args)
	case "doctor":
		err = cmdDoctor(args)
	case "version", "--version", "-v":
		fmt.Println("ghostalert", version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghostalert:", err)
		os.Exit(1)
	}
}

// clientFlags adds --url/--token to a flag set and resolves them against config.
type clientFlags struct {
	url   string
	token string
}

func (c *clientFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&c.url, "url", "", "daemon base URL")
	fs.StringVar(&c.token, "token", "", "access token")
}

func (c *clientFlags) build() (*client.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	base := c.url
	if base == "" {
		base = os.Getenv("GHOSTALERT_URL")
	}
	if base == "" {
		base = fmt.Sprintf("http://127.0.0.1:%d", server.PortOf(cfg.Addr))
	}
	token := c.token
	if token == "" {
		token = os.Getenv("GHOSTALERT_TOKEN")
	}
	if token == "" {
		token = cfg.Token
	}
	return client.New(base, token), nil
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "", "listen address (default from config)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *addr != "" && *addr != cfg.Addr {
		// Persist it, or `ghostalert url` and every client command would keep
		// pointing at the old port and quietly fail to reach this daemon.
		cfg.Addr = *addr
		if err := cfg.Save(); err != nil {
			return err
		}
	}

	host, _ := os.Hostname()
	store, err := state.New(filepath.Join(config.Dir(), "state.json"), host, cfg.Cols, cfg.Rows)
	if err != nil {
		return err
	}
	cfg.Cols, cfg.Rows = store.Grid()

	logger := log.New(os.Stderr, "", log.LstdFlags)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.New(cfg, store, logger),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	logger.Printf("ghostalert %s listening on %s", version, cfg.Addr)
	logger.Printf("phone: %s/#t=%s", server.LANAddr(cfg.Addr), cfg.Token)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func cmdSet(args []string) error {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	var cf clientFlags
	cf.bind(fs)
	slot := fs.Int("slot", 0, "tile slot (default: the current tab's tile)")
	name := fs.String("name", "", "tile name")
	color := fs.String("color", "", "tile colour, e.g. '#dee7f7'")
	tty := fs.String("tty", "", "terminal device (default: detected)")
	bind := fs.Bool("bind", false, "also claim this tab's tile, as `register` does")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return errors.New("usage: ghostalert set <state> [message]")
	}
	st := strings.ToLower(rest[0])
	if !state.ValidState(st) {
		return fmt.Errorf("unknown state %q (want one of: %s)", st, strings.Join(state.States, " "))
	}
	msg := strings.Join(rest[1:], " ")

	c, err := cf.build()
	if err != nil {
		return err
	}
	req := map[string]any{"state": st, "message": msg}
	if *slot > 0 {
		req["slot"] = *slot
	}
	if *name != "" {
		req["name"] = *name
	}
	if *color != "" {
		req["color"] = *color
	}
	t := pickTTY(*tty)
	if t != "" {
		req["tty"] = t
	}
	if *bind && t != "" {
		// Cheap enough to redo on every call, and it keeps the tab's title and
		// position fresh for a tile that was bound long ago.
		if loc, err := ghostty.Locate(t); err == nil {
			req["tabTitle"] = loc.TabTitle
			req["pid"] = loc.PID
			req["window"] = loc.WindowTitle
			req["tabIndex"] = loc.Tab
		}
	}
	var tile state.Tile
	if err := c.Post("/api/tile", req, &tile); err != nil {
		return err
	}
	fmt.Printf("slot %d  %s  %s  %s\n", tile.Slot, tile.Name, tile.State, tile.Message)
	return nil
}

func cmdRegister(args []string) error {
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	var cf clientFlags
	cf.bind(fs)
	slot := fs.Int("slot", 0, "tile slot (default: this tab's position)")
	name := fs.String("name", "", "tile name (default: the tab's title)")
	color := fs.String("color", "", "tile colour, e.g. '#dee7f7'")
	tty := fs.String("tty", "", "terminal device (default: detected)")
	tabTitle := fs.String("tab", "", "Ghostty tab title (default: this tab's title)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	t := pickTTY(*tty)
	if t == "" && *tabTitle == "" {
		return errors.New("could not detect a terminal device; pass --tty /dev/ttysNNN or --tab TITLE")
	}

	req := map[string]any{"state": state.StateIdle}
	if t != "" {
		req["tty"] = t
	}
	if *tabTitle != "" {
		req["tabTitle"] = *tabTitle
	} else {
		// Registration is an interactive act performed in the tab being
		// registered, so the tab showing in that Ghostty window is this one.
		loc, err := ghostty.Locate(t)
		if err != nil {
			return fmt.Errorf("%w\nrun this from the tab you want to register, or pass --tab TITLE", err)
		}
		req["tabTitle"] = loc.TabTitle
		req["pid"] = loc.PID
		req["window"] = loc.WindowTitle
		req["tabIndex"] = loc.Tab
	}
	if *slot > 0 {
		req["slot"] = *slot
	}
	if *name != "" {
		req["name"] = *name
	}
	if *color != "" {
		req["color"] = *color
	}

	c, err := cf.build()
	if err != nil {
		return err
	}
	var tile state.Tile
	if err := c.Post("/api/tile", req, &tile); err != nil {
		return err
	}
	fmt.Printf("slot %d = %q (tab %q, %s)\n", tile.Slot, tile.Name, tile.TabTitle, tile.TTY)
	return nil
}

func pickTTY(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := os.Getenv("GHOSTALERT_TTY"); env != "" {
		return env
	}
	return ghostty.CurrentTTY()
}

func cmdFocus(args []string) error {
	fs := flag.NewFlagSet("focus", flag.ExitOnError)
	var cf clientFlags
	cf.bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: ghostalert focus <slot|name>")
	}
	c, err := cf.build()
	if err != nil {
		return err
	}
	slot, err := strconv.Atoi(fs.Arg(0))
	if err != nil {
		var snap state.Snapshot
		if err := c.Get("/api/state", &snap); err != nil {
			return err
		}
		for _, t := range snap.Tiles {
			if strings.EqualFold(t.Name, fs.Arg(0)) {
				slot = t.Slot
				break
			}
		}
		if slot == 0 {
			return fmt.Errorf("no tile named %q", fs.Arg(0))
		}
	}
	var out map[string]any
	if err := c.Post("/api/focus", map[string]any{"slot": slot}, &out); err != nil {
		return err
	}
	fmt.Printf("focused slot %d (%v)\n", slot, out["via"])
	return nil
}

// cmdColor recolours a tile. Ghostty keeps a tab's background to itself — it is
// absent from the accessibility tree and from disk — so the colour is either
// named by hand or read from the terminal in that tab with --detect.
func cmdColor(args []string) error {
	fs := flag.NewFlagSet("color", flag.ExitOnError)
	var cf clientFlags
	cf.bind(fs)
	detect := fs.Bool("detect", false, "read the real background of the terminal you run this in")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := cf.build()
	if err != nil {
		return err
	}

	if *detect {
		if !ghostty.TerminalOwned() {
			return errors.New("--detect must be run directly in the tab, at a shell prompt")
		}
		hex, err := ghostty.QueryBackground(600 * time.Millisecond)
		if err != nil {
			return err
		}
		tty := pickTTY("")
		req := map[string]any{"tty": tty, "color": hex}
		if loc, err := ghostty.Locate(tty); err == nil {
			req["tabTitle"] = loc.TabTitle
			req["pid"] = loc.PID
			req["window"] = loc.WindowTitle
			req["tabIndex"] = loc.Tab
		}
		var tile state.Tile
		if err := c.Post("/api/tile", req, &tile); err != nil {
			return err
		}
		fmt.Printf("slot %d  %s  %s\n", tile.Slot, tile.Color, tile.Name)
		return nil
	}

	if fs.NArg() != 2 {
		return fmt.Errorf("usage: ghostalert color <slot|name> <colour>\n"+
			"colours: %s, or a hex code like #f7f3de", strings.Join(config.ColorNames, " "))
	}
	hex, err := config.ResolveColor(fs.Arg(1))
	if err != nil {
		return err
	}

	req := map[string]any{"color": hex}
	if slot, convErr := strconv.Atoi(fs.Arg(0)); convErr == nil {
		req["slot"] = slot
	} else {
		var snap state.Snapshot
		if err := c.Get("/api/state", &snap); err != nil {
			return err
		}
		found := 0
		for _, t := range snap.Tiles {
			if strings.EqualFold(t.Name, fs.Arg(0)) {
				found = t.Slot
				break
			}
		}
		if found == 0 {
			return fmt.Errorf("no tile named %q", fs.Arg(0))
		}
		req["slot"] = found
	}

	var tile state.Tile
	if err := c.Post("/api/tile", req, &tile); err != nil {
		return err
	}
	fmt.Printf("slot %d  %s  %s\n", tile.Slot, tile.Color, tile.Name)
	return nil
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	var cf clientFlags
	cf.bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := cf.build()
	if err != nil {
		return err
	}
	var snap state.Snapshot
	if err := c.Get("/api/state", &snap); err != nil {
		return err
	}
	fmt.Printf("%s  %dx%d grid\n", snap.Host, snap.Cols, snap.Rows)
	for _, t := range snap.Tiles {
		if t.State == state.StateEmpty {
			fmt.Printf("%3d  -\n", t.Slot)
			continue
		}
		fmt.Printf("%3d  %-8s %-22s %-7s %s\n", t.Slot, t.Color, truncate(t.Name, 22), t.State, t.Message)
	}
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func cmdURL(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	base := server.LANAddr(cfg.Addr)
	fmt.Printf("host:  %s\n", base)
	fmt.Printf("token: %s\n", cfg.Token)
	fmt.Printf("web:   %s/#t=%s\n", base, cfg.Token)
	return nil
}

// cmdPair configures the phone app over adb, because typing a token on a phone
// keyboard is miserable and getting one character wrong just says "offline".
func cmdPair(args []string) error {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	device := fs.String("s", "", "adb device serial, when more than one is attached")
	host := fs.String("host", "", "address the phone should use (default: this machine's LAN address)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	base := *host
	if base == "" {
		base = server.LANAddr(cfg.Addr)
	}
	url := fmt.Sprintf("%s/#t=%s", base, cfg.Token)

	adb := []string{}
	if *device != "" {
		adb = append(adb, "-s", *device)
	}
	adb = append(adb,
		"shell", "am", "start",
		"-n", "com.sixhobbits.ghostalert/.MainActivity",
		"-e", "url", url,
	)
	cmd := exec.Command("adb", adb...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	if strings.Contains(string(out), "Error") {
		return fmt.Errorf("adb: %s", strings.TrimSpace(string(out)))
	}
	fmt.Printf("paired phone with %s\n", url)
	return nil
}

func cmdGrid(args []string) error {
	fs := flag.NewFlagSet("grid", flag.ExitOnError)
	var cf clientFlags
	cf.bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: ghostalert grid <cols> <rows>")
	}
	cols, err := strconv.Atoi(fs.Arg(0))
	if err != nil {
		return err
	}
	rows, err := strconv.Atoi(fs.Arg(1))
	if err != nil {
		return err
	}
	c, err := cf.build()
	if err != nil {
		return err
	}
	var snap state.Snapshot
	if err := c.Post("/api/grid", map[string]any{"cols": cols, "rows": rows}, &snap); err != nil {
		return err
	}
	fmt.Printf("grid is now %dx%d (%d tiles)\n", snap.Cols, snap.Rows, snap.Cols*snap.Rows)
	return nil
}

func cmdClear(args []string) error {
	fs := flag.NewFlagSet("clear", flag.ExitOnError)
	var cf clientFlags
	cf.bind(fs)
	all := fs.Bool("all", false, "remove every tile")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := cf.build()
	if err != nil {
		return err
	}
	req := map[string]any{"all": *all}
	if !*all {
		if fs.NArg() != 1 {
			return errors.New("usage: ghostalert clear <slot> | ghostalert clear --all")
		}
		slot, err := strconv.Atoi(fs.Arg(0))
		if err != nil {
			return err
		}
		req["slot"] = slot
	}
	return c.Post("/api/clear", req, nil)
}

func cmdTabs(args []string) error {
	windows, err := ghostty.List()
	if err != nil {
		return err
	}
	if len(windows) == 0 {
		fmt.Println("no Ghostty windows")
		return nil
	}
	for n, w := range windows {
		fmt.Printf("window %d: %q (pid %d, %d tabs)\n", n+1, w.Title, w.PID, len(w.Tabs))
		for i, t := range w.Tabs {
			marker := " "
			if i+1 == w.Selected {
				marker = "*"
			}
			fmt.Printf(" %s %2d  %s\n", marker, i+1, t)
		}
	}
	return nil
}

func cmdRefresh(args []string) error {
	fs := flag.NewFlagSet("refresh", flag.ExitOnError)
	var cf clientFlags
	cf.bind(fs)
	window := fs.Int("window", 0, "window number from `ghostalert tabs` (default: the one with the most tabs)")
	start := fs.Int("start", 1, "first tile slot to fill")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := cf.build()
	if err != nil {
		return err
	}
	var tiles []state.Tile
	if err := c.Post("/api/refresh", map[string]any{"window": *window, "startSlot": *start}, &tiles); err != nil {
		return err
	}
	for _, t := range tiles {
		fmt.Printf("slot %d  %s  %-7s %s\n", t.Slot, t.Color, t.State, t.Name)
	}
	return nil
}

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	var cf clientFlags
	cf.bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Printf("config      %s\n", config.Path())
	fmt.Printf("listen      %s\n", cfg.Addr)
	fmt.Printf("phone URL   %s/#t=%s\n", server.LANAddr(cfg.Addr), cfg.Token)

	if tty := ghostty.CurrentTTY(); tty != "" {
		if loc, err := ghostty.Locate(tty); err == nil {
			fmt.Printf("this tab    %s -> %q (window %q, tab %d)\n", tty, loc.TabTitle, loc.WindowTitle, loc.Tab)
		} else {
			fmt.Printf("this tab    %s (not resolved: %v)\n", tty, err)
		}
	} else {
		fmt.Println("this tab    no tty detected (run from a terminal, or pass --tty)")
	}

	if ghostty.Running() {
		windows, err := ghostty.List()
		if err != nil {
			fmt.Printf("ghostty     running, but the tab bar is unreadable: %v\n", err)
		} else {
			total := 0
			for _, w := range windows {
				total += len(w.Tabs)
			}
			fmt.Printf("ghostty     running, %d window(s), %d tab(s)\n", len(windows), total)
		}
	} else {
		fmt.Println("ghostty     not running (or accessibility permission is missing)")
	}

	c, err := cf.build()
	if err != nil {
		return err
	}
	var snap state.Snapshot
	if err := c.Get("/api/state", &snap); err != nil {
		fmt.Printf("daemon      unreachable: %v\n", err)
		return nil
	}
	used := 0
	for _, t := range snap.Tiles {
		if t.State != state.StateEmpty {
			used++
		}
	}
	fmt.Printf("daemon      ok, %dx%d grid, %d tile(s) in use\n", snap.Cols, snap.Rows, used)
	return nil
}
