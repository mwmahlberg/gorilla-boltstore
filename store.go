package boltstore

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	uuid "github.com/satori/go.uuid"
	bolt "go.etcd.io/bbolt"
)

// DefaultBucketname is unsurprisingly the default name of the bucket in
// which the sessions are stored.
const DefaultBucketname = "_boltstore_sessions"

var (
	// ErrInsufficientKeys is returned by New if no key were given for encryption
	// and/or signing of the cookies.
	ErrInsufficientKeys = errors.New("No keys or keypairs were given")

	// ErrSessionNotStored is returned by Get if there was a valid session,
	// but no data was found in the database.
	ErrSessionNotStored = errors.New("session not found in store")
)

// An IDGeneratorFunc is used to generate a unique session ID.
type IDGeneratorFunc func(*http.Request) (string, error)

// A SessionStoreOption sets parameters for the session store.
type SessionStoreOption func(s *store) error

type store struct {
	db          *bolt.DB
	genfunc     IDGeneratorFunc
	bucket      []byte
	sessionOpts *sessions.Options
	codecs      []securecookie.Codec
}

// DefaultIDGenerator is the default implementation of IDGeneratorFunc.
// It generates a UUID V4 string.
func DefaultIDGenerator() IDGeneratorFunc {
	return func(_ *http.Request) (string, error) {
		return uuid.NewV4().String(), nil
	}
}

// Keys sets the key pairs for encryting and signing the secure cookies
// set.
//
func Keys(keyPairs ...[]byte) SessionStoreOption {
	return func(s *store) error {
		s.codecs = securecookie.CodecsFromPairs(keyPairs...)
		return nil
	}
}

// IDGenerator sets the function that is used to generate unique IDs for each session.
//
// By default, a UUID V4 is used to generate unique IDs.
func IDGenerator(f IDGeneratorFunc) SessionStoreOption {
	return func(s *store) error {
		s.genfunc = f
		return nil
	}
}

// SessionBucket sets the name of the boltdb bucket in which the sessions are stored.
func SessionBucket(name string) SessionStoreOption {
	return func(s *store) error {
		s.bucket = []byte(name)
		return nil
	}
}

// SessionOptions sets the options for the sessions.
func SessionOptions(options *sessions.Options) SessionStoreOption {
	return func(s *store) error {
		s.sessionOpts = options
		return nil
	}
}

// New creates a new session store for gorilla/sessions backed by
// "go.etcd.io/bbolt". The configured bucket is also created.
//
// Returns a new session store or nil and an error if an error occured.
// If no keys were given, the error returned is ErrInsufficientKeys.
func New(db *bolt.DB, opts ...SessionStoreOption) (sessions.Store, error) {
	s := &store{
		db: db,
		sessionOpts: &sessions.Options{
			Path:   "/",
			MaxAge: 86400 * 30,
		},
	}
	var err error
	for _, opt := range opts {
		if err = opt(s); err != nil {
			return nil, fmt.Errorf("sessionstore: applying option: %s", err)
		}
	}

	if len(s.codecs) == 0 {
		return nil, ErrInsufficientKeys
	}

	if s.genfunc == nil {
		s.genfunc = DefaultIDGenerator()
	}

	if len(s.bucket) == 0 {
		s.bucket = []byte(DefaultBucketname)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(s.bucket)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("initializing bucket: %s", err)
	}
	return s, nil
}

// New satisfies the sessions.Store interface.
func (s *store) New(r *http.Request, name string) (*sessions.Session, error) {

	var err error

	sess := sessions.NewSession(s, name)
	opts := *s.sessionOpts
	sess.Options = &opts
	sess.IsNew = true

	if sess.ID, err = s.genfunc(r); err != nil {
		return nil, fmt.Errorf("generating ID: %s", err)
	}
	return sess, nil
}

func (s *store) Get(r *http.Request, name string) (*sessions.Session, error) {

	var sess *sessions.Session

	id, err := retrieveSessionID(r, name)

	if err != nil && err == http.ErrNoCookie {
		sess, _ = s.New(r, name)
		return sess, nil
	} else if err != nil {
		return nil, fmt.Errorf("retrieving session cookie: %s", err)
	}

	err = s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(s.bucket).Get([]byte(id))
		if raw == nil {
			return ErrSessionNotStored
		}
		sess = sessions.NewSession(s, name)
		sess.ID = id
		err := securecookie.DecodeMulti(name, string(raw), &sess.Values, s.codecs...)
		if err != nil {
			return fmt.Errorf("unmarshalling session: %s", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("retrieving session from database: %s", err)
	}
	return sess, nil
}

func (s *store) Save(r *http.Request, w http.ResponseWriter, sess *sessions.Session) error {

	err := s.db.Update(func(tx *bolt.Tx) error {
		sess.IsNew = false
		d, err := securecookie.EncodeMulti(sess.Name(), &sess.Values, s.codecs...)
		if err != nil {
			return fmt.Errorf("encoding session: %s", err)
		}
		return tx.Bucket(s.bucket).Put([]byte(sess.ID), []byte(d))
	})

	if err != nil {
		return fmt.Errorf("saving session %s: %s", sess.ID, err)
	}

	http.SetCookie(w, s.newCookie(sess.Name(), sess.ID))
	return nil
}

func (s *store) newCookie(name, id string) *http.Cookie {
	return &http.Cookie{
		Name:  name,
		Value: id,
	}
}

func retrieveSessionID(r *http.Request, name string) (string, error) {
	c, err := r.Cookie(name)

	if err != nil && err == http.ErrNoCookie {
		return "", err
	} else if err != nil {
		return "", fmt.Errorf("retrieving session cookie: %s", err)
	}

	return c.Value, nil
}
