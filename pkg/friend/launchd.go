package friend

import (
	"encoding/xml"
	"strings"
)

// PlistArgs is the ProgramArguments array of a launchd plist, in order; nil
// when the plist has none or cannot be read as XML.
func PlistArgs(plist string) []string {
	dec := xml.NewDecoder(strings.NewReader(plist))
	dec.Strict = false
	var args []string
	key, inArray, found := "", false, false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			if end, ok := tok.(xml.EndElement); ok && end.Name.Local == "array" && inArray {
				return args
			}
			continue
		}
		switch el.Name.Local {
		case "key":
			var k string
			if dec.DecodeElement(&k, &el) == nil {
				key = strings.TrimSpace(k)
			}
		case "array":
			inArray = key == "ProgramArguments"
			found = found || inArray
		case "string":
			var v string
			if dec.DecodeElement(&v, &el) == nil && inArray {
				args = append(args, v)
			}
		}
	}
	if !found {
		return nil
	}
	return args
}
