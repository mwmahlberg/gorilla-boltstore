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
	"fmt"
	"net/http"

	boltstore "github.com/mwmahlberg/gorilla-boltstore"
	"go.etcd.io/bbolt"
)

func ExampleIDGeneratorFunc(db *bbolt.DB) {
	gen := boltstore.IDGeneratorFunc(func(_ *http.Request) (string, error) {
		return "foo", nil
	})
	id, _ := gen(nil)
	fmt.Println(id)
	//Output:
	// foo
}
