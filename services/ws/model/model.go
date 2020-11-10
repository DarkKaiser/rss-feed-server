package model

import "time"

type ModelGetter interface {
	GetModel() RssFeedProvidersAccessor
}

//noinspection GoSnakeCaseUsage
type RssFeedProvidersAccessor interface {
	InsertArticles(providerID string, articles []*RssFeedProviderArticle) (int, error)

	LatestCrawledArticleData(providerID, emptyOrBoardID string) (string, time.Time, error)
	UpdateLatestCrawledArticleID(providerID, emptyOrBoardID, latestCrawledArticleID string) error
}
