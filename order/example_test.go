package order_test

import (
	"fmt"

	"github.com/assanoff/skit/order"
)

// Parse reads untrusted "field,DIRECTION" input against an allowlist: an
// empty value gives the default, a field outside the list is an error.
func ExampleParse() {
	sortable := map[string]string{"created_at": "created_at", "name": "name"}
	def := order.NewBy("created_at", order.DESC)

	for _, in := range []string{"name,DESC", "name", "", "password,ASC"} {
		by, err := order.Parse(sortable, in, def)
		fmt.Printf("%q: %+v %v\n", in, by, err)
	}
	// Output:
	// "name,DESC": {Field:name Direction:DESC} <nil>
	// "name": {Field:name Direction:ASC} <nil>
	// "": {Field:created_at Direction:DESC} <nil>
	// "password,ASC": {Field: Direction:} order: unknown field "password"
}
