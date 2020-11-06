package model

import (
	"database/sql"
	"fmt"
	"github.com/darkkaiser/rss-feed-server/g"
	"github.com/darkkaiser/rss-feed-server/notifyapi"
	"github.com/darkkaiser/rss-feed-server/utils"
	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"strings"
	"time"
)

const (
	RssProviderModel ModelType = "rss_provider_model"
)

type RssProviderArticle struct {
	BoardID   string
	BoardName string
	ArticleID string
	Title     string
	Content   string
	Link      string
	Author    string
	CreatedAt time.Time
}

func (a RssProviderArticle) String() string {
	return fmt.Sprintf("[%s, %s, %s, %s, %s, %s, %s, %s]", a.BoardID, a.BoardName, a.ArticleID, a.Title, a.Content, a.Link, a.Author, a.CreatedAt.Format("2006-10-02 15:04:05"))
}

type RssProvider struct {
	db *sql.DB

	// RssProvider 모델에서 RSS Feed 서비스 지원이 가능한 사이트 목록
	rssFeedSupportedSites []string
}

func NewRssProvider(config *g.AppConfig, db *sql.DB) *RssProvider {
	p := &RssProvider{
		db: db,

		rssFeedSupportedSites: []string{g.RssFeedSupportedSiteNaverCafe},
	}

	if err := p.init(config); err != nil {
		m := "RSS Feed DB를 초기화하는 중에 치명적인 오류가 발생하였습니다."

		notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)

		log.Panicf("%s (error:%s)", m, err)
	}

	return p
}

func (p *RssProvider) init(config *g.AppConfig) error {
	if err := p.createTables(); err != nil {
		return err
	}

	// @@@@@ 검토
	////////////////////////
	for _, c := range config.RssFeed.Providers {
		// 기초 데이터를 추가한다.
		if err := p.insertRssProvider(c.ID, c.Config.ID, c.Config.Name, c.Config.Description, c.Config.Url); err != nil {
			return err
		}

		for _, b := range c.Config.Boards {
			if err := p.insertRssProviderBoard(c.ID, b.ID, b.Name); err != nil {
				return err
			}
		}

		// 일정 시간이 지난 게시글 자료를 모두 삭제한다.
		if err := p.deleteOutOfDateArticle(c.ID, c.Config.ArticleArchiveDate); err != nil {
			return err
		}
	}
	////////////////////////

	return nil
}

//noinspection GoUnhandledErrorResult
func (p *RssProvider) createTables() error {
	//
	// rss_provider 테이블
	//
	stmt1, err := p.db.Prepare(`
		CREATE TABLE IF NOT EXISTS rss_provider (
			id 					VARCHAR( 50) PRIMARY KEY NOT NULL UNIQUE,
			s_id 				VARCHAR( 50) NOT NULL,
			s_name 				VARCHAR(130) NOT NULL,
			s_description 		VARCHAR(200),
			s_url 				VARCHAR(100) NOT NULL
		)
	`)
	if err != nil {
		return err
	}
	defer stmt1.Close()
	if _, err = stmt1.Exec(); err != nil {
		return err
	}

	stmt2, err := p.db.Prepare(`
		CREATE INDEX IF NOT EXISTS rss_provider_index01 ON rss_provider(s_id)
	`)
	if err != nil {
		return err
	}
	defer stmt2.Close()
	if _, err = stmt2.Exec(); err != nil {
		return err
	}

	//
	// rss_provider_board 테이블
	//
	stmt3, err := p.db.Prepare(`
		CREATE TABLE IF NOT EXISTS rss_provider_board (
			p_id 		VARCHAR( 50) NOT NULL,
			id			VARCHAR( 50) NOT NULL,
			name 		VARCHAR(130) NOT NULL,
			PRIMARY KEY (p_id, id)
			FOREIGN KEY (p_id) REFERENCES rss_provider(id)
		)
	`)
	if err != nil {
		return err
	}
	defer stmt3.Close()
	if _, err = stmt3.Exec(); err != nil {
		return err
	}

	stmt4, err := p.db.Prepare(`
		CREATE INDEX IF NOT EXISTS rss_provider_board_index01 ON rss_provider_board(p_id)
	`)
	if err != nil {
		return err
	}
	defer stmt4.Close()
	if _, err = stmt4.Exec(); err != nil {
		return err
	}

	//
	// rss_provider_article 테이블
	//
	stmt5, err := p.db.Prepare(`
		CREATE TABLE IF NOT EXISTS rss_provider_article (
			p_id 			VARCHAR( 50) NOT NULL,
			b_id 			VARCHAR( 50) NOT NULL,
			id 				VARCHAR( 50) NOT NULL,
			title 			VARCHAR(400) NOT NULL,
			content			TEXT,
			link 			VARCHAR(1000) NOT NULL,
			author 			VARCHAR(50),
			created_date	DATETIME,
			PRIMARY KEY (p_id, b_id, id)
			FOREIGN KEY (p_id) REFERENCES rss_provider(id)
			FOREIGN KEY (b_id) REFERENCES rss_provider_board(id)
		)
	`)
	if err != nil {
		return err
	}
	defer stmt5.Close()
	if _, err = stmt5.Exec(); err != nil {
		return err
	}

	stmt6, err := p.db.Prepare(`
		CREATE INDEX IF NOT EXISTS rss_provider_article_index01 ON rss_provider_article(created_date)
	`)
	if err != nil {
		return err
	}
	defer stmt6.Close()
	if _, err = stmt6.Exec(); err != nil {
		return err
	}

	return nil
}

// @@@@@ 검토
//noinspection GoUnhandledErrorResult
func (p *RssProvider) insertRssProvider(id, siteId, siteName, siteDescription, siteUrl string) error {
	stmt, err := p.db.Prepare(`
		INSERT OR REPLACE
		  INTO rss_provider (id, s_id, s_name, s_description, s_url) 
	    VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(id, siteId, siteName, siteDescription, siteUrl); err != nil {
		return err
	}

	return nil
}

// @@@@@ 검토
//noinspection GoUnhandledErrorResult
func (p *RssProvider) insertRssProviderBoard(providerID, id, name string) error {
	stmt, err := p.db.Prepare("INSERT OR REPLACE INTO rss_provider_board (p_id, id, name) VALUES (?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(providerID, id, name); err != nil {
		return err
	}

	return nil
}

// @@@@@
//noinspection GoUnhandledErrorResult
func (p *RssProvider) CrawledLatestArticleID(cafeID string) (int64, error) {
	var crawledLatestArticleID int64
	err := p.db.QueryRow(`
		SELECT MAX(articleId) 
		  FROM (    SELECT IFNULL(crawledLatestArticleId, 0) articleId
		              FROM naver_cafe_info
		             WHERE cafeId = ?
                 UNION ALL
                    SELECT IFNULL(MAX(articleId), 0) articleId
		              FROM naver_cafe_article
		             WHERE cafeId = ?
		       )
	`, cafeID, cafeID).Scan(&crawledLatestArticleID)

	if err != nil {
		return 0, err
	}

	return crawledLatestArticleID, nil
}

// @@@@@
//noinspection GoUnhandledErrorResult
func (p *RssProvider) UpdateCrawledLatestArticleID(cafeID string, articleID int64) error {
	stmt, err := p.db.Prepare("UPDATE naver_cafe_info SET crawledLatestArticleId = ? WHERE cafeId = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(articleID, cafeID); err != nil {
		return err
	}

	return nil
}

// @@@@@
//noinspection GoUnhandledErrorResult
func (p *RssProvider) InsertArticles(cafeID string, articles []*RssProviderArticle) (int, error) {
	stmt, err := p.db.Prepare(`
		INSERT OR REPLACE
		  INTO naver_cafe_article (cafeId, boardId, articleId, title, content, link, author, createdAt)
	    VALUES (?, ?, ?, ?, ?, ?, ?, datetime(?))
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var insertedCnt int
	var sentNotifyMessage = false
	for _, article := range articles {
		if _, err := stmt.Exec(cafeID, article.BoardID, article.ArticleID, article.Title, article.Content, article.Link, article.Author, article.CreatedAt.UTC().Format("2006-01-02 15:04:05")); err != nil {
			m := fmt.Sprintf("네이버 카페('%s > %s')의 게시글 등록이 실패하였습니다.", cafeID, article.BoardName)

			log.Errorf("%s (게시글정보:%s) (error:%s)", m, article, err)

			// 너무 많은 알림 메시지가 발송될 수 있으므로, 동시에 입력되는 게시글 중 최초 오류건에 대해서만 알림 메시지를 보낸다.
			if sentNotifyMessage == false {
				sentNotifyMessage = true
				notifyapi.SendNotifyMessage(fmt.Sprintf("%s\r\n\r\n%s", m, err), true)
			}
		} else {
			insertedCnt += 1
		}
	}

	return insertedCnt, nil
}

// @@@@@ 검토, order 컬럼 추가 필요(createAt으로는 안되나?? 안될거같긴함)
//noinspection GoUnhandledErrorResult
func (p *RssProvider) Articles(pID string, boardIDs []string, maxArticleCount uint) ([]*RssProviderArticle, error) {
	stmt, err := p.db.Prepare(fmt.Sprintf(`
		SELECT a.b_id
             , b.name boardName
		     , a.id
		     , a.title
		     , IFNULL(a.content, "") content
		     , a.link
		     , IFNULL(a.author, "") author
		     , a.createdAt
		  FROM naver_cafe_article a
               INNER JOIN rss_provider_board b ON ( a.p_id = b.p_id AND a.b_id = b.id )
		 WHERE a.p_id = ?
		   AND a.boardId IN (%s)
      ORDER BY a.id DESC
         LIMIT ?
	`, fmt.Sprintf("'%s'", strings.Join(boardIDs, "', '"))))
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(pID, maxArticleCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	articles := make([]*RssProviderArticle, 0)

	for rows.Next() {
		var article RssProviderArticle

		var createdAt sql.NullTime
		if err = rows.Scan(&article.BoardID, &article.BoardName, &article.ArticleID, &article.Title, &article.Content, &article.Link, &article.Author, &createdAt); err != nil {
			return nil, err
		}
		if createdAt.Valid == true {
			article.CreatedAt = createdAt.Time.Local()
		}

		articles = append(articles, &article)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return articles, nil
}

// @@@@@ 검토
//noinspection GoUnhandledErrorResult
func (p *RssProvider) deleteOutOfDateArticle(id string, articleArchiveDate uint) error {
	stmt, err := p.db.Prepare(fmt.Sprintf(`
		DELETE 
		  FROM rss_provider_article
		 WHERE p_id = ?
		   AND createdAt < date(datetime('now', 'utc'), '-%d days')
	`, articleArchiveDate))
	if err != nil {
		return err
	}
	defer stmt.Close()
	if _, err = stmt.Exec(id); err != nil {
		return err
	}

	return nil
}

func (p *RssProvider) RssFeedSupportedSite(site string) bool {
	return utils.Contains(p.rssFeedSupportedSites, site)
}
