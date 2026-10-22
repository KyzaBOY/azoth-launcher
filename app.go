package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/KyzaBOY/azoth-launcher/internal/account"
	"github.com/KyzaBOY/azoth-launcher/internal/client"
	"github.com/KyzaBOY/azoth-launcher/internal/config"
)

// Events sent to the frontend (runtime.EventsOn on the JS side).
const (
	evBusy    = "app:busy"        // bool: Play or the login is running
	evAccount = "account:changed" // AccountInfo: login, logout or an expired session
	evStatus  = "play:status"     // string: the status line under Play

	ticketTimeout = 10 * time.Second // asking the account server for the Play ticket
	protonScript  = "proton"         // the script every Proton folder has (the protonPath setting)
)

// msgNotAllowed is what a player sees when the server's access list refuses
// their Discord account (HTTP 403 on login or on the Play ticket).
const msgNotAllowed = "Your Discord account doesn't have access yet. The playtest is closed for now."

// App is the object exposed to the frontend (window.go.main.App).
type App struct {
	ctx context.Context

	mu           sync.Mutex
	loginCancel  context.CancelFunc // Discord login in progress (CancelLogin)
	searchCancel context.CancelFunc // folder search in progress (CancelSearch)
	cfg          config.Config
	busy         bool

	// emit sends an event to the frontend. It is a no-op until startup, and tests replace it.
	emit func(event string, data ...any)
}

// AccountInfo is the Discord login the screen shows (it never carries the session or the ticket).
type AccountInfo struct {
	LoggedIn bool   `json:"loggedIn"`
	ID       string `json:"id"`      // Discord id
	Name     string `json:"name"`    // Discord name
	Expires  int64  `json:"expires"` // end of the session (Unix, s)
}

// PlayResult is the outcome of Launch. Error is a sentence for the player;
// Notice replaces "Game started" when the game started with a caveat.
type PlayResult struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Notice string `json:"notice,omitempty"`
}

// SearchResult is the outcome of SearchInstalls.
type SearchResult struct {
	Installs  []client.Install `json:"installs"`
	Complete  bool             `json:"complete"`  // false: the time cap hit before every folder was checked
	Cancelled bool             `json:"cancelled"` // the player cancelled the search
}

func NewApp() *App { return &App{emit: func(string, ...any) {}} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.emit = func(event string, data ...any) { runtime.EventsEmit(ctx, event, data...) }
	cfg, err := config.Load()
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	if err != nil {
		runtime.LogWarning(ctx, "config: "+err.Error())
	}
}

func (a *App) setBusy(b bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if b && a.busy {
		return false
	}
	a.busy = b
	a.emit(evBusy, b)
	return true
}

func (a *App) setStatus(s string) { a.emit(evStatus, s) }

func (a *App) snapshot() config.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

// ---- bindings -----------------------------------------------------------

// GetVersion returns the launcher version.
func (a *App) GetVersion() string { return Version }

// GetPlatform returns "windows" or "linux": the screen shows the Linux
// settings only on Linux.
func (a *App) GetPlatform() string { return goruntime.GOOS }

// GetConfig returns the current settings.
func (a *App) GetConfig() config.Config { return a.snapshot() }

// SaveConfig saves the settings. The New World folder is only checked for
// existing here; whether it really is New World is checked when the player
// presses Play. The copy folder becomes <picked>\AzothClient (see copyDirFor)
// and has to be outside the game. When the copy moves, the old one is deleted
// (client.RemoveCopy) and the next Play builds it in the new folder.
func (a *App) SaveConfig(c config.Config) (config.Config, error) {
	c.GameDir = cleanPath(c.GameDir)
	c.ProtonPath = cleanPath(c.ProtonPath)
	if c.GameDir != "" {
		if _, err := os.Stat(c.GameDir); err != nil {
			return a.snapshot(), errors.New("That folder doesn't exist.")
		}
	}
	if c.ProtonPath != "" {
		if st, err := os.Stat(filepath.Join(c.ProtonPath, protonScript)); err != nil || st.IsDir() {
			return a.snapshot(), errors.New("That Proton folder has no proton script in it.")
		}
	}
	c.CopyDir = cleanPath(c.CopyDir)
	if c.CopyDir != "" {
		if !filepath.IsAbs(c.CopyDir) {
			return a.snapshot(), errors.New("Choose a full folder path for the game copy.")
		}
		c.CopyDir = copyDirFor(c.CopyDir)
		if st, err := os.Stat(filepath.Dir(c.CopyDir)); err != nil || !st.IsDir() {
			return a.snapshot(), errors.New("The game copy folder doesn't exist.")
		}
		if st, err := os.Stat(c.CopyDir); err == nil && !st.IsDir() {
			return a.snapshot(), fmt.Errorf("%s is a file. Choose another folder for the game copy.", c.CopyDir)
		}
		if client.IsSteamInstall(filepath.Join(c.CopyDir, "Bin64")) {
			return a.snapshot(), errors.New("The game copy can't go inside a game install. Choose another folder.")
		}
		if root, _, err := client.ResolveGame(c.GameDir); err == nil && (client.PathInside(c.CopyDir, root) || client.PathInside(root, c.CopyDir)) {
			return a.snapshot(), errors.New("The game copy can't go inside your New World folder. Choose another folder.")
		}
	}
	a.mu.Lock()
	old := copyRoot(a.cfg)
	a.cfg = c
	a.mu.Unlock()
	if err := config.Save(c); err != nil {
		return c, fmt.Errorf("Couldn't save the settings: %w", err)
	}
	if !client.SamePath(old, copyRoot(c)) {
		if _, err := os.Stat(old); err == nil {
			if err := client.RemoveCopy(old); err != nil { // the old copy only costs disk space
				runtime.LogWarning(a.ctx, "old game copy: "+err.Error())
			}
		}
	}
	return c, nil
}

// DefaultCopyDir is where the game copy lives when no folder is picked.
func (a *App) DefaultCopyDir() string { return client.IsolatedCopyDir(config.DataDir()) }

// cleanPath trims spaces and the quotes Explorer puts around "Copy as path".
func cleanPath(p string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(p), `"`))
}

// PickGameDir opens the folder picker for the New World folder.
func (a *App) PickGameDir() (string, error) {
	return a.PickFolder("New World folder", a.snapshot().GameDir)
}

// PickFolder opens the folder picker with title, starting at start if it exists.
func (a *App) PickFolder(title, start string) (string, error) {
	if _, err := os.Stat(start); err != nil {
		start = ""
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: start,
	})
}

// SearchInstalls looks for New World installs (Steam libraries and a scan of
// the fixed drives). It blocks until it is done, cancelled with CancelSearch or
// the time cap hits; the screen awaits it without freezing.
func (a *App) SearchInstalls() (SearchResult, error) {
	a.mu.Lock()
	if a.searchCancel != nil {
		a.mu.Unlock()
		return SearchResult{}, errors.New("A search is already running.")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.searchCancel = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.searchCancel = nil
		a.mu.Unlock()
		cancel()
	}()

	installs, complete, err := client.Search(ctx, client.SearchOptions{Skip: []string{config.DataDir(), copyRoot(a.snapshot())}})
	if err != nil { // only a cancelled context
		return SearchResult{Installs: []client.Install{}, Cancelled: true}, nil
	}
	if installs == nil {
		installs = []client.Install{}
	}
	return SearchResult{Installs: installs, Complete: complete}, nil
}

// CancelSearch stops the search in progress.
func (a *App) CancelSearch() {
	a.mu.Lock()
	cancel := a.searchCancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// GetAccount returns the stored Discord login (an expired session counts as logged out).
func (a *App) GetAccount() AccountInfo {
	s, ok := account.Load(config.DataDir())
	if !ok {
		return AccountInfo{}
	}
	return AccountInfo{LoggedIn: true, ID: s.User.ID, Name: s.User.Name, Expires: s.Expires}
}

// LoginDiscord opens the browser on the account site's Discord login and waits
// for it to come back (up to account.LoginTimeout). The session is stored
// encrypted with DPAPI in account.dat.
func (a *App) LoginDiscord() (AccountInfo, error) {
	if err := account.ValidateURL(accountURL); err != nil {
		return a.GetAccount(), errBadAccountAt
	}
	if !a.setBusy(true) {
		return a.GetAccount(), errors.New("Something is already in progress.")
	}
	defer a.setBusy(false)
	open := func(u string) error {
		runtime.BrowserOpenURL(a.ctx, u)
		return nil
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	a.loginCancel = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.loginCancel = nil
		a.mu.Unlock()
		cancel()
	}()
	s, err := account.Login(ctx, accountURL, open)
	if errors.Is(err, context.Canceled) {
		return a.GetAccount(), account.ErrDenied
	}
	if errors.Is(err, account.ErrNotAllowed) {
		return a.GetAccount(), errors.New(msgNotAllowed)
	}
	if err != nil {
		return a.GetAccount(), err
	}
	if err := account.Save(config.DataDir(), s); err != nil {
		return a.GetAccount(), fmt.Errorf("Couldn't save the login: %w", err)
	}
	info := a.GetAccount()
	a.emit(evAccount, info)
	return info, nil
}

// CancelLogin gives up on the login in progress (the person closed the browser tab).
func (a *App) CancelLogin() {
	a.mu.Lock()
	cancel := a.loginCancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Logout deletes the stored session.
func (a *App) Logout() AccountInfo {
	if err := account.Clear(config.DataDir()); err != nil {
		runtime.LogWarning(a.ctx, "account: "+err.Error())
	}
	info := a.GetAccount()
	a.emit(evAccount, info)
	return info
}

// Launch starts the game with the in-memory patches. It refuses without a
// Discord login. Progress goes out as evStatus events; the result carries the
// error text, if any. Neither the session nor the ticket is ever logged or shown.
func (a *App) Launch() PlayResult {
	if !a.setBusy(true) {
		return PlayResult{Error: "Something is already in progress."}
	}
	defer a.setBusy(false)
	notice, err := a.play()
	if err != nil {
		return PlayResult{Error: err.Error()}
	}
	return PlayResult{OK: true, Notice: notice}
}

func (a *App) play() (notice string, err error) {
	gw, err := parseGateway(gatewayHost, gatewayPort)
	if err != nil {
		return "", err
	}
	dataDir := config.DataDir()
	sess, ok := account.Load(dataDir)
	if !ok {
		return "", errNotLoggedIn
	}
	cfg := a.snapshot()
	binDir, err := prepareGame(cfg.GameDir, copyRoot(cfg), a.setStatus)
	if err != nil {
		return "", err
	}
	ticket, err := a.ticket(dataDir, sess)
	if err != nil {
		return "", err
	}
	a.setStatus(statusStarting)
	notice, err = a.launchGame(cfg, client.LaunchOptions{
		BinDir:      binDir,
		GatewayAddr: gw.addr,
		PingHost:    gw.host,
		Persona:     ticket,
		RegionName:  regionName,
	})
	if err != nil { // the launch already killed the process if a patch failed
		return "", startError(err)
	}
	return notice, nil
}

// ticket trades the stored session for the ticket that goes into the game as
// the persona, and turns the account server's answers into texts for the player.
func (a *App) ticket(dataDir string, s account.Session) (string, error) {
	if err := account.ValidateURL(accountURL); err != nil {
		return "", errBadAccountAt
	}
	ctx, cancel := context.WithTimeout(a.ctx, ticketTimeout)
	defer cancel()
	ticket, _, err := account.Ticket(ctx, accountURL, s.Token)
	switch {
	case err == nil:
		return ticket, nil
	case errors.Is(err, account.ErrSessionExpired):
		_ = account.Clear(dataDir)
		a.emit(evAccount, AccountInfo{})
		return "", errors.New("Your login expired. Log in with Discord again.")
	case errors.Is(err, account.ErrNotAllowed): // the session stays: only access is missing
		return "", errors.New(msgNotAllowed)
	default:
		return "", fmt.Errorf("Could not reach the account server: %w", err)
	}
}
