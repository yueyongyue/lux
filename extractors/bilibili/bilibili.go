package bilibili

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/iawia002/lux/extractors"
	"github.com/iawia002/lux/parser"
	"github.com/iawia002/lux/request"
	"github.com/iawia002/lux/utils"
)

func init() {
	bilibiliExtractor := New()
	extractors.Register("bilibili", bilibiliExtractor)
	extractors.Register("b23", bilibiliExtractor)
}

const (
	bilibiliAPI        = "https://api.bilibili.com/x/player/playurl?"
	bilibiliBangumiAPI = "https://api.bilibili.com/pgc/player/web/playurl?"
	bilibiliTokenAPI   = "https://api.bilibili.com/x/player/playurl/token?"
)

const referer = "https://www.bilibili.com"

var utoken string

var (
	sessionCookies string
	sessionMu      sync.Mutex
	sessionExpiry  time.Time
)

type spiResp struct {
	Code int `json:"code"`
	Data struct {
		B3 string `json:"b_3"`
		B4 string `json:"b_4"`
	} `json:"data"`
}

type secPayload struct {
	Q      string `json:"q"`
	R      string `json:"r"`
	Verity int    `json:"verity"`
	Exp    int64  `json:"exp"`
}

type checkResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func newClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DisableCompression:  true,
			TLSHandshakeTimeout: 10 * time.Second,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
		Timeout: 30 * time.Second,
	}
}

func doGet(client *http.Client, targetURL, ua, cookie string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Referer", referer)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	return client.Do(req)
}

// solveBilibiliChallenge handles Bilibili's PoW (Proof of Work) security challenge.
// Flow: SPI → buvid3/4 → 412 challenge → SHA256 PoW → check → verified token.
func solveBilibiliChallenge(triggerURL string) (string, time.Time) {
	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	client := newClient()

	// Step 1: get buvid3/buvid4
	resp, err := doGet(client, "https://api.bilibili.com/x/frontend/finger/spi", ua, "")
	if err != nil {
		return "", time.Time{}
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var spi spiResp
	if json.Unmarshal(body, &spi) != nil || spi.Code != 0 || spi.Data.B3 == "" {
		return "", time.Time{}
	}
	cookies := fmt.Sprintf("buvid3=%s; buvid4=%s", spi.Data.B3, spi.Data.B4)

	// Step 2: trigger 412 challenge on the actual target URL
	resp2, err := doGet(client, triggerURL, ua, cookies)
	if err != nil || resp2.StatusCode != 412 {
		if resp2 != nil {
			io.Copy(io.Discard, resp2.Body) //nolint:errcheck
			resp2.Body.Close()
		}
		return cookies, time.Now().Add(5 * time.Minute)
	}
	io.Copy(io.Discard, resp2.Body) //nolint:errcheck
	resp2.Body.Close()

	var secToken string
	for _, c := range resp2.Cookies() {
		if c.Name == "X-BILI-SEC-TOKEN" {
			secToken = c.Value
			break
		}
	}
	if secToken == "" {
		return cookies, time.Now().Add(5 * time.Minute)
	}

	// Step 3: decode JWT payload from X-BILI-SEC-TOKEN
	parts := strings.SplitN(secToken, ",", 2)
	if len(parts) != 2 {
		return cookies + "; X-BILI-SEC-TOKEN=" + secToken, time.Now().Add(5 * time.Minute)
	}
	jwtParts := strings.Split(parts[1], ".")
	if len(jwtParts) != 3 {
		return cookies + "; X-BILI-SEC-TOKEN=" + secToken, time.Now().Add(5 * time.Minute)
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(jwtParts[1])
	if err != nil {
		return cookies + "; X-BILI-SEC-TOKEN=" + secToken, time.Now().Add(5 * time.Minute)
	}
	var payload secPayload
	json.Unmarshal(payloadBytes, &payload) //nolint:errcheck

	expiry := time.Now().Add(30 * time.Minute)
	if payload.Exp > 0 {
		expiry = time.Unix(payload.Exp, 0)
	}

	// Already verified (verity != 0)
	if payload.Verity != 0 {
		return cookies + "; X-BILI-SEC-TOKEN=" + secToken, expiry
	}

	// Step 4: SHA256 proof-of-work — find i such that sha256(q+i) == r
	result := -1
	for i := range 5_000_000 {
		hash := sha256.Sum256([]byte(payload.Q + strconv.Itoa(i)))
		if hex.EncodeToString(hash[:]) == payload.R {
			result = i
			break
		}
	}
	if result == -1 {
		return cookies + "; X-BILI-SEC-TOKEN=" + secToken, expiry
	}

	// Step 5: POST result to Bilibili check endpoint
	form := url.Values{"token": {parts[1]}, "result": {strconv.Itoa(result)}}
	req, err := http.NewRequest(http.MethodPost, "https://security.bilibili.com/th/captcha/check", strings.NewReader(form.Encode()))
	if err != nil {
		return cookies + "; X-BILI-SEC-TOKEN=" + secToken, expiry
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", referer+"/")
	req.Header.Set("Origin", referer)
	req.Header.Set("Cookie", cookies+"; X-BILI-SEC-TOKEN="+secToken)
	resp3, err := client.Do(req)
	if err != nil {
		return cookies + "; X-BILI-SEC-TOKEN=" + secToken, expiry
	}
	body3, _ := io.ReadAll(resp3.Body)
	resp3.Body.Close()

	var check checkResp
	if json.Unmarshal(body3, &check) != nil || check.Code != 0 || check.Message == "" {
		return cookies + "; X-BILI-SEC-TOKEN=" + secToken, expiry
	}

	return cookies + "; X-BILI-SEC-TOKEN=" + check.Message, expiry
}

// getBilibiliCookies returns a valid cookie string for Bilibili, solving the PoW challenge
// if needed. Results are cached until the token expires.
func getBilibiliCookies(triggerURL string) string {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if sessionCookies != "" && time.Now().Before(sessionExpiry) {
		return sessionCookies
	}
	c, exp := solveBilibiliChallenge(triggerURL)
	if c != "" {
		sessionCookies = c
		sessionExpiry = exp
	}
	return sessionCookies
}

// bilibiliHeaders returns extra headers for Bilibili API calls.
// When the user has provided their own cookies via -c, we skip injecting
// our generated PoW cookies to avoid buvid3/4 conflicts.
func bilibiliHeaders() map[string]string {
	h := map[string]string{"Origin": referer}
	if !request.HasCookie() {
		sessionMu.Lock()
		c := sessionCookies
		sessionMu.Unlock()
		if c != "" {
			h["Cookie"] = c
		}
	}
	return h
}

func genAPI(aid, cid, quality int, bvid string, bangumi bool, cookie string) (string, error) {
	var (
		err        error
		baseAPIURL string
		params     string
	)
	if cookie != "" && utoken == "" {
		utoken, err = request.Get(
			fmt.Sprintf("%said=%d&cid=%d", bilibiliTokenAPI, aid, cid),
			referer,
			bilibiliHeaders(),
		)
		if err != nil {
			return "", err
		}
		var t token
		err = json.Unmarshal([]byte(utoken), &t)
		if err != nil {
			return "", err
		}
		if t.Code != 0 {
			return "", errors.Errorf("cookie error: %s", t.Message)
		}
		utoken = t.Data.Token
	}
	var api string
	if bangumi {
		// The parameters need to be sorted by name
		// qn=0 flag makes the CDN address different every time
		// quality=120(4k) is the highest quality so far
		params = fmt.Sprintf(
			"cid=%d&bvid=%s&qn=%d&type=&otype=json&fourk=1&fnver=0&fnval=16",
			cid, bvid, quality,
		)
		baseAPIURL = bilibiliBangumiAPI
	} else {
		params = fmt.Sprintf(
			"avid=%d&cid=%d&bvid=%s&qn=%d&type=&otype=json&fourk=1&fnver=0&fnval=2000",
			aid, cid, bvid, quality,
		)
		baseAPIURL = bilibiliAPI
	}
	api = baseAPIURL + params
	// bangumi utoken also need to put in params to sign, but the ordinary video doesn't need
	if !bangumi && utoken != "" {
		api = fmt.Sprintf("%s&utoken=%s", api, utoken)
	}
	return api, nil
}

type bilibiliOptions struct {
	url      string
	html     string
	bangumi  bool
	aid      int
	cid      int
	bvid     string
	page     int
	subtitle string
}

func extractBangumi(url, html string, extractOption extractors.Options) ([]*extractors.Data, error) {
	dataString := utils.MatchOneOf(html, `const playurlSSRData = ({[\s\S]+})`)[1]
	epArrayString := utils.MatchOneOf(dataString, `"episode_info"\s*:\s*(.+?)\s*,\s*"season_info"`)[1]
	fullVideoIdString := utils.MatchOneOf(dataString, `"videoId"\s*:\s*"(ep|ss)(\d+)"`)
	epSsString := fullVideoIdString[1] // "ep" or "ss"
	videoIdString := fullVideoIdString[2]

	var epArray EpVideoInfo
	err := json.Unmarshal([]byte(epArrayString), &epArray)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	var data bangumiData

	videoId, err := strconv.ParseInt(videoIdString, 10, 0)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if epArray.EpID == int(videoId) || (epSsString == "ss" && epArray.Title == "第1话") {
		data.EpInfo = epArray
	}
	data.EpList = append(data.EpList, epArray)

	sort.Slice(data.EpList, func(i, j int) bool {
		return data.EpList[i].EpID < data.EpList[j].EpID
	})

	if !extractOption.Playlist {
		aid := data.EpInfo.Aid
		cid := data.EpInfo.Cid
		bvid := data.EpInfo.Bvid
		titleFormat := data.EpInfo.Title
		longTitle := data.EpInfo.LongTitle
		if aid <= 0 || cid <= 0 || bvid == "" {
			aid = data.EpList[0].Aid
			cid = data.EpList[0].Cid
			bvid = data.EpList[0].Bvid
			titleFormat = data.EpList[0].Title
			longTitle = data.EpList[0].LongTitle
		}
		options := bilibiliOptions{
			url:     url,
			html:    html,
			bangumi: true,
			aid:     aid,
			cid:     cid,
			bvid:    bvid,

			subtitle: fmt.Sprintf("%s %s", titleFormat, longTitle),
		}
		return []*extractors.Data{bilibiliDownload(options, extractOption)}, nil
	}

	// handle bangumi playlist
	needDownloadItems := utils.NeedDownloadList(extractOption.Items, extractOption.ItemStart, extractOption.ItemEnd, len(data.EpList))
	extractedData := make([]*extractors.Data, len(needDownloadItems))
	wgp := utils.NewWaitGroupPool(extractOption.ThreadNumber)
	dataIndex := 0
	for index, u := range data.EpList {
		if !slices.Contains(needDownloadItems, index+1) {
			continue
		}
		wgp.Add()
		id := u.EpID
		if id == 0 {
			id = u.EpID
		}
		// html content can't be reused here
		options := bilibiliOptions{
			url:     fmt.Sprintf("https://www.bilibili.com/bangumi/play/ep%d", id),
			bangumi: true,
			aid:     int(u.Aid),
			cid:     int(u.Cid),
			bvid:    u.Bvid,

			subtitle: fmt.Sprintf("%s %s", u.Title, u.LongTitle),
		}
		go func(index int, options bilibiliOptions, extractedData []*extractors.Data) {
			defer wgp.Done()
			extractedData[index] = bilibiliDownload(options, extractOption)
		}(dataIndex, options, extractedData)
		dataIndex++
	}
	wgp.Wait()
	return extractedData, nil
}

func getMultiPageData(html string) (*multiPage, error) {
	var data multiPage
	multiPageDataString := utils.MatchOneOf(
		html, `window.__INITIAL_STATE__=(.+?);\(function`,
	)
	if multiPageDataString == nil {
		return &data, errors.New("this page has no playlist")
	}
	err := json.Unmarshal([]byte(multiPageDataString[1]), &data)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return &data, nil
}

func extractFestival(url, html string, extractOption extractors.Options) ([]*extractors.Data, error) {
	matches := utils.MatchAll(html, "<\\s*script[^>]*>\\s*window\\.__INITIAL_STATE__=([\\s\\S]*?);\\s?\\(function[\\s\\S]*?<\\/\\s*script\\s*>")
	if len(matches) < 1 {
		return nil, errors.WithStack(extractors.ErrURLParseFailed)
	}
	if len(matches[0]) < 2 {
		return nil, errors.New("could not find video in page")
	}

	var festivalData festival
	err := json.Unmarshal([]byte(matches[0][1]), &festivalData)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	options := bilibiliOptions{
		url:  url,
		html: html,
		aid:  festivalData.VideoInfo.Aid,
		bvid: festivalData.VideoInfo.BVid,
		cid:  festivalData.VideoInfo.Cid,
		page: 0,
	}

	return []*extractors.Data{bilibiliDownload(options, extractOption)}, nil
}

func extractNormalVideo(url, html string, extractOption extractors.Options) ([]*extractors.Data, error) {
	pageData, err := getMultiPageData(html)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if !extractOption.Playlist {
		// handle URL that has a playlist, mainly for unified titles
		// <h1> tag does not include subtitles
		// bangumi doesn't need this
		pageString := utils.MatchOneOf(url, `\?p=(\d+)`)
		var p int
		if pageString == nil {
			// https://www.bilibili.com/video/av20827366/
			p = 1
		} else {
			// https://www.bilibili.com/video/av20827366/?p=2
			p, _ = strconv.Atoi(pageString[1])
		}

		if len(pageData.VideoData.Pages) < p || p < 1 {
			return nil, errors.WithStack(extractors.ErrURLParseFailed)
		}

		page := pageData.VideoData.Pages[p-1]
		options := bilibiliOptions{
			url:  url,
			html: html,
			aid:  int(pageData.Aid),
			bvid: pageData.BVid,
			cid:  int(page.Cid),
			page: p,
		}
		// "part":"" or "part":"Untitled"
		if page.Part == "Untitled" || len(pageData.VideoData.Pages) == 1 {
			options.subtitle = ""
		} else {
			options.subtitle = page.Part
		}
		return []*extractors.Data{bilibiliDownload(options, extractOption)}, nil
	}

	// handle normal video playlist
	// Priority: ugcSeason (合集) > sections (多季) > multi-page (分P)
	if len(pageData.UgcSeason.Sections) > 0 {
		return ugcSeasonDownload(url, extractOption, pageData)
	}
	if len(pageData.Sections) == 0 {
		// https://www.bilibili.com/video/av20827366/?p=* each video in playlist has different p=?
		return multiPageDownload(url, html, extractOption, pageData)
	}
	// handle another kind of playlist
	// https://www.bilibili.com/video/av*** each video in playlist has different av/bv id
	return multiEpisodeDownload(url, html, extractOption, pageData)
}

// ugcSeasonDownload downloads all videos in a Bilibili 合集 (ugcSeason collection).
// Each episode in all sections is treated as an independent download item.
func ugcSeasonDownload(url string, extractOption extractors.Options, pageData *multiPage) ([]*extractors.Data, error) {
	// Flatten all episodes across all sections
	type indexedEpisode struct {
		ep         ugcSeasonEpisode
		globalIdx  int // 1-based index across the whole collection
		sectionNum int
	}
	var all []indexedEpisode
	for _, section := range pageData.UgcSeason.Sections {
		for _, ep := range section.Episodes {
			all = append(all, indexedEpisode{ep: ep, globalIdx: len(all) + 1, sectionNum: len(all) + 1})
		}
	}

	needDownloadItems := utils.NeedDownloadList(extractOption.Items, extractOption.ItemStart, extractOption.ItemEnd, len(all))
	extractedData := make([]*extractors.Data, len(needDownloadItems))
	wgp := utils.NewWaitGroupPool(extractOption.ThreadNumber)
	dataIndex := 0
	for _, item := range all {
		if !slices.Contains(needDownloadItems, item.globalIdx) {
			continue
		}
		wgp.Add()
		options := bilibiliOptions{
			url:      fmt.Sprintf("https://www.bilibili.com/video/%s", item.ep.BVid),
			aid:      int(item.ep.Aid),
			bvid:     item.ep.BVid,
			cid:      int(item.ep.Cid),
			subtitle: item.ep.Title,
		}
		go func(idx int, opts bilibiliOptions) {
			defer wgp.Done()
			extractedData[idx] = bilibiliDownload(opts, extractOption)
		}(dataIndex, options)
		dataIndex++
	}
	wgp.Wait()
	return extractedData, nil
}

// handle multi episode download
func multiEpisodeDownload(url, html string, extractOption extractors.Options, pageData *multiPage) ([]*extractors.Data, error) {
	needDownloadItems := utils.NeedDownloadList(extractOption.Items, extractOption.ItemStart, extractOption.ItemEnd, len(pageData.Sections[0].Episodes))
	extractedData := make([]*extractors.Data, len(needDownloadItems))
	wgp := utils.NewWaitGroupPool(extractOption.ThreadNumber)
	dataIndex := 0
	for index, u := range pageData.Sections[0].Episodes {
		if !slices.Contains(needDownloadItems, index+1) {
			continue
		}
		wgp.Add()
		options := bilibiliOptions{
			url:      url,
			html:     html,
			aid:      int(u.Aid),
			bvid:     u.BVid,
			cid:      int(u.Cid),
			subtitle: fmt.Sprintf("%s P%d", u.Title, index+1),
		}
		go func(index int, options bilibiliOptions, extractedData []*extractors.Data) {
			defer wgp.Done()
			extractedData[index] = bilibiliDownload(options, extractOption)
		}(dataIndex, options, extractedData)
		dataIndex++
	}
	wgp.Wait()
	return extractedData, nil
}

// handle multi page download
func multiPageDownload(url, html string, extractOption extractors.Options, pageData *multiPage) ([]*extractors.Data, error) {
	needDownloadItems := utils.NeedDownloadList(extractOption.Items, extractOption.ItemStart, extractOption.ItemEnd, len(pageData.VideoData.Pages))
	extractedData := make([]*extractors.Data, len(needDownloadItems))
	wgp := utils.NewWaitGroupPool(extractOption.ThreadNumber)
	dataIndex := 0
	for index, u := range pageData.VideoData.Pages {
		if !slices.Contains(needDownloadItems, index+1) {
			continue
		}
		wgp.Add()
		options := bilibiliOptions{
			url:      url,
			html:     html,
			aid:      int(pageData.Aid),
			bvid:     pageData.BVid,
			cid:      int(u.Cid),
			subtitle: u.Part,
			page:     u.Page,
		}
		go func(index int, options bilibiliOptions, extractedData []*extractors.Data) {
			defer wgp.Done()
			extractedData[index] = bilibiliDownload(options, extractOption)
		}(dataIndex, options, extractedData)
		dataIndex++
	}
	wgp.Wait()
	return extractedData, nil
}

type extractor struct{}

// New returns a bilibili extractor.
func New() extractors.Extractor {
	return &extractor{}
}

// Extract is the main function to extract the data.
func (e *extractor) Extract(url string, option extractors.Options) ([]*extractors.Data, error) {
	// When user provides their own cookies via -c, use them directly.
	// Generating our own PoW cookies would conflict with the user's buvid3/4.
	pageHeaders := map[string]string{"Origin": referer}
	if !request.HasCookie() {
		if cookies := getBilibiliCookies(url); cookies != "" {
			pageHeaders["Cookie"] = cookies
		}
	}

	var err error
	html, err := request.Get(url, referer, pageHeaders)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// set thread number to 1 manually to avoid http 412 error
	option.ThreadNumber = 1

	if strings.Contains(url, "bangumi") {
		// handle bangumi
		return extractBangumi(url, html, option)
	} else if strings.Contains(url, "festival") {
		return extractFestival(url, html, option)
	} else {
		// handle normal video
		return extractNormalVideo(url, html, option)
	}
}

// bilibiliDownload is the download function for a single URL
func bilibiliDownload(options bilibiliOptions, extractOption extractors.Options) *extractors.Data {
	var (
		err  error
		html string
	)
	if options.html != "" {
		// reuse html string, but this can't be reused in case of playlist
		html = options.html
	} else {
		html, err = request.Get(options.url, referer, bilibiliHeaders())
		if err != nil {
			return extractors.EmptyData(options.url, err)
		}
	}

	// Get "accept_quality" and "accept_description"
	// "accept_description":["超高清 8K","超清 4K","高清 1080P+","高清 1080P","高清 720P","清晰 480P","流畅 360P"],
	// "accept_quality":[127，120,112,80,48,32,16],
	api, err := genAPI(options.aid, options.cid, 127, options.bvid, options.bangumi, extractOption.Cookie)
	if err != nil {
		return extractors.EmptyData(options.url, err)
	}
	jsonString, err := request.Get(api, referer, bilibiliHeaders())
	if err != nil {
		return extractors.EmptyData(options.url, err)
	}

	var data dash
	err = json.Unmarshal([]byte(jsonString), &data)
	if err != nil {
		return extractors.EmptyData(options.url, err)
	}
	var dashData dashInfo
	if data.Data.Description == nil {
		dashData = data.Result
	} else {
		dashData = data.Data
	}

	var audioPart *extractors.Part
	if dashData.Streams.Audio != nil {
		// Get audio part
		var audioID int
		audios := map[int]string{}
		bandwidth := 0
		for _, stream := range dashData.Streams.Audio {
			if stream.Bandwidth > bandwidth {
				audioID = stream.ID
				bandwidth = stream.Bandwidth
			}
			audios[stream.ID] = stream.BaseURL
		}
		s, err := request.Size(audios[audioID], referer)
		if err != nil {
			return extractors.EmptyData(options.url, err)
		}
		audioPart = &extractors.Part{
			URL:  audios[audioID],
			Size: s,
			Ext:  "m4a",
		}
	}

	streams := make(map[string]*extractors.Stream, len(dashData.Quality))
	for _, stream := range dashData.Streams.Video {
		s, err := request.Size(stream.BaseURL, referer)
		if err != nil {
			return extractors.EmptyData(options.url, err)
		}
		parts := make([]*extractors.Part, 0, 2)
		parts = append(parts, &extractors.Part{
			URL:  stream.BaseURL,
			Size: s,
			Ext:  getExtFromMimeType(stream.MimeType),
		})
		if audioPart != nil {
			parts = append(parts, audioPart)
		}
		var size int64
		for _, part := range parts {
			size += part.Size
		}
		id := fmt.Sprintf("%d-%d", stream.ID, stream.Codecid)
		streams[id] = &extractors.Stream{
			Parts:    parts,
			Size:     size,
			Quality:  fmt.Sprintf("%s %s", qualityString[stream.ID], stream.Codecs),
			Priority: stream.ID,
		}
		if audioPart != nil {
			streams[id].NeedMux = true
		}
	}

	for _, durl := range dashData.DURLs {
		var ext string
		switch dashData.DURLFormat {
		case "flv", "flv480":
			ext = "flv"
		case "mp4", "hdmp4": // nolint
			ext = "mp4"
		}

		parts := make([]*extractors.Part, 0, 1)
		parts = append(parts, &extractors.Part{
			URL:  durl.URL,
			Size: durl.Size,
			Ext:  ext,
		})

		streams[strconv.Itoa(dashData.CurQuality)] = &extractors.Stream{
			Parts:    parts,
			Size:     durl.Size,
			Quality:  qualityString[dashData.CurQuality],
			Priority: dashData.CurQuality,
		}
	}

	// get the title
	doc, err := parser.GetDoc(html)
	if err != nil {
		return extractors.EmptyData(options.url, err)
	}
	title := parser.Title(doc)
	if options.subtitle != "" {
		pageString := ""
		if options.page > 0 {
			pageString = fmt.Sprintf("P%d ", options.page)
		}
		if extractOption.EpisodeTitleOnly {
			title = fmt.Sprintf("%s%s", pageString, options.subtitle)
		} else {
			title = fmt.Sprintf("%s %s%s", title, pageString, options.subtitle)
		}
	}

	return &extractors.Data{
		Site:    "哔哩哔哩 bilibili.com",
		Title:   title,
		Type:    extractors.DataTypeVideo,
		Streams: streams,
		Captions: map[string]*extractors.CaptionPart{
			"danmaku": {
				Part: extractors.Part{
					URL: fmt.Sprintf("https://comment.bilibili.com/%d.xml", options.cid),
					Ext: "xml",
				},
			},
			"subtitle": getSubTitleCaptionPart(options.aid, options.cid),
		},
		URL: options.url,
	}
}

func getExtFromMimeType(mimeType string) string {
	exts := strings.Split(mimeType, "/")
	if len(exts) == 2 {
		return exts[1]
	}
	return "mp4"
}

func getSubTitleCaptionPart(aid int, cid int) *extractors.CaptionPart {
	jsonString, err := request.Get(
		fmt.Sprintf("http://api.bilibili.com/x/player/wbi/v2?aid=%d&cid=%d", aid, cid), referer, bilibiliHeaders(),
	)
	if err != nil {
		return nil
	}
	stu := bilibiliWebInterface{}
	err = json.Unmarshal([]byte(jsonString), &stu)
	if err != nil || len(stu.Data.SubtitleInfo.SubtitleList) == 0 {
		return nil
	}
	return &extractors.CaptionPart{
		Part: extractors.Part{
			URL: fmt.Sprintf("https:%s", stu.Data.SubtitleInfo.SubtitleList[0].SubtitleUrl),
			Ext: "srt",
		},
		Transform: subtitleTransform,
	}
}

func subtitleTransform(body []byte) ([]byte, error) {
	bytes := ""
	captionData := bilibiliSubtitleFormat{}
	err := json.Unmarshal(body, &captionData)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	for i := 0; i < len(captionData.Body); i++ {
		bytes += fmt.Sprintf("%d\n%s --> %s\n%s\n\n",
			i,
			time.Unix(0, int64(captionData.Body[i].From*1000)*int64(time.Millisecond)).UTC().Format("15:04:05.000"),
			time.Unix(0, int64(captionData.Body[i].To*1000)*int64(time.Millisecond)).UTC().Format("15:04:05.000"),
			captionData.Body[i].Content,
		)
	}
	return []byte(bytes), nil
}
