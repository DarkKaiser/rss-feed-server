package feeds

// rss support
// validation done according to spec here:
//    http://cyber.law.harvard.edu/rss/rss.html

import (
	"encoding/xml"
	"time"
)

type CDATA string

func (c CDATA) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	return e.EncodeElement(CDATA2{string(c)}, start)
}

type CDATA2 struct {
	Text string `xml:",cdata"`
}

// RssFeedXml private wrapper around the RssFeed which gives us the <rss>...</rss> xml
type RssFeedXml struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel *RssFeed
}

type RssFeed struct {
	XMLName        xml.Name `xml:"channel"`
	Title          CDATA    `xml:"title"`       // required
	Link           string   `xml:"link"`        // required
	Description    CDATA    `xml:"description"` // required
	Language       string   `xml:"language,omitempty"`
	Copyright      string   `xml:"copyright,omitempty"`
	ManagingEditor string   `xml:"managingEditor,omitempty"` // Author used
	WebMaster      string   `xml:"webMaster,omitempty"`
	PubDate        string   `xml:"pubDate,omitempty"`       // created or updated
	LastBuildDate  string   `xml:"lastBuildDate,omitempty"` // updated used
	Category       string   `xml:"category,omitempty"`
	Generator      string   `xml:"generator,omitempty"`
	Docs           string   `xml:"docs,omitempty"`
	Cloud          string   `xml:"cloud,omitempty"`
	Ttl            int      `xml:"ttl,omitempty"`
	Rating         string   `xml:"rating,omitempty"`
	SkipHours      string   `xml:"skipHours,omitempty"`
	SkipDays       string   `xml:"skipDays,omitempty"`
	Image          *RssImage
	TextInput      *RssTextInput
	Items          []*RssItem `xml:"item"`
}

// FeedXml returns an XML-ready object for an RssFeed object
func (r *RssFeed) FeedXml() interface{} {
	return &RssFeedXml{
		Version: "2.0",
		Channel: r,
	}
}

type RssItem struct {
	XMLName     xml.Name `xml:"item"`
	Title       CDATA    `xml:"title"`       // required
	Link        string   `xml:"link"`        // required
	Description CDATA    `xml:"description"` // required
	Content     *RssContent
	Author      CDATA  `xml:"author,omitempty"`
	Category    CDATA  `xml:"category,omitempty"`
	Comments    string `xml:"comments,omitempty"`
	Enclosure   *RssEnclosure
	Guid        string `xml:"guid,omitempty"`    // ID used
	PubDate     string `xml:"pubDate,omitempty"` // created or updated
	Source      string `xml:"source,omitempty"`
}

type RssContent struct {
	XMLName xml.Name `xml:"content:encoded"`
	Content string   `xml:",cdata"`
}

type RssImage struct {
	XMLName xml.Name `xml:"image"`
	Url     string   `xml:"url"`
	Title   string   `xml:"title"`
	Link    string   `xml:"link"`
	Width   int      `xml:"width,omitempty"`
	Height  int      `xml:"height,omitempty"`
}

type RssTextInput struct {
	XMLName     xml.Name `xml:"textInput"`
	Title       string   `xml:"title"`
	Description string   `xml:"description"`
	Name        string   `xml:"name"`
	Link        string   `xml:"link"`
}

type RssEnclosure struct {
	//RSS 2.0 <enclosure url="http://example.com/file.mp3" length="123456789" type="audio/mpeg" />
	XMLName xml.Name `xml:"enclosure"`
	Url     string   `xml:"url,attr"`
	Length  string   `xml:"length,attr"`
	Type    string   `xml:"type,attr"`
}

// returns the first non-zero time formatted as a string or ""
func anyTimeFormat(format string, times ...time.Time) string {
	for _, t := range times {
		if !t.IsZero() {
			return t.Format(format)
		}
	}
	return ""
}

func NewRssFeed(title, link, description, language, generator string, pubDate, lastBuildDate time.Time) *RssFeed {
	feed := &RssFeed{
		Title:         CDATA(title),
		Link:          link,
		Description:   CDATA(description),
		PubDate:       anyTimeFormat(time.RFC1123Z, pubDate),
		LastBuildDate: anyTimeFormat(time.RFC1123Z, lastBuildDate),
	}

	if len(language) > 0 {
		feed.Language = language
	}
	if len(generator) > 0 {
		feed.Generator = generator
	}

	return feed
}

func NewRssFeedItem(title, link, description, author, category string, pubDate time.Time) *RssItem {
	item := &RssItem{
		Title:       CDATA(title),
		Link:        link,
		Description: CDATA(description),
		Guid:        link,
		PubDate:     anyTimeFormat(time.RFC1123Z, pubDate),
	}

	if len(author) > 0 {
		item.Author = CDATA(author)
	}
	if len(category) > 0 {
		item.Category = CDATA(category)
	}

	return item
}
