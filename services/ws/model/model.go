package model

type ModelGetter interface {
	GetModel() interface{}
}

// @@@@@
//noinspection GoSnakeCaseUsage
type NaverCafe_RssFeedProvidersAccessor interface {
	InsertArticles(providerID string, articles []*RssFeedProviderArticle) (int, error)
	NaverCafe_CrawledLatestArticleID(providerID string) (int64, error)
	NaverCafe_UpdateCrawledLatestArticleID(providerID string, crawledLatestArticleID int64) error
}
