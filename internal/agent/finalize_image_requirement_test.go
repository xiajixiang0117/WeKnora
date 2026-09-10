package agent

import (
	"strings"
	"testing"
)

func TestFinalAnswerImageRequirement(t *testing.T) {
	if got := finalAnswerImageRequirement(false); got != "" {
		t.Fatalf("text-only tool results should not add an image requirement: %q", got)
	}

	got := finalAnswerImageRequirement(true)
	for _, required := range []string{
		"MUST include at least one relevant Markdown image",
		"Preserve its complete URL exactly",
		"ASCII half-width parentheses",
		"silently verify",
	} {
		if !strings.Contains(got, required) {
			t.Fatalf("expected %q in final-answer image requirement:\n%s", required, got)
		}
	}
}

func TestFinalAnswerCitationRequirement(t *testing.T) {
	if got := finalAnswerCitationRequirement(false); got != "" {
		t.Fatalf("citations disabled should not add a citation requirement: %q", got)
	}

	got := finalAnswerCitationRequirement(true)
	for _, required := range []string{
		"MUST cite every material claim",
		"<ref id=\"cN\"/>",
		"same line as the claim",
		"Do not group citations at the end",
		"silently verify",
	} {
		if !strings.Contains(got, required) {
			t.Fatalf("expected %q in final-answer citation requirement:\n%s", required, got)
		}
	}
}

func TestFinalAnswerPresentationRequirement(t *testing.T) {
	got := finalAnswerPresentationRequirement
	for _, required := range []string{
		"clear, natural language",
		"Headings and lists are optional",
		"Do not force a fixed section template",
	} {
		if !strings.Contains(got, required) {
			t.Fatalf("expected %q in final-answer presentation requirement:\n%s", required, got)
		}
	}
	if strings.Contains(got, "structured format") {
		t.Fatalf("presentation requirement must not force a structured format:\n%s", got)
	}
}
