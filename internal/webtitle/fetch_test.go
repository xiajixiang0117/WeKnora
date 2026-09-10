package webtitle

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
)

func TestSelectTitleUsesBrowserTitleBeforeGenericHeading(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`
		<html><head>
			<title>快速入门 - SiFli SDK编程指南 文档</title>
			<meta property="og:title" content="SDK 快速入门" />
		</head><body><h1>说明</h1></body></html>`))
	require.NoError(t, err)
	require.Equal(t, "快速入门 - SiFli SDK编程指南 文档", selectTitle(doc))
}

func TestSelectTitleFallsBackToOpenGraphThenHeading(t *testing.T) {
	ogDoc, err := goquery.NewDocumentFromReader(strings.NewReader(`
		<html><head><meta property="og:title" content="页面标题" /></head><body><h1>说明</h1></body></html>`))
	require.NoError(t, err)
	require.Equal(t, "页面标题", selectTitle(ogDoc))

	h1Doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body><h1>说明</h1></body></html>`))
	require.NoError(t, err)
	require.Equal(t, "说明", selectTitle(h1Doc))
}
