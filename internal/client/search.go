package client

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// DefaultSearchDepth is how far below a scan root the scan looks for
	// <dir>/Bin64/NewWorld.exe.
	DefaultSearchDepth = 3
	// DefaultSearchTimeout is the total time cap of a Search.
	DefaultSearchTimeout = 20 * time.Second
)

// Install is a New World install found by Search.
type Install struct {
	Path      string `json:"path"`      // install root: the folder that holds Bin64\NewWorld.exe
	Supported bool   `json:"supported"` // normalized sha256 == ExpectedSHA256
}

// SearchOptions tunes Search. The zero value searches the Steam libraries and
// the platform's scan roots (searchRoots).
type SearchOptions struct {
	Roots   []string      // folders to scan instead of searchRoots() (tests)
	NoSteam bool          // skip the Steam libraries (tests)
	Skip    []string      // folders that are never entered (the launcher's own data directory)
	Depth   int           // levels below each root; 0 = DefaultSearchDepth
	Timeout time.Duration // total time cap; 0 = DefaultSearchTimeout
}

// skipNames are folders never entered, at any depth.
var skipNames = map[string]bool{
	"windows":                   true,
	"$recycle.bin":              true,
	"system volume information": true,
	"programdata":               true,
}

// Search finds New World installs: every Steam library that has one, plus a
// scan of every scan root (fixed drives on Windows, disk mounts and the home
// folder on Linux) for <dir>/Bin64/NewWorld.exe up to opts.Depth levels below
// the root. The scan skips system folders (skipNames, alwaysSkip), hidden
// folders and anything in opts.Skip, and does not follow junctions or
// symlinks. Each hit gets its normalized sha256 checked.
//
// The scan stops at opts.Timeout: complete is then false and the hits found so
// far are returned. If ctx is cancelled, Search returns ctx's error.
func Search(ctx context.Context, opts SearchOptions) (installs []Install, complete bool, err error) {
	depth := opts.Depth
	if depth <= 0 {
		depth = DefaultSearchDepth
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultSearchTimeout
	}
	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var roots []string
	seen := map[string]bool{}
	add := func(root string) {
		key := pathKey(root)
		if !seen[key] {
			seen[key] = true
			roots = append(roots, filepath.Clean(root))
		}
	}

	if !opts.NoSteam {
		for _, r := range FindSteamInstalls() {
			add(r)
		}
	}
	skip := map[string]bool{}
	for _, list := range [][]string{alwaysSkip(), opts.Skip} {
		for _, s := range list {
			if s != "" {
				skip[pathKey(s)] = true
			}
		}
	}
	scanRoots := opts.Roots
	if scanRoots == nil {
		scanRoots = searchRoots()
	}
	for _, r := range scanRoots {
		scan(scanCtx, r, depth, skip, add)
	}
	complete = scanCtx.Err() == nil
	if ctx.Err() != nil { // cancelled by the caller (not by the time cap)
		return nil, false, ctx.Err()
	}

	for _, r := range roots {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		sum, _, herr := NormalizedSHA256(filepath.Join(r, "Bin64", ExeName))
		installs = append(installs, Install{Path: r, Supported: herr == nil && sum == ExpectedSHA256})
	}
	sortInstalls(installs)
	return installs, complete, nil
}

// sortInstalls puts supported installs first, then orders by path.
func sortInstalls(in []Install) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Supported != in[j].Supported {
			return in[i].Supported
		}
		return strings.ToLower(in[i].Path) < strings.ToLower(in[j].Path)
	})
}

// scan walks root breadth-first (shallow hits first) down to depth levels and
// calls found for every directory that holds Bin64/NewWorld.exe. It also looks
// for a Steam library layout (steamapps/common/New World), so a library that
// Steam no longer lists is still found. It returns when ctx is done.
func scan(ctx context.Context, root string, depth int, skip map[string]bool, found func(string)) {
	type item struct {
		dir   string
		level int
	}
	queue := []item{{root, 0}}
	for len(queue) > 0 {
		if ctx.Err() != nil {
			return
		}
		it := queue[0]
		queue = queue[1:]

		if hasExe(it.dir) {
			found(it.dir)
		}
		if strings.EqualFold(filepath.Base(it.dir), "steamapps") {
			if nw := filepath.Join(it.dir, "common", "New World"); hasExe(nw) {
				found(nw)
			}
		}
		if it.level >= depth {
			continue
		}
		entries, err := os.ReadDir(it.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !walkable(e) || skipNames[strings.ToLower(e.Name())] {
				continue
			}
			child := filepath.Join(it.dir, e.Name())
			if skip[pathKey(child)] {
				continue
			}
			queue = append(queue, item{child, it.level + 1})
		}
	}
}
