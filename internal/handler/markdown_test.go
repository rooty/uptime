package handler

import (
	"strings"
	"testing"

	"snippetbook/internal/model"
)

func TestRenderMarkdownParsesAndEscapesRunbookContent(t *testing.T) {
	rendered := renderMarkdown("# Health\n\n- **Check** `endpoint`\n\n[docs](https://example.com)\n<script>alert(1)</script>")
	for _, expected := range []string{"<h1>Health</h1>", "<ul>", "<strong>Check</strong>", "<code>endpoint</code>", "href=\"https://example.com\"", "&lt;script&gt;"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("rendered Markdown does not contain %q: %s", expected, rendered)
		}
	}
	if strings.Contains(rendered, "<script>") {
		t.Fatalf("rendered Markdown contains executable script: %s", rendered)
	}
}

func TestRenderItemContentRendersMarkdownButNotSecrets(t *testing.T) {
	item := model.Item{Kind: model.KindNote, Content: "# Note\n\n**important**"}
	renderItemContent(&item)
	if !strings.Contains(item.RenderedHTML, "<h1>Note</h1>") ||
		!strings.Contains(item.RenderedHTML, "<strong>important</strong>") {
		t.Fatalf("item Markdown was not rendered: %s", item.RenderedHTML)
	}

	secret := model.Item{Kind: model.KindSecret, Content: "do not render"}
	renderItemContent(&secret)
	if secret.RenderedHTML != "" {
		t.Fatalf("secret content was rendered: %s", secret.RenderedHTML)
	}
}
