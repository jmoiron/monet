package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"

	"labix.org/v2/mgo/bson"

	"github.com/chimeracoder/anaconda"
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/db"
)

var twitter struct {
	ApiKey            string
	ApiSecret         string
	AccessToken       string
	AccessTokenSecret string
	Enabled           bool
}

type MongoTweet struct {
	Sourceid        string `bson:"sourceid"`
	Url             string `bson:"url"`
	Type            string `bson:"type"`
	Title           string `bson:"title"`
	Data            string `bson:"data"` // original json of loaded tweet
	Summaryrendered string `bson:"summaryrendered"`
	Contentrendered string `bson:"contentrendered"`
	Timestamp       int64  `bson:"timestamp"`
}

func init() {
	streams := conf.Config.Streams
	for _, stream := range streams {
		name, ok := stream["type"]
		if ok && name == "twitter" {
			twitter.ApiKey = stream["api_key"]
			twitter.ApiSecret = stream["api_secret"]
			twitter.AccessToken = stream["access_token"]
			twitter.AccessTokenSecret = stream["access_token_secret"]
			if len(twitter.ApiKey) > 0 && len(twitter.ApiSecret) > 0 &&
				len(twitter.AccessToken) > 0 && len(twitter.AccessTokenSecret) > 0 {
				twitter.Enabled = true
			}
		}
	}

}

func twitterUrl(id, username string) string {
	return fmt.Sprintf("https://twitter.com/%s/status/%s", username, id)
}

// render a tweet for display on my front-end, which is kind of a pain
func renderSummary(tweet anaconda.Tweet) string {
	url := twitterUrl(tweet.IdStr, tweet.User.ScreenName)
	// outer receives the content
	outer := `<div class="entry twitter">%s</div>`
	// tweet goes in content, gets the URL and the rendered content
	inner := `<a href="%s"><i class="icon icon-twitter-sign"></i></a>%s`
	content := renderTweet(tweet)
	return fmt.Sprintf(outer, fmt.Sprintf(inner, url, content))
}

type trepl struct {
	Indices     [2]int
	Replacement string
}

type repls []trepl

func (r repls) Len() int           { return len(r) }
func (r repls) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
func (r repls) Less(i, j int) bool { return r[i].Indices[0] < r[j].Indices[0] }

func renderTweet(tweet anaconda.Tweet) string {
	// create templates
	ht := `<a href="https://twitter.com/search?q=%%23%s" title="#%s" 
		class="tweet-url hashtag" rel="nofollow">#%s</a>`
	urlt := `<a href="%s" rel="nofollow">%s</a>`
	usert := `<a href="https://twitter.com/%s" title="@%s"
		class="tweet-url username" rel="nofollow">@%s</a>`
	mediat := `<a href="%s" rel="nofollow">%s</a>`

	// allocate replacements
	e := tweet.Entities
	size := len(e.Hashtags) + len(e.Urls) + len(e.User_mentions) + len(e.Media)
	repl := make(repls, 0, size)

	// store each entity type as a 'replacement'
	for _, hashtag := range e.Hashtags {
		r := trepl{
			Indices:     [2]int{hashtag.Indices[0], hashtag.Indices[1]},
			Replacement: fmt.Sprintf(ht, hashtag.Text, hashtag.Text, hashtag.Text),
		}
		repl = append(repl, r)
	}

	for _, url := range e.Urls {
		r := trepl{
			Indices:     [2]int{url.Indices[0], url.Indices[1]},
			Replacement: fmt.Sprintf(urlt, url.Expanded_url, url.Display_url),
		}
		repl = append(repl, r)
	}

	for _, user := range e.User_mentions {
		r := trepl{
			Indices:     [2]int{user.Indices[0], user.Indices[1]},
			Replacement: fmt.Sprintf(usert, user.Screen_name, user.Screen_name, user.Screen_name),
		}
		repl = append(repl, r)
	}

	for _, media := range e.Media {
		r := trepl{
			Indices:     [2]int{media.Indices[0], media.Indices[1]},
			Replacement: fmt.Sprintf(mediat, media.Expanded_url, media.Display_url),
		}
		repl = append(repl, r)
	}

	sort.Sort(sort.Reverse(repl))
	text := tweet.Text
	for _, r := range repl {
		text = text[:r.Indices[0]] + r.Replacement + text[r.Indices[1]:]
	}
	return text
}

func updateTwitter() {
	if !twitter.Enabled {
		fmt.Println("Twitter updating is not enabled.")
	}

	anaconda.SetConsumerKey(twitter.ApiKey)
	anaconda.SetConsumerSecret(twitter.ApiSecret)
	api := anaconda.NewTwitterApi(twitter.AccessToken, twitter.AccessTokenSecret)

	args := url.Values{}
	args.Set("username", "jmoiron")
	args.Set("user_id", "23013064")
	args.Set("count", "200")
	args.Set("include_rts", "false")
	args.Set("exclude_replies", "true")
	// args.Set("trim_user", "true")
	res, err := api.GetUserTimeline(args)

	if err != nil {
		fmt.Println(err)
		return
	}

	mts := []MongoTweet{}
	for _, tweet := range res {
		b, err := json.Marshal(tweet)
		if err != nil {
			fmt.Println(err)
			continue
		}
		t, err := tweet.CreatedAtTime()
		if err != nil {
			fmt.Println(err)
			continue
		}
		m := MongoTweet{
			Sourceid:        tweet.IdStr,
			Url:             twitterUrl(tweet.IdStr, tweet.User.ScreenName),
			Type:            "twitter",
			Title:           fmt.Sprintf("tweet @ %s", tweet.CreatedAt),
			Data:            string(b),
			Summaryrendered: renderSummary(tweet),
			Timestamp:       t.Unix(),
		}
		mts = append(mts, m)
	}

	for _, m := range mts {
		_, err := db.Current.Db.C("stream").Upsert(bson.M{"type": "twitter", "sourceid": m.Sourceid}, m)
		if err != nil {
			fmt.Println(err)
		}
	}
}
