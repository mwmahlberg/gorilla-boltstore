/*
 *  Copyright 2020 Markus W Mahlberg
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 *  This is a license template.
 */

package boltstore_test

import (
	"context"
	"io/ioutil"
	"math/rand"
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

func (suite *StoreSuite) SetupTest() {
	f, err := ioutil.TempFile("", "boltsessionstore.*.db")
	if err != nil {
		suite.FailNow("creating temporary database file", "error: %s", err.Error())
		return
	}
	f.Close()
	suite.db, err = bolt.Open(f.Name(), 0600, nil)
	if err != nil {
		suite.FailNow("opening temporary database", "error: %s", err.Error())
		return
	}
	if err := suite.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(boltstore.DefaultBucketname))
		return err
	}); err != nil {
		panic(err)
	}
}

func (suite *StoreSuite) TearDownTest() {
	defer func() { suite.db = nil }()
	defer os.Remove(suite.db.Path())

	if err := suite.db.Close(); err != nil {
		suite.FailNow("closing temporary database", "path: %s, error: %s", suite.db.Path(), err.Error())
	}
	os.Remove(suite.db.Path())
}

func (suite *StoreSuite) TestNewStore() {

	hash := make([]byte, 64)
	key := make([]byte, 64)

	for _, b := range [][]byte{hash, key} {
		n, err := rand.Read(b)
		assert.NoError(suite.T(), err)
		assert.Equal(suite.T(), 64, n)
	}

	testCases := []struct {
		desc      string
		db        *bolt.DB
		opts      []boltstore.SessionStoreOption
		isValidID func(string) bool
	}{
		{
			desc: "Static generator",
			db:   suite.db,
			opts: []boltstore.SessionStoreOption{
				boltstore.IDGenerator(func(_ *http.Request) (string, error) { return "foo", nil }),
				boltstore.Keys(hash, key),
			},
			isValidID: func(s string) bool { return s == "foo" },
		},
		{
			desc: "Default Generator",
			db:   suite.db,
			opts: []boltstore.SessionStoreOption{
				boltstore.IDGenerator(boltstore.DefaultIDGenerator()),
				boltstore.Keys(hash, key),
			},
			isValidID: func(s string) bool {
				id, err := uuid.FromString(s)
				return id.Version() == uuid.V4 && err == nil
			},
		},
		{
			desc: "Nil Generator",
			db:   suite.db,
			opts: []boltstore.SessionStoreOption{boltstore.Keys(hash, key)},
			isValidID: func(s string) bool {
				id, err := uuid.FromString(s)
				return id.Version() == uuid.V4 && err == nil
			},
		},
		{
			desc: "Custom bucket name",
			db:   suite.db,
			opts: []boltstore.SessionStoreOption{
				boltstore.SessionBucket("customBucket"),
				boltstore.Keys(hash, key),
			},
			isValidID: func(s string) bool {
				id, err := uuid.FromString(s)
				return id.Version() == uuid.V4 && err == nil
			},
		},
	}
	for _, tC := range testCases {
		suite.T().Run(tC.desc, func(t *testing.T) {
			st, err := boltstore.New(tC.db, tC.opts...)
			assert.NoError(t, err, "creating store: %s", err)
			assert.NotNil(t, st, "creating store: store is nil")
			sess, err := st.New(nil, "sess")
			assert.NoError(t, err, "error retrieving session from store")
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
			assert.NoError(suite.T(), err)
			assert.EqualValues(suite.T(), tC.values, restored.Values, "values of restored session are not equal")
		})
	}
}
