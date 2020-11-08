package model

type ModelGetter interface {
	GetModel() RssFeedProvidersAccessor
}

//noinspection GoSnakeCaseUsage
type RssFeedProvidersAccessor interface {
	InsertArticles(providerID string, articles []*RssFeedProviderArticle) (int, error)

	// @@@@@
	CrawledLatestArticleID(providerID string) (int64, error)
	UpdateCrawledLatestArticleID(providerID string, crawledLatestArticleID int64) error
}
