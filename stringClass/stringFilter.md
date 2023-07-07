Package:
strings
func TrimLeftFunc(s string, f func(rune) bool) string
TrimLeftFunc returns a slice of the string s with all leading Unicode code points c satisfying f(c) removed.
 
Example
Code:
fmt.Print(strings.TrimLeftFunc("¡¡¡Hello, Gophers!!!", func(r rune) bool {     return !unicode.IsLetter(r) && !unicode.IsNumber(r) }))
Output:
Hello, Gophers!!



Package:
unicode
func IsSpace(r rune) bool
IsSpace reports whether the rune is a space character as defined by Unicode's White Space property; in the Latin-1 space this is
 '\t', '\n', '\v', '\f', '\r', ' ', U+0085 (NEL), U+00A0 (NBSP).
Other definitions of spacing characters are set by category Z and property Pattern_White_Space.
 
Example
Code:
fmt.Printf("%t\n", unicode.IsSpace(' ')) fmt.Printf("%t\n", unicode.IsSpace('\n')) fmt.Printf("%t\n", unicode.IsSpace('\t')) fmt.Printf("%t\n", unicode.IsSpace('a'))
Output:
true
true
true
false
