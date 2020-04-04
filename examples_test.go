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
