package inflect

//Helpers is a map of the helper names with its corresponding inflect function
var Helpers = map[string]interface{}{
	"asciffy":             Asciify,
	"camelize":            Camelize,
	"camelize_down_first": CamelizeDownFirst,
	"capitalize":          Capitalize,
	"dasherize":           Dasherize,
	"humanize":            Humanize,
	"ordinalize":          Ordinalize,
	"parameterize":        Parameterize,
	"pluralize":           Pluralize,
	"pluralize_with_size": PluralizeWithSize,
	"singularize":         Singularize,
	"tableize":            Tableize,
	"typeify":             Typeify,
	"underscore":          Underscore,
}
