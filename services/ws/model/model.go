package model

import "time"

type ModelGetter interface {
	GetModel() RssFeedProvidersAccessor
}

//noinspection GoSnakeCaseUsage
type RssFeedProvidersAccessor interface {
	InsertArticles(providerID string, articles []*RssFeedProviderArticle) (int, error)

	CrawledLatestArticleData(providerID, emptyOrBoardID string) (string, time.Time, error)
	UpdateCrawledLatestArticleID(providerID, emptyOrBoardID, crawledLatestArticleID string) error
}
