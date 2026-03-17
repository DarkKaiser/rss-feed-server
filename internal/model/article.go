package model

import (
	"fmt"
	"time"
)

// Article RSS Feed Provider의 게시글 정보
type Article struct {
	BoardID   string
	BoardName string
	BoardType string
	ArticleID string
	Title     string
	Content   string
	Link      string
	Author    string
	CreatedAt time.Time
}

func (a Article) String() string {
	return fmt.Sprintf("[%s, %s, %s, %s, %s, %s, %s, %s]", a.BoardID, a.BoardName, a.ArticleID, a.Title, a.Content, a.Link, a.Author, a.CreatedAt.Format("2006-01-02 15:04:05"))
}
