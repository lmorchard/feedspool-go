package main

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-queue/queue"

	log "github.com/sirupsen/logrus"

	"github.com/mmcdole/gofeed"
)

func main() {
	fetchQueueConcurrency := 32
	baseFeedsPath := "data/feeds"

	mkdirpIfNotExist(baseFeedsPath)

	feedUrls, err := readLines("data/feeds.txt")
	if err != nil {
		log.Fatal(err)
	}

	rets := make(chan string)
	q := queue.NewPool(fetchQueueConcurrency)
	defer q.Release()

	for i := 0; i < len(feedUrls); i++ {
		go func(url string) {
			log.Infof("enqueue: %s", url)

			err := q.QueueTask(func(ctx context.Context) error {
				err = pollOneFeed(url, baseFeedsPath)
				rets <- url
				return err
			})
			if err != nil {
				log.Fatal(err)
			}
		}(feedUrls[i])
	}

	// wait until all tasks done
	for i := 0; i < len(feedUrls); i++ {
		log.Infof("fetched: %s %s", i, <-rets)
	}
}

func readLines(fileName string) ([]string, error) {
	f, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	lines := []string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func pollOneFeed(url string, baseFeedsPath string) error {
	hash := sha1Hash(url)
	feedPath := fmt.Sprintf("%s/%s", baseFeedsPath, hash)
	itemsPath := fmt.Sprintf("%s/items", feedPath)

	mkdirpIfNotExist(feedPath)
	mkdirpIfNotExist(itemsPath)

	log.Infof("fetching: %s %s", hash, url)

	fc, err := fetchURL(url)
	if err != nil {
		log.Warn(err.Error())
		return nil
	}

	var feed *gofeed.Feed
	feed, err = gofeed.NewParser().ParseString(fc)
	if err != nil {
		log.Warn(err.Error())
		return nil
	}

	var itemsByDate = make(map[string][]*gofeed.Item)
	for _, item := range feed.Items {
		if item.PublishedParsed == nil {
			log.Warnf("No published date for item in %s %s", url, hash)
			continue
		}
		publishedDT := item.PublishedParsed.Format(time.RFC3339)
		publishedParts := strings.Split(publishedDT, "T")
		publishedDate := publishedParts[0]

		items, exists := itemsByDate[publishedDate]
		if exists {
			itemsByDate[publishedDate] = append(items, item)
		} else {
			itemsByDate[publishedDate] = []*gofeed.Item{item}
		}
	}

	for itemsKey, items := range itemsByDate {
		itemsFilename := fmt.Sprintf("%s/%s.json", itemsPath, itemsKey)
		err = writeJSONFile(itemsFilename, items)
		if err != nil {
			log.Warn(err.Error())
			continue
		}
	}

	feed.Items = []*gofeed.Item{}

	metaFilename := fmt.Sprintf("%s/meta.json", feedPath)
	err = writeJSONFile(metaFilename, feed)
	if err != nil {
		log.Warn(err.Error())
		return nil
	}

	return nil
}

func writeJSONFile(fileName string, obj interface{}) error {
	bytes, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(fileName, bytes, 0644)
	if err != nil {
		return err
	}
	return nil
}

func mkdirpIfNotExist(dirPath string) {
	_, err := os.Stat(dirPath)
	if os.IsNotExist(err) {
		err := os.MkdirAll(dirPath, 0755)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func sha1Hash(source string) string {
	h := sha1.New()
	h.Write([]byte(source))
	bs := h.Sum(nil)
	return fmt.Sprintf("%x", bs)
}

func fetchURL(url string) (string, error) {
	client := http.Client{
		Timeout: 3 * time.Second,
	}
	response, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	return string(contents), nil
}
