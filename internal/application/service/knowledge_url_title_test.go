package service

import "testing"

func TestShouldRefreshURLKnowledgeTitle(t *testing.T) {
	const sourceURL = "https://docs.example.com/quickstart/index.html"

	tests := []struct {
		name  string
		title string
		want  bool
	}{
		{name: "empty", want: true},
		{name: "source URL", title: sourceURL, want: true},
		{name: "generated markdown name", title: "快速入门.md", want: true},
		{name: "case insensitive markdown extension", title: "QuickStart.MD", want: true},
		{name: "user title", title: "快速入门 - SiFli SDK编程指南", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldRefreshURLKnowledgeTitle(test.title, sourceURL); got != test.want {
				t.Fatalf("shouldRefreshURLKnowledgeTitle(%q) = %v, want %v", test.title, got, test.want)
			}
		})
	}
}
