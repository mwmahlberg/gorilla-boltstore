package boltstore_test

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	boltstore "github.com/mwmahlberg/gorilla-boltstore"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	bolt "go.etcd.io/bbolt"
)

func TestStoreSuite(t *testing.T) {
	suite.Run(t, new(StoreSuite))
}

type StoreSuite struct {
	suite.Suite
	db *bolt.DB
}

func (s *StoreSuite) SetupTest() {
	f, err := ioutil.TempFile("", "boltsessionstore.*.db")
	if err != nil {
		s.FailNow("creating temporary database file", "error: %s", err.Error())
		return
	}
	f.Close()
	s.db, err = bolt.Open(f.Name(), 0600, nil)
	if err != nil {
		s.FailNow("opening temporary database", "error: %s", err.Error())
		return
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(boltstore.DefaultBucketname))
		return err
	}); err != nil {
		panic(err)
	}
}

func (s *StoreSuite) TearDownTest() {
	defer func() { s.db = nil }()
	defer os.Remove(s.db.Path())

	if err := s.db.Close(); err != nil {
		s.FailNow("closing temporary database", "path: %s, error: %s", s.db.Path(), err.Error())
	}
	os.Remove(s.db.Path())
}

func (s *StoreSuite) TestNewStore() {
	testCases := []struct {
		desc      string
		db        *bolt.DB
		opts      []boltstore.SessionStoreOption
		isValidID func(string) bool
	}{
		{
			desc: "Static generator",
			db:   s.db,
			opts: []boltstore.SessionStoreOption{
				boltstore.IDGenerator(func(_ *http.Request) (string, error) { return "foo", nil }),
				boltstore.Keys([]byte("foo")),
			},
			isValidID: func(s string) bool { return s == "foo" },
		},
		{
			desc: "Default Generator",
			db:   s.db,
			opts: []boltstore.SessionStoreOption{
				boltstore.IDGenerator(boltstore.DefaultIDGenerator()),
				boltstore.Keys([]byte("foo")),
			},
			isValidID: func(s string) bool {
				id, err := uuid.FromString(s)
				return id.Version() == uuid.V4 && err == nil
			},
		},
		{
			desc: "Nil Generator",
			db:   s.db,
			opts: []boltstore.SessionStoreOption{boltstore.Keys([]byte("foo"))},
			isValidID: func(s string) bool {
				id, err := uuid.FromString(s)
				return id.Version() == uuid.V4 && err == nil
			},
		},
		{
			desc: "Custom bucket name",
			db:   s.db,
			opts: []boltstore.SessionStoreOption{
				boltstore.SessionBucket("customBucket"),
				boltstore.Keys([]byte("foo")),
			},
			isValidID: func(s string) bool {
				id, err := uuid.FromString(s)
				return id.Version() == uuid.V4 && err == nil
			},
		},
	}
	for _, tC := range testCases {
		s.T().Run(tC.desc, func(t *testing.T) {
			st, err := boltstore.New(tC.db, tC.opts...)
			assert.NoError(t, err, "creating store: %s", err)
			assert.NotNil(t, st, "creating store: store is nil")
			sess, err := st.New(nil, "sess")
			assert.True(t, tC.isValidID(sess.ID), "Session ID does not match")
		})
	}
}

func (suite *StoreSuite) TestLC() {
	testCases := []struct {
		desc           string
		flashes        []string
		values         map[interface{}]interface{}
		cookiename     string
		expectedToFail bool
	}{
		{
			desc:           "No Values",
			flashes:        []string{},
			values:         make(map[interface{}]interface{}),
			cookiename:     "testcookie",
			expectedToFail: false,
		},
		{
			desc:           "Values",
			flashes:        []string{},
			values:         map[interface{}]interface{}{"foo": "bar", "baz": 1.2},
			cookiename:     "testcookie",
			expectedToFail: false,
		},
		{
			desc:           "Values and Flashes",
			flashes:        []string{"foo", "bar", "baz"},
			values:         map[interface{}]interface{}{"foo": "bar", "baz": 1.2},
			cookiename:     "testcookie",
			expectedToFail: false,
		},
		{
			desc:           "No Cookie and no values",
			flashes:        []string{},
			values:         map[interface{}]interface{}{},
			cookiename:     "foo",
			expectedToFail: true,
		},
	}

	st, err := boltstore.New(suite.db, boltstore.Keys([]byte("foo")))
	assert.NoError(suite.T(), err, "creating new session store: %s", err)
	assert.NotNil(suite.T(), st, "session store is nil after creation")

	for _, tC := range testCases {
		suite.T().Run(tC.desc, func(t *testing.T) {
			first, _ := http.NewRequest(http.MethodGet, "http://localhost", nil)
			new, err := st.Get(first, "testcookie")
			assert.NoError(suite.T(), err, "error getting session")
			assert.NotNil(suite.T(), new, "session is nil")
			assert.True(suite.T(), new.IsNew, "session is old")

			for _, f := range tC.flashes {
				new.AddFlash(f)
			}
			new.Values = tC.values

			w := httptest.NewRecorder()
			assert.NoError(suite.T(), new.Save(first, w), "error while saving session")
			w.Flush()
			assert.Len(suite.T(), w.Result().Cookies(), 1, "more than one cookie set")
			assert.Equal(suite.T(), "testcookie", w.Result().Cookies()[0].Name)

			second := first.Clone(context.TODO())
			second.AddCookie(w.Result().Cookies()[0])

			restored, err := st.Get(second, tC.cookiename)
			if tC.expectedToFail {
				assert.NotEqual(suite.T(), restored.ID, new.ID)
				return
			}
			assert.EqualValues(suite.T(), tC.values, restored.Values, "values of restored session are not equal")
		})
	}
}
