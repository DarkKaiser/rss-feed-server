package handler

import (
	"github.com/darkkaiser/rss-feed-server/internal/config"
	"github.com/darkkaiser/rss-feed-server/internal/model"
)

type Handler struct {
	config *config.AppConfig

	rssFeedProviderStore *model.RssFeedProviderStore
}

func NewHandler(config *config.AppConfig, rssFeedProviderStore *model.RssFeedProviderStore) *Handler {
	return &Handler{
		config: config,

		rssFeedProviderStore: rssFeedProviderStore,
	}
}
