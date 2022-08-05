// package main is a SSB client which "plugs" a RSS feed into the Scuttleverse.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
	"github.com/ssbc/go-luigi"
	"github.com/ssbc/go-ssb"
	refs "github.com/ssbc/go-ssb-refs"
	ssbClient "github.com/ssbc/go-ssb/client"
	"github.com/ssbc/go-ssb/message"
	"github.com/ssbc/go-ssb/sbot"
	"gopkg.in/yaml.v2"
)

// Config is a rss-butt-plug config file.
type Config struct {
	DataDir string      `yaml:"data-dir"`
	Feed    string      `yaml:"feed"`
	Addr    string      `yaml:"addr"`
	Port    string      `yaml:"port"`
	WsPort  string      `yaml:"ws-port"`
	ShsCap  string      `yaml:"shs-cap"`
	KeyPair ssb.KeyPair `yaml:"key-pair,omitempty"`
	Avatar  string      `yaml:"avatar,omitempty"`
}

// Post is a ssb post message.
type Post struct {
	Type string `json:"type"`
	Link string `json:"link"`
	Text string `json:"text"`
	Root string `json:"root,omitempty"`
}

// help is the rss-butt-plug CLI help output.
const help = `rss-butt-plug [options] [<feed>]

A SSB client which "plugs" a RSS feed into the Scuttleverse.

An example configuration file:

---
data-dir: ~/.rss-butt-plug
feed: https://openrss.org/opencollective.com/secure-scuttlebutt-consortium/updates 
addr: localhost
port: 8008
ws-port: 8989
shs-cap: "1KHLiKZvAvjbY1ziZEHMXawbCEIM6qwjCDm3VYRan/s="
avatar: https://images.opencollective.com/secure-scuttlebutt-consortium/676f245/logo/256.png

Arguments:
  <feed>    a feed to test parsing

Options:
  -h    output help
  -c    path to config file
  -p    feed poll frequency in minutes
`

// maxPostLength is a post limit set by rss-butt-plug which is smaller than the
// actual max post length of 8192, to allow some buffer. Any RSS post with a
// greater length will be split up into threads.
const maxPostLength = 7000

var helpFlag bool
var debugFlag bool
var configFlag string
var pollFrequencyFlag int

// handleCliFlags parses CLI flags.
func handleCliFlags() error {
	flag.BoolVar(&helpFlag, "h", false, "output help")
	flag.StringVar(&configFlag, "c", "rss-butt-plug.yaml", "config file")
	flag.IntVar(&pollFrequencyFlag, "p", 5, "feed poll frequency in minutes")
	flag.Parse()

	return nil
}

// parseRSSFeed parses an entire RSS feed into memory.
func parseRSSFeed(url string) (gofeed.Feed, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	feedParser := gofeed.NewParser()
	feed, err := feedParser.ParseURLWithContext(url, ctx)
	if err != nil {
		return gofeed.Feed{}, fmt.Errorf("unable to parse %s: %w", url, err)
	}

	return *feed, nil
}

// getImage retrieves an image from the internet.
func getImage(url string) (io.Reader, error) {
	response, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("getImage: unable to retrieve %s: %w", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		return nil, fmt.Errorf("getImage: unable to retrieve %s: HTTP %d", url, response.StatusCode)
	}

	body, error := ioutil.ReadAll(response.Body)
	if error != nil {
		return nil, fmt.Errorf("getImage: unable to read response body: %w", err)
	}

	return bytes.NewReader(body), nil
}

// htmlToMarkdown converts HTML to Markdown. Image links are processed into
// blob refs for SSB client readers.
func htmlToMarkdown(content string, pub *sbot.Sbot, postBlobs bool) (string, error) {
	var markdown string

	converter := md.NewConverter("", true, nil)

	converter.AddRules(
		md.Rule{
			Filter: []string{"img"},
			Replacement: func(content string, selec *goquery.Selection, opt *md.Options) *string {
				if postBlobs {
					src, _ := selec.Attr("src")
					srcReader, err := getImage(src)
					if err != nil {
						log.Fatal(fmt.Errorf("htmlToMarkdown: %w", err))
					}

					ref, err := pub.BlobStore.Put(srcReader)
					if err != nil {
						log.Fatal(fmt.Errorf("htmlToMarkdown: %w", err))
					}

					log.Printf("htmlToMarkdown: successfully posted %s as blob", src)

					return md.String("![](" + ref.String() + ")")
				}

				return nil
			},
		},
	)

	markdown, err := converter.ConvertString(content)
	if err != nil {
		return markdown, fmt.Errorf("htmlToMarkdown: unable to convert html to markdown: %w", err)
	}

	return markdown, nil
}

// firstRSSPost retrieves the post content of the first message of a RSS feed.
func firstRSSPost(testFeed string, pub *sbot.Sbot) (string, error) {
	var markdown string

	feed, err := parseRSSFeed(testFeed)
	if err != nil {
		return markdown, fmt.Errorf("firstRSSPost: %w", err)
	}

	for _, feed := range feed.Items {
		content := feed.Content
		if feed.Content == "" {
			content = feed.Description
		}

		log.Printf("firstRSSPost: converting '%s' to markdown", feed.Title)

		markdown, err = htmlToMarkdown(content, pub, false)
		if err != nil {
			return markdown, fmt.Errorf("firstRSSPost: %w", err)
		}

		return markdown, err
	}

	return markdown, fmt.Errorf("firstRSSPost: %w", err)
}

// loadYAMLConfig loads a rss-butt-plug YAML user config.
func loadYAMLConfig() (Config, error) {
	var cfg Config

	configPath, err := filepath.Abs(configFlag)
	if err != nil {
		return Config{}, fmt.Errorf("loadYAMLConfig: unable to convert %s to an absolute path: %w", configFlag, err)
	}

	conf, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("loadYAMLConfig: unable to read %s: %w", configFlag, err)
	}

	err = yaml.UnmarshalStrict(conf, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("loadYAMLConfig: unable to unmarshal %s: %w", string(conf), err)
	}

	return cfg, nil
}

// generatePublicInvite generates an invite by speaking to a local go-sbot instance. It
// uses a high "uses" value (666) so as to make the invite usable to more
// people. It's more a public share invite in that sense.
func generatePublicInvite(pub *sbot.Sbot) (string, error) {
	var token string

	client, err := ssbClient.NewTCP(pub.KeyPair, pub.Network.GetListenAddr())
	if err != nil {
		return token, fmt.Errorf("generatePublicInvite: unable to initalise TCP client: %w", err)
	}

	token, err = client.InviteCreate(message.InviteCreateArgs{Uses: 666})
	if err != nil {
		return token, fmt.Errorf("generatePublicInvite: unable to create invite: %w", err)
	}

	if err := client.Close(); err != nil {
		return token, fmt.Errorf("generatePublicInvite: unable to close TCP client: %w", err)
	}

	if strings.Contains(token, "[::]") {
		token = strings.Replace(token, "[::]", "localhost", 1)
	}

	return token, nil
}

// messagesFromLog retrieves all messages from the user log.
func messagesFromLog(pub *sbot.Sbot) ([]Post, error) {
	var posts []Post

	src, err := pub.ReceiveLog.Query()
	if err != nil {
		return posts, fmt.Errorf("messagesFromLog: unable to query log: %w", err)
	}

	for {
		var post Post

		v, err := src.Next(context.Background())
		if luigi.IsEOS(err) {
			break
		}

		message := v.(refs.Message)
		content := message.ContentBytes()
		if err = json.Unmarshal(content, &post); err != nil {
			return posts, fmt.Errorf("messagesFromLog: unable to unmarshal %s: %w", string(content), err)
		}

		posts = append(posts, post)
	}

	return posts, nil
}

// newSbot instantiates a new go-sbot instance.
func newSbot(cfg Config) (*sbot.Sbot, error) {
	dataDir, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("newSbot: unable to convert %s to an absolute path: %w", cfg.DataDir, err)
	}

	sbotOpts := []sbot.Option{
		sbot.EnableAdvertismentBroadcasts(true),
		sbot.EnableAdvertismentDialing(true),
		sbot.LateOption(sbot.WithUNIXSocket()),
		sbot.WithHops(2),
		sbot.WithListenAddr(fmt.Sprintf(":%s", cfg.Port)),
		sbot.WithRepoPath(dataDir),
		sbot.WithWebsocketAddress(fmt.Sprintf(":%s", cfg.WsPort)),
	}

	pub, err := sbot.New(sbotOpts...)
	if err != nil {
		return nil, fmt.Errorf("newSbot: unable to initialise sbot: %w", err)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c

		pub.Shutdown()
		time.Sleep(2 * time.Second)

		if err := pub.Close(); err != nil {
			log.Fatal(fmt.Errorf("newSbot: %w", err))
		}

		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()

	return pub, nil
}

// serveSbot serves a go-sbot over the network.
func serveSbot(pub *sbot.Sbot) {
	for {
		ctx := context.TODO()
		err := pub.Network.Serve(ctx)
		if err != nil {
			log.Fatal(fmt.Errorf("serveSbot: %w", err))
		}

		time.Sleep(1 * time.Second)

		select {
		case <-ctx.Done():
			err := pub.Close()
			if err != nil {
				log.Fatal(fmt.Errorf("serveSbot: %w", err))
			}
		default:
		}
	}
}

// getNewRSSPosts gathers new posts from a RSS feed.
func getNewRSSPosts(feed gofeed.Feed, posts []Post, pub *sbot.Sbot) ([]map[string]interface{}, error) {
	var messages []map[string]interface{}

	for idx := len(feed.Items) - 1; idx >= 0; idx-- {
		feed := feed.Items[idx]
		alreadyPosted := false

		for _, post := range posts {
			if feed.Link == post.Link {
				alreadyPosted = true
				break
			}
		}

		if alreadyPosted {
			log.Printf("getNewRSSPosts: skipping %s, already posted", feed.Link)
			continue
		}

		feedContent := feed.Content
		if feed.Content == "" {
			feedContent = feed.Description
		}

		log.Printf("getNewRSSPosts: converting '%s' to markdown", feed.Title)

		markdown, err := htmlToMarkdown(feedContent, pub, true)
		if err != nil {
			return messages, fmt.Errorf("getNewRSSPosts: %w", err)
		}

		content := fmt.Sprintf("# %s\n", feed.Title)

		if feed.Image != nil {
			srcReader, err := getImage(feed.Image.URL)
			if err != nil {
				return messages, fmt.Errorf("getNewRSSPosts: %w", err)
			}

			ref, err := pub.BlobStore.Put(srcReader)
			if err != nil {
				return messages, fmt.Errorf("getNewRSSPosts: unable to upload blob: %w", err)
			}

			content += "\n![](" + ref.String() + ")\n"
		}

		content += markdown
		content += "\n---\n[Clearnet link](" + feed.Link + ")\n"

		messages = append(messages, map[string]interface{}{
			"type": "post",
			"link": feed.Link,
			"text": content,
		})
	}

	return messages, nil
}

// createAboutMessage publishes an about message with accompanying avatar, if available in config).
func createAboutMessage(pub *sbot.Sbot, posts []Post, feed gofeed.Feed, cfg Config) (map[string]interface{}, bool, error) {
	for _, post := range posts {
		if post.Type == "about" {
			log.Printf("createAboutMessage: skipping about message post, already done")
			return nil, false, nil
		}
	}

	message := map[string]interface{}{
		"type":  "about",
		"about": pub.KeyPair.ID(),
		"name":  feed.Title,
	}

	if cfg.Avatar != "" {
		srcReader, err := getImage(cfg.Avatar)
		if err != nil {
			return nil, false, fmt.Errorf("createAboutMessage: %w", err)
		}

		ref, err := pub.BlobStore.Put(srcReader)
		if err != nil {
			return nil, false, fmt.Errorf("createAboutMessage: unable to post blob: %w", err)
		}

		message["image"] = ref.String()
	}

	log.Printf("createAboutMessage: creating about message post")

	return message, true, nil
}

// chunkByLine chunks a full markdown converted RSS post into a thread.
// Meaning, a series of chunks which fit under the max post size of a ssb post.
func chunkByLine(content string) []string {
	var chunks []string

	toChunk := content
	for toChunk != "" {
		if len(toChunk) <= maxPostLength {
			chunks = append(chunks, toChunk)
			toChunk = ""
			continue
		}

		chunkIdx := maxPostLength
		for toChunk[chunkIdx] != 10 {
			chunkIdx--
		}

		chunks = append(chunks, toChunk[:chunkIdx])
		toChunk = toChunk[chunkIdx:]
	}

	return chunks
}

// publishAsThread posts a message as a series of linked messages. This is
// useful when the content of the RSS post is too long.
func publishAsThread(publish ssb.Publisher, message map[string]interface{}) error {
	chunks := chunkByLine(message["text"].(string))

	root := map[string]interface{}{
		"type": "post",
		"link": message["link"],
		"text": chunks[0],
	}

	ref, err := publish.Publish(root)
	if err != nil {
		return fmt.Errorf("publishAsThread: failed to publish: %w", err)
	}

	for _, chunk := range chunks[1:] {
		threadReply := map[string]interface{}{
			"type": "post",
			"link": message["link"],
			"text": chunk,
			"root": ref.Key().String(),
		}
		_, err := publish.Publish(threadReply)
		if err != nil {
			return fmt.Errorf("publishAsThread: failed to publish: %w", err)
		}
	}

	return nil
}

// postMessagesToLog posts messages to the local user feed.
func postMessagesToLog(messages []map[string]interface{}, pub *sbot.Sbot) error {
	publish, err := message.OpenPublishLog(pub.ReceiveLog, pub.Users, pub.KeyPair)
	if err != nil {
		return fmt.Errorf("postMessagesToLog: failed to open publish log: %w", err)
	}

	for _, message := range messages {
		if message["type"] == "post" {
			log.Printf("postMessagesToLog: publishing %s to log", message["link"])

			if len(message["text"].(string)) > maxPostLength {
				log.Printf("postMessagesToLog: turning content of %s into thread, too long", message["link"])
				if err := publishAsThread(publish, message); err != nil {
					return fmt.Errorf("postMessagesToLog: unable to thread content for %s: %w", message["link"], err)
				}
				continue
			}
		}

		_, err := publish.Publish(message)
		if err != nil {
			return fmt.Errorf("postMessagesToLog: failed to publish: %w", err)
		}
	}

	return nil
}

// main is the main CLI entrypoint.
func main() {
	handleCliFlags()

	if helpFlag {
		fmt.Printf(help)
		os.Exit(0)
	}

	cfg, err := loadYAMLConfig()
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("loaded %s", configFlag)

	pub, err := newSbot(cfg)
	if err != nil {
		log.Fatal(err)
	}

	args := os.Args[1:]
	if len(args) > 0 {
		markdown, err := firstRSSPost(args[0], pub)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(markdown)
		return
	}

	go serveSbot(pub)

	log.Print("main: bootstrapped internally managed go-sbot")

	cfg.KeyPair = pub.KeyPair
	feed, err := parseRSSFeed(cfg.Feed)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("main: parsed %s", cfg.Feed)

	posts, err := messagesFromLog(pub)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("main: retrieved %d posts from log", len(posts))

	var messages []map[string]interface{}

	aboutMessage, posted, err := createAboutMessage(pub, posts, feed, cfg)
	if err != nil {
		log.Fatal(err)
	}
	if posted {
		messages = append(messages, aboutMessage)
	}

	newRSSPosts, err := getNewRSSPosts(feed, posts, pub)
	if err != nil {
		log.Fatal(err)
	}

	for _, newRSSPost := range newRSSPosts {
		messages = append(messages, newRSSPost)
	}

	if err := postMessagesToLog(messages, pub); err != nil {
		log.Fatal(err)
	}

	token, err := generatePublicInvite(pub)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("main: pub invite: %s", token)

	for {
		log.Printf("main: going to sleep for %d minutes...", pollFrequencyFlag)
		time.Sleep(time.Duration(pollFrequencyFlag) * time.Minute)
		log.Printf("main: waking up to poll %s for new posts", cfg.Feed)

		posts, err := messagesFromLog(pub)
		if err != nil {
			log.Fatal(err)
		}

		messages, err := getNewRSSPosts(feed, posts, pub)
		if err != nil {
			log.Fatal(err)
		}

		if err := postMessagesToLog(messages, pub); err != nil {
			log.Fatal(err)
		}
	}
}
