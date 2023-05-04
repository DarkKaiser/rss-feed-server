package model

import "time"

//goland:noinspection GoNameStartsWithPackageName
type ModelGetter interface {
	GetModel() RssFeedProvidersAccessor
}

//noinspection GoSnakeCaseUsage
type RssFeedProvidersAccessor interface {
	InsertArticles(providerID string, articles []*RssFeedProviderArticle) (int, error)

	LatestCrawledInfo(providerID, emptyOrBoardID string) (string, time.Time, error)
	UpdateLatestCrawledArticleID(providerID, emptyOrBoardID, latestCrawledArticleID string) error
}
