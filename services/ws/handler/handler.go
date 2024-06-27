package handler

import (
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/model"
)

type Handler struct {
	config *g.AppConfig

	rssFeedProviderStore *model.RssFeedProviderStore
}

func NewHandler(config *g.AppConfig, rssFeedProviderStore *model.RssFeedProviderStore) *Handler {
	return &Handler{
		config: config,

		rssFeedProviderStore: rssFeedProviderStore,
	}
}
