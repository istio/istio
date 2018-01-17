// Package validator implements value validations
//
// Copyright 2014 Roberto Teixeira <robteix@robteix.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/*
Package validator implements value validations based on struct tags.

In code it is often necessary to validate that a given value is valid before
using it for something. A typical example might be something like this.

	if age < 18 {
		return error.New("age cannot be under 18")
	}

This is a simple enough example, but it can get significantly more complex,
especially when dealing with structs.

	l := len(strings.Trim(s.Username))
	if l < 3 || l > 40  || !regexp.MatchString("^[a-zA-Z]$", s.Username) ||	s.Age < 18 || s.Password {
		return errors.New("Invalid request")
	}

You get the idea. Package validator allows one to define valid values as
struct tags when defining a new struct type.

	type NewUserRequest struct {
		Username string `validate:"min=3,max=40,regexp=^[a-zA-Z]*$"`
		Name string     `validate:"nonzero"`
		Age int         `validate:"min=18"`
		Password string `validate:"min=8"`
	}

Then validating a variable of type NewUserRequest becomes trivial.

	nur := NewUserRequest{Username: "something", ...}
	if errs := validator.Validate(nur); errs != nil {
		// do something
	}

Builtin validator functions

Here is the list of validator functions builtin in the package.

	len
		For numeric numbers, len will simply make sure that the value is
		equal to the parameter given. For strings, it checks that
		the string length is exactly that number of characters. For slices,
		arrays, and maps, validates the number of items. (Usage: len=10)

	max
		For numeric numbers, max will simply make sure that the value is
		lesser or equal to the parameter given. For strings, it checks that
		the string length is at most that number of characters. For slices,
		arrays, and maps, validates the number of items. (Usage: max=10)

	min
		For numeric numbers, min will simply make sure that the value is
		greater or equal to the parameter given. For strings, it checks that
		the string length is at least that number of characters. For slices,
		arrays, and maps, validates the number of items. (Usage: min=10)

	nonzero
		This validates that the value is not zero. The appropriate zero value
		is given by the Go spec (e.g. for int it's 0, for string it's "", for
		pointers is nil, etc.) Usage: nonzero

	regexp
		Only valid for string types, it will validate that the value matches
		the regular expression provided as parameter. (Usage: regexp=^a.*b$)


Note that there are no tests to prevent conflicting validator parameters. For
instance, these fields will never be valid.

	...
	A int     `validate:"max=0,min=1"`
	B string  `validate:"len=10,regexp=^$"
	...

Custom validation functions

It is possible to define custom validation functions by using SetValidationFunc.
First, one needs to create a validation function.

	// Very simple validation func
	func notZZ(v interface{}, param string) error {
		st := reflect.ValueOf(v)
		if st.Kind() != reflect.String {
			return validate.ErrUnsupported
		}
		if st.String() == "ZZ" {
			return errors.New("value cannot be ZZ")
		}
		return nil
	}

Then one needs to add it to the list of validation funcs and give it a "tag" name.

	validate.SetValidationFunc("notzz", notZZ)

Then it is possible to use the notzz validation tag. This will print
"Field A error: value cannot be ZZ"

	type T struct {
		A string  `validate:"nonzero,notzz"`
	}
	t := T{"ZZ"}
	if errs := validator.Validate(t); errs != nil {
		fmt.Printf("Field A error: %s\n", errs["A"][0])
	}

To use parameters, it is very similar.

	// Very simple validator with parameter
	func notSomething(v interface{}, param string) error {
		st := reflect.ValueOf(v)
		if st.Kind() != reflect.String {
			return validate.ErrUnsupported
		}
		if st.String() == param {
			return errors.New("value cannot be " + param)
		}
		return nil
	}

And then the code below should print "Field A error: value cannot be ABC".

	validator.SetValidationFunc("notsomething", notSomething)
	type T struct {
		A string  `validate:"notsomething=ABC"`
	}
	t := T{"ABC"}
	if errs := validator.Validate(t); errs != nil {
		fmt.Printf("Field A error: %s\n", errs["A"][0])
	}

As well, it is possible to overwrite builtin validation functions.

	validate.SetValidationFunc("min", myMinFunc)

And you can delete a validation function by setting it to nil.

	validate.SetValidationFunc("notzz", nil)
	validate.SetValidationFunc("nonzero", nil)

Using a non-existing validation func in a field tag will always return
false and with error validate.ErrUnknownTag.

Finally, package validator also provides a helper function that can be used
to validate simple variables/values.

    	// errs: nil
	errs = validator.Valid(42, "min=10, max=50")

	// errs: [validate.ErrZeroValue]
	errs = validator.Valid(nil, "nonzero")

	// errs: [validate.ErrMin,validate.ErrMax]
	errs = validator.Valid("hi", "nonzero,min=3,max=2")

Custom tag name

In case there is a reason why one would not wish to use tag 'validate' (maybe due to
a conflict with a different package), it is possible to tell the package to use
a different tag.

	validator.SetTag("valid")

Then.

	Type T struct {
		A int    `valid:"min=8, max=10"`
		B string `valid:"nonzero"`
	}

SetTag is permanent. The new tag name will be used until it is again changed
with a new call to SetTag. A way to temporarily use a different tag exists.

	validator.WithTag("foo").Validate(t)
	validator.WithTag("bar").Validate(t)
	// But this will go back to using 'validate'
	validator.Validate(t)

Multiple validators

You may often need to have a different set of validation
rules for different situations. In all the examples above,
we only used the default validator but you could create a
new one and set specific rules for it.

For instance, you might use the same struct to decode incoming JSON for a REST API
but your needs will change when you're using it to, say, create a new instance
in storage vs. when you need to change something.

	type User struct {
		Username string `validate:"nonzero"`
		Name string     `validate:"nonzero"`
		Age int         `validate:"nonzero"`
		Password string `validate:"nonzero"`
	}

Maybe when creating a new user, you need to make sure all values in the struct are filled,
but then you use the same struct to handle incoming requests to, say, change the password,
in which case you only need the Username and the Password and don't care for the others.
You might use two different validators.

	type User struct {
		Username string `creating:"nonzero" chgpw:"nonzero"`
		Name string     `creating:"nonzero"`
		Age int         `creating:"nonzero"`
		Password string `creating:"nonzero" chgpw:"nonzero"`
	}

	var (
		creationValidator = validator.NewValidator()
		chgPwValidator = validator.NewValidator()
	)

	func init() {
		creationValidator.SetTag("creating")
		chgPwValidator.SetTag("chgpw")
	}

	...

	func CreateUserHandler(w http.ResponseWriter, r *http.Request) {
		var u User
		json.NewDecoder(r.Body).Decode(&user)
		if errs := creationValidator.Validate(user); errs != nil {
			// the request did not include all of the User
			// struct fields, so send a http.StatusBadRequest
			// back or something
		}
		// create the new user
	}

	func SetNewUserPasswordHandler(w http.ResponseWriter, r *http.Request) {
		var u User
		json.NewDecoder(r.Body).Decode(&user)
		if errs := chgPwValidator.Validate(user); errs != nil {
			// the request did not Username and Password,
			// so send a http.StatusBadRequest
			// back or something
		}
		// save the new password
	}

It is also possible to do all of that using only the default validator as long
as SetTag is always called before calling validator.Validate() or you chain the
with WithTag().

*/
package validator
