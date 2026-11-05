package sprint

import "strings"

// CollectVerdict is a report's verdict (the first word of its first Verdict: line, upper
// case, markdown and punctuation trimmed; "" for none) and its head (the first word of its
// first Head: line, lower case): a work card's report as a finish records it (isHold).
func CollectVerdict(report string) (verdict, head string) {
	var vok, hok bool
	for _, l := range strings.Split(report, "\n") {
		key, val := collectKey(l)
		word, _, _ := strings.Cut(strings.TrimSpace(strings.TrimLeft(val, "*_ \t")), " ")
		switch key {
		case "verdict":
			if !vok {
				verdict, vok = strings.ToUpper(strings.Trim(word, "*_.,;:!()")), true
			}
		case "head":
			if !hok {
				head, hok = strings.ToLower(strings.Trim(word, "*_`.,;:")), true
			}
		}
	}
	return verdict, head
}

func collectKey(line string) (key, val string) {
	key, val, ok := strings.Cut(strings.TrimLeft(line, "#*-_ \t"), ":")
	if !ok {
		return "", ""
	}
	return strings.ToLower(strings.Trim(key, "*_ \t")), val
}
