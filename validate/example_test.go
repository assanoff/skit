package validate_test

import (
	"fmt"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/validate"
)

type lineItem struct {
	Name string `json:"name" validate:"required"`
}

type createOrder struct {
	Customer string     `json:"customer" validate:"required,max=10"`
	Items    []lineItem `json:"items" validate:"min=1,dive"`
}

// Check reports every failed tag as a field of one InvalidArgument error,
// named as the client sends it.
func ExampleCheck() {
	err := validate.Check(createOrder{
		Customer: "a very long name",
		Items:    []lineItem{{Name: "pen"}, {}},
	})

	body, _, _ := errs.From(err).Encode()
	fmt.Println(errs.From(err).HTTPStatus())
	fmt.Println(string(body))
	fmt.Println(validate.Check(createOrder{Customer: "acme", Items: []lineItem{{Name: "pen"}}}))
	// Output:
	// 400
	// {"code":"invalid_argument","title":"data validation error","detail":"validation failed","fields":[{"field":"customer","error":"max"},{"field":"items[1].name","error":"required"}]}
	// <nil>
}
