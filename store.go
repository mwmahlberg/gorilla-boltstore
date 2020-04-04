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

const DefaultBucketname = "_boltstore_sessions"

var ErrSessionNotStored = errors.New("session not found in store")

type IDGeneratorFunc func() (string, error)
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
	return func() (string, error) {
		return uuid.NewV4().String(), nil
	}
}

func Keys(keyPairs ...[]byte) SessionStoreOption {
	return func(s *store) error {
		s.codecs = securecookie.CodecsFromPairs(keyPairs...)
		return nil
	}
}

func IDGenerator(f IDGeneratorFunc) SessionStoreOption {
	return func(s *store) error {
		s.genfunc = f
		return nil
	}
}

func SessionBucket(name string) SessionStoreOption {
	return func(s *store) error {
		s.bucket = []byte(name)
		return nil
	}
}

func SessionOptions(options *sessions.Options) SessionStoreOption {
	return func(s *store) error {
		s.sessionOpts = options
		return nil
	}
}

// NewBoltDBSessionStore creates a new session store for gorilla/sessions backed by
// "go.etcd.io/bbolt".
func NewBoltDBSessionStore(db *bolt.DB, opts ...SessionStoreOption) (sessions.Store, error) {
	s := &store{db: db}
	var err error
	for _, opt := range opts {
		if err = opt(s); err != nil {
			return nil, fmt.Errorf("sessionstore: applying option: %s", err)
		}
	}
	if s.genfunc == nil {
		s.genfunc = DefaultIDGenerator()
	}

	if len(s.bucket) == 0 {
		s.bucket = []byte(DefaultBucketname)
	}
	return s, nil
}

// New satisfies the sessions.Store interface.
func (s *store) New(r *http.Request, name string) (*sessions.Session, error) {

	var err error

	sess := sessions.NewSession(s, name)
	sess.IsNew = true

	if sess.ID, err = s.genfunc(); err != nil {
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
