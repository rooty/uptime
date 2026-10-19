package handler

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
)

var (
	markdownHeading  = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	markdownBullet   = regexp.MustCompile(`^\s*[-*]\s+(.+)$`)
	markdownOrdered  = regexp.MustCompile(`^\s*\d+\.\s+(.+)$`)
	markdownLink     = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)\s]+)\)`)
	markdownStrong   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	markdownEmphasis = regexp.MustCompile(`\*([^*]+)\*`)
	markdownCode     = regexp.MustCompile("`([^`]+)`")
)

// renderMarkdown supports the runbook syntax while escaping all user HTML.
// It deliberately accepts links only to absolute http/https URLs.
func renderMarkdown(source string) string {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	var result strings.Builder
	inCode := false
	listType := ""
	closeList := func() {
		if listType != "" {
			result.WriteString("</" + listType + ">")
			listType = ""
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inCode {
				result.WriteString("</code></pre>")
				inCode = false
			} else {
				closeList()
				result.WriteString("<pre><code>")
				inCode = true
			}
			continue
		}
		if inCode {
			result.WriteString(html.EscapeString(line) + "\n")
			continue
		}
		if strings.TrimSpace(line) == "" {
			closeList()
			continue
		}
		if match := markdownHeading.FindStringSubmatch(line); match != nil {
			closeList()
			level := len(match[1])
			result.WriteString(fmt.Sprintf("<h%d>%s</h%d>", level, renderInline(match[2]), level))
			continue
		}
		if match := markdownBullet.FindStringSubmatch(line); match != nil {
			if listType != "ul" {
				closeList()
				result.WriteString("<ul>")
				listType = "ul"
			}
			result.WriteString("<li>" + renderInline(match[1]) + "</li>")
			continue
		}
		if match := markdownOrdered.FindStringSubmatch(line); match != nil {
			if listType != "ol" {
				closeList()
				result.WriteString("<ol>")
				listType = "ol"
			}
			result.WriteString("<li>" + renderInline(match[1]) + "</li>")
			continue
		}
		closeList()
		result.WriteString("<p>" + renderInline(line) + "</p>")
	}
	if inCode {
		result.WriteString("</code></pre>")
	}
	closeList()
	return result.String()
}

func renderInline(source string) string {
	value := html.EscapeString(source)
	value = markdownLink.ReplaceAllStringFunc(value, func(match string) string {
		parts := markdownLink.FindStringSubmatch(match)
		parsed, err := url.Parse(parts[2])
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return html.EscapeString(match)
		}
		return `<a href="` + html.EscapeString(parts[2]) + `" target="_blank" rel="noreferrer">` + parts[1] + `</a>`
	})
	value = markdownCode.ReplaceAllString(value, "<code>$1</code>")
	value = markdownStrong.ReplaceAllString(value, "<strong>$1</strong>")
	return markdownEmphasis.ReplaceAllString(value, "<em>$1</em>")
}
