package account

import (
	"errors"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

// The Secret Service API (freedesktop.org), spoken over the session bus. The
// session is stored as one item with the attributes below; the "plain"
// transfer is what libsecret uses too (the bus is local to this user).
const (
	secretsName     = "org.freedesktop.secrets"
	secretsPath     = dbus.ObjectPath("/org/freedesktop/secrets")
	serviceIface    = "org.freedesktop.Secret.Service"
	collectionIface = "org.freedesktop.Secret.Collection"
	itemIface       = "org.freedesktop.Secret.Item"
	promptIface     = "org.freedesktop.Secret.Prompt"
	noPrompt        = dbus.ObjectPath("/")

	secretLabel   = "Azoth Launcher (Discord session)"
	secretApp     = "azoth-launcher"
	secretType    = "application/json"
	promptTimeout = 2 * time.Minute // the person unlocks the keyring in a system dialog
)

// dbusSecret is the Secret struct of the API: (oayays).
type dbusSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

type secretService struct{}

// secretConn is an open connection with a "plain" session and the unlocked
// default collection.
type secretConn struct {
	conn       *dbus.Conn
	session    dbus.ObjectPath
	collection dbus.ObjectPath
}

// attributes find the item of one data directory, so a test directory never
// touches the real session.
func attributes(dir string) map[string]string {
	return map[string]string{"application": secretApp, "dataDir": dir}
}

func openSecrets() (*secretConn, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errNoService, err)
	}
	svc := conn.Object(secretsName, secretsPath)
	var out dbus.Variant
	var session dbus.ObjectPath
	if err := svc.Call(serviceIface+".OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&out, &session); err != nil {
		conn.Close()
		return nil, fmt.Errorf("%w: %w", errNoService, err)
	}
	var coll dbus.ObjectPath
	if err := svc.Call(serviceIface+".ReadAlias", 0, "default").Store(&coll); err != nil || coll == noPrompt {
		conn.Close()
		return nil, fmt.Errorf("%w: no default collection", errNoService)
	}
	s := &secretConn{conn: conn, session: session, collection: coll}
	if err := s.unlock([]dbus.ObjectPath{coll}); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *secretConn) close() {
	_ = s.conn.Object(secretsName, s.session).Call("org.freedesktop.Secret.Session.Close", 0).Err
	s.conn.Close()
}

// unlock unlocks paths, showing the keyring's own dialog when it asks for one.
func (s *secretConn) unlock(paths []dbus.ObjectPath) error {
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := s.conn.Object(secretsName, secretsPath).Call(serviceIface+".Unlock", 0, paths).Store(&unlocked, &prompt); err != nil {
		return fmt.Errorf("unlock the keyring: %w", err)
	}
	return s.prompt(prompt)
}

// prompt runs a Secret Service prompt and waits for the person to answer it.
func (s *secretConn) prompt(p dbus.ObjectPath) error {
	if p == noPrompt || p == "" {
		return nil
	}
	if err := s.conn.AddMatchSignal(dbus.WithMatchObjectPath(p), dbus.WithMatchInterface(promptIface), dbus.WithMatchMember("Completed")); err != nil {
		return err
	}
	signals := make(chan *dbus.Signal, 1)
	s.conn.Signal(signals)
	defer s.conn.RemoveSignal(signals)
	if err := s.conn.Object(secretsName, p).Call(promptIface+".Prompt", 0, "").Err; err != nil {
		return fmt.Errorf("keyring prompt: %w", err)
	}
	timeout := time.After(promptTimeout)
	for {
		select {
		case sig := <-signals:
			if sig.Path != p || len(sig.Body) == 0 {
				continue
			}
			if dismissed, _ := sig.Body[0].(bool); dismissed {
				return errors.New("the keyring was not unlocked")
			}
			return nil
		case <-timeout:
			return errors.New("the keyring was not unlocked in time")
		}
	}
}

// find returns the item of dir, unlocking it if needed ("" = none).
func (s *secretConn) find(dir string) (dbus.ObjectPath, error) {
	var unlocked, locked []dbus.ObjectPath
	if err := s.conn.Object(secretsName, secretsPath).Call(serviceIface+".SearchItems", 0, attributes(dir)).Store(&unlocked, &locked); err != nil {
		return "", fmt.Errorf("search the keyring: %w", err)
	}
	if len(unlocked) > 0 {
		return unlocked[0], nil
	}
	if len(locked) == 0 {
		return "", nil
	}
	if err := s.unlock(locked[:1]); err != nil {
		return "", err
	}
	return locked[0], nil
}

func (secretService) get(dir string) ([]byte, error) {
	s, err := openSecrets()
	if err != nil {
		return nil, err
	}
	defer s.close()
	item, err := s.find(dir)
	if err != nil {
		return nil, err
	}
	if item == "" {
		return nil, errNoSecret
	}
	var sec dbusSecret
	if err := s.conn.Object(secretsName, item).Call(itemIface+".GetSecret", 0, s.session).Store(&sec); err != nil {
		return nil, fmt.Errorf("read the keyring: %w", err)
	}
	return sec.Value, nil
}

func (secretService) set(dir string, secret []byte) error {
	s, err := openSecrets()
	if err != nil {
		return err
	}
	defer s.close()
	props := map[string]dbus.Variant{
		itemIface + ".Label":      dbus.MakeVariant(secretLabel),
		itemIface + ".Attributes": dbus.MakeVariant(attributes(dir)),
	}
	sec := dbusSecret{Session: s.session, Parameters: []byte{}, Value: secret, ContentType: secretType}
	var item, prompt dbus.ObjectPath
	if err := s.conn.Object(secretsName, s.collection).Call(collectionIface+".CreateItem", 0, props, sec, true).Store(&item, &prompt); err != nil {
		return fmt.Errorf("write the keyring: %w", err)
	}
	return s.prompt(prompt)
}

func (secretService) delete(dir string) error {
	s, err := openSecrets()
	if err != nil {
		return err
	}
	defer s.close()
	item, err := s.find(dir)
	if err != nil || item == "" {
		return err
	}
	var prompt dbus.ObjectPath
	if err := s.conn.Object(secretsName, item).Call(itemIface+".Delete", 0).Store(&prompt); err != nil {
		return fmt.Errorf("delete from the keyring: %w", err)
	}
	return s.prompt(prompt)
}
