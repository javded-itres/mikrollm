package guard

import "regexp"

// Heuristics inspired by LiteLLM detect_prompt_injection (verbs × instructions),
// plus RU phrases. No LLM-as-judge: that would recurse through the gateway.
var injectionPhrases = []string{
	"ignore previous instructions",
	"ignore all previous",
	"ignore the above",
	"disregard previous",
	"forget your instructions",
	"forget previous instructions",
	"do anything now",
	"developer mode",
	"jailbreak",
	"dan mode",
	"bypass the rules",
	"override the system",
	"new system prompt",
	"pretend you have no restrictions",
	"you are now",
	"игнорируй предыдущие",
	"игнорируй все инструкции",
	"забудь инструкции",
	"забудь предыдущие",
	"режим разработчика",
	"без ограничений",
	"обходи правила",
	"новый системный промпт",
}

var piiRe = map[string]*regexp.Regexp{
	"email": regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
	"phone": regexp.MustCompile(`(?:\+?\d{1,3}[\s\-]?)?(?:\(?\d{3}\)?[\s\-]?)?\d{3}[\s\-]?\d{2}[\s\-]?\d{2,4}`),
	"card":  regexp.MustCompile(`\b(?:\d[ \-]*){13,19}\b`),
	"ip":    regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),
}
