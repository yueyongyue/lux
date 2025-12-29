package bilibili

import (
	"encoding/json"
	"strconv"
)

// jsonInt handles Bilibili's inconsistent API where aid can be a JSON number or a JSON string.
type jsonInt int

func (j *jsonInt) UnmarshalJSON(data []byte) error {
	// Try numeric first
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*j = jsonInt(n)
		return nil
	}
	// Try string form
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if s == "" {
			*j = 0
			return nil
		}
		n, err := strconv.Atoi(s)
		if err == nil {
			*j = jsonInt(n)
			return nil
		}
	}
	*j = 0
	return nil
}

// {"code":0,"message":"0","ttl":1,"data":{"token":"aaa"}}
// {"code":-101,"message":"账号未登录","ttl":1}
type tokenData struct {
	Token string `json:"token"`
}

type token struct {
	Code    int       `json:"code"`
	Message string    `json:"message"`
	Data    tokenData `json:"data"`
}

type Interaction struct {
	Interaction bool `json:"interaction"`
}

type EpVideoInfo struct {
	Aid                           int         `json:"aid"`
	Bvid                          string      `json:"bvid"`
	Cid                           int         `json:"cid"`
	DeliveryBusinessFragmentVideo bool        `json:"delivery_business_fragment_video"`
	DeliveryFragmentVideo         bool        `json:"delivery_fragment_video"`
	EpID                          int         `json:"ep_id"`
	EpStatus                      int         `json:"ep_status"`
	Interaction                   Interaction `json:"interaction"`
	LongTitle                     string      `json:"long_title"`
	Title                         string      `json:"title"`
}

type bangumiData struct {
	EpInfo EpVideoInfo   `json:"epInfo"`
	EpList []EpVideoInfo `json:"epList"`
}

type videoPagesData struct {
	Cid  jsonInt `json:"cid"`
	Part string  `json:"part"`
	Page int     `json:"page"`
}

type multiPageVideoData struct {
	Title string           `json:"title"`
	Pages []videoPagesData `json:"pages"`
}

type episode struct {
	Aid   jsonInt `json:"aid"`
	Cid   jsonInt `json:"cid"`
	Title string  `json:"title"`
	BVid  string  `json:"bvid"`
}

type multiEpisodeData struct {
	Seasionid int       `json:"season_id"`
	Episodes  []episode `json:"episodes"`
}

// ugcSeasonEpisode represents a single video in a Bilibili 合集 (ugcSeason).
type ugcSeasonEpisode struct {
	Aid   jsonInt `json:"aid"`
	BVid  string  `json:"bvid"`
	Cid   jsonInt `json:"cid"`
	Title string  `json:"title"`
}

// ugcSeasonSection is one section within a ugcSeason collection.
type ugcSeasonSection struct {
	Title    string             `json:"title"`
	Episodes []ugcSeasonEpisode `json:"episodes"`
}

// ugcSeason represents Bilibili's 合集 (video collection) structure embedded in __INITIAL_STATE__.
type ugcSeason struct {
	Title    string             `json:"title"`
	Sections []ugcSeasonSection `json:"sections"`
}

type multiPage struct {
	Aid       jsonInt            `json:"aid"`
	BVid      string             `json:"bvid"`
	Sections  []multiEpisodeData `json:"sections"`
	VideoData multiPageVideoData `json:"videoData"`
	UgcSeason ugcSeason          `json:"ugcSeason"`
}

type dashStream struct {
	ID        int    `json:"id"`
	BaseURL   string `json:"baseUrl"`
	Bandwidth int    `json:"bandwidth"`
	MimeType  string `json:"mimeType"`
	Codecid   int    `json:"codecid"`
	Codecs    string `json:"codecs"`
}

type dashStreams struct {
	Video []dashStream `json:"video"`
	Audio []dashStream `json:"audio"`
}

type dashInfo struct {
	CurQuality  int         `json:"quality"`
	Description []string    `json:"accept_description"`
	Quality     []int       `json:"accept_quality"`
	Streams     dashStreams `json:"dash"`
	DURLFormat  string      `json:"format"`
	DURLs       []dURL      `json:"durl"`
}

type dURL struct {
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

type dash struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Data    dashInfo `json:"data"`
	Result  dashInfo `json:"result"`
}

var qualityString = map[int]string{
	127: "超高清 8K",
	120: "超清 4K",
	116: "高清 1080P60",
	74:  "高清 720P60",
	112: "高清 1080P+",
	80:  "高清 1080P",
	64:  "高清 720P",
	48:  "高清 720P",
	32:  "清晰 480P",
	16:  "流畅 360P",
	15:  "流畅 360P",
}

type subtitleData struct {
	From     float32 `json:"from"`
	To       float32 `json:"to"`
	Location int     `json:"location"`
	Content  string  `json:"content"`
}

type bilibiliSubtitleFormat struct {
	FontSize        float32        `json:"font_size"`
	FontColor       string         `json:"font_color"`
	BackgroundAlpha float32        `json:"background_alpha"`
	BackgroundColor string         `json:"background_color"`
	Stroke          string         `json:"Stroke"`
	Body            []subtitleData `json:"body"`
}

type subtitleProperty struct {
	ID          int64  `json:"id"`
	Lan         string `json:"lan"`
	LanDoc      string `json:"lan_doc"`
	SubtitleUrl string `json:"subtitle_url"`
}

type subtitleInfo struct {
	AllowSubmit  bool               `json:"allow_submit"`
	SubtitleList []subtitleProperty `json:"subtitles"`
}

type bilibiliWebInterfaceData struct {
	Bvid         string       `json:"bvid"`
	SubtitleInfo subtitleInfo `json:"subtitle"`
}

type bilibiliWebInterface struct {
	Code int                      `json:"code"`
	Data bilibiliWebInterfaceData `json:"data"`
}

type festival struct {
	VideoSections []struct {
		Id    int64  `json:"id"`
		Title string `json:"title"`
		Type  int    `json:"type"`
	} `json:"videoSections"`
	Episodes  []episode `json:"episodes"`
	VideoInfo struct {
		Aid   int    `json:"aid"`
		BVid  string `json:"bvid"`
		Cid   int    `json:"cid"`
		Title string `json:"title"`
		Desc  string `json:"desc"`
		Pages []struct {
			Cid       int    `json:"cid"`
			Duration  int    `json:"duration"`
			Page      int    `json:"page"`
			Part      string `json:"part"`
			Dimension struct {
				Width  int `json:"width"`
				Height int `json:"height"`
				Rotate int `json:"rotate"`
			} `json:"dimension"`
		} `json:"pages"`
	} `json:"videoInfo"`
}
