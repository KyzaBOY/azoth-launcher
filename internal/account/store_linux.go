package account

import "errors"

// secretStore keeps one secret per data directory.
type secretStore interface {
	get(dir string) ([]byte, error) // errNoSecret when there is none
	set(dir string, secret []byte) error
	delete(dir string) error // no error when there is none
}

var (
	// errNoService: there is no Secret Service on the session bus (or no
	// default collection), so the session goes to account.dat instead.
	errNoService = errors.New("no Secret Service keyring")
	// errNoSecret: the keyring works but holds no session for this directory.
	errNoSecret = errors.New("no session in the keyring")
)

// keyring is the Secret Service (GNOME Keyring, KWallet, KeePassXC...). Tests
// replace it.
var keyring secretStore = secretService{}

// saveBlob puts the session in the keyring. Without a keyring it goes to
// account.dat with mode 0600, NOT encrypted (the README says so). A session
// saved to the keyring removes an older account.dat, so no copy stays on disk.
func saveBlob(dir string, plain []byte) error {
	err := keyring.set(dir, plain)
	if err == nil {
		return removeAccountFile(dir)
	}
	if !errors.Is(err, errNoService) {
		return err
	}
	return writeAccountFile(dir, plain)
}

// loadBlob reads the session from the keyring, or from account.dat when the
// keyring has none (no keyring, or a session saved before there was one).
func loadBlob(dir string) ([]byte, error) {
	plain, err := keyring.get(dir)
	if err == nil {
		return plain, nil
	}
	if !errors.Is(err, errNoService) && !errors.Is(err, errNoSecret) {
		return nil, err
	}
	return readAccountFile(dir)
}

// clearBlob deletes the session from the keyring and account.dat.
func clearBlob(dir string) error {
	kerr := keyring.delete(dir)
	if errors.Is(kerr, errNoService) {
		kerr = nil
	}
	return errors.Join(kerr, removeAccountFile(dir))
}
