package adapter

import "regexp"

// tokenLike matches what could be a credential in a vendor tool's text:
// an "Authorization: Bearer" value, an sk- key, a JWT, or any other run of
// 32 or more key characters. It errs towards hiding: a long request id is
// hidden too.
var tokenLike = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(bearer\s+)\S+`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]*)?`),
	regexp.MustCompile(`[A-Za-z0-9_+/=-]{32,}`),
}

// HideTokens replaces each token-like string in text that came from a
// vendor tool (a renewal failure from claude's stderr or codex's answer, an
// `opencode models` error line) with [redacted]: modelpick stores, warns
// with and shows that text.
func HideTokens(reason string) string {
	reason = tokenLike[0].ReplaceAllString(reason, "${1}[redacted]")
	for _, re := range tokenLike[1:] {
		reason = re.ReplaceAllString(reason, "[redacted]")
	}
	return reason
}
