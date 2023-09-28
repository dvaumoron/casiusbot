/*
 *
 * Copyright 2023 casiusbot authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package main

import (
	"log"
	"time"

	"github.com/dvaumoron/casiusbot/common"
	"github.com/mmcdole/gofeed"
)

type linkInfo struct {
	link        string
	description string
}

func bgReadMultipleRSS(messageSender chan<- common.MultipartMessage, feeds []any, translater Translater, startTime time.Time, tickers []chan time.Time) {
	if len(feeds) == 0 {
		return
	}

	defaultLinkSender := createLinkSender(messageSender)
	for index, feed := range feeds {
		if casted, ok := feed.(map[string]any); ok {
			if feedURL, _ := casted["URL"].(string); feedURL != "" {
				selector, _ := casted["TRANSLATE_SELECTOR"].(string)
				checkRule, _ := casted["CHECKER"].(string)
				filteringSender := initLinkSender(messageSender, selector, translater, defaultLinkSender)
				checkLink := common.InitChecker(checkRule)
				go startReadRSS(filteringSender, feedURL, checkLink, startTime, tickers[index])
			}
		}
	}
}

func initLinkSender(messageSender chan<- common.MultipartMessage, selector string, translater Translater, defaultLinkSender chan<- linkInfo) chan<- linkInfo {
	if selector == "" || translater == nil {
		return defaultLinkSender

	}
	return bgAddTranslationFilter(messageSender, selector, translater)

}

func createLinkSender(messageSender chan<- common.MultipartMessage) chan<- linkInfo {
	linkChan := make(chan linkInfo)
	go sendLink(messageSender, linkChan)
	return linkChan
}

func sendLink(messageSender chan<- common.MultipartMessage, linkReceiver <-chan linkInfo) {
	for info := range linkReceiver {
		messageSender <- common.MultipartMessage{Message: info.link}
	}
}

func startReadRSS(linkSender chan<- linkInfo, feedURL string, checkLink func(string) bool, previous time.Time, ticker <-chan time.Time) {
	fp := gofeed.NewParser()
	for range ticker {
		previous = readRSS(linkSender, fp, feedURL, checkLink, previous)
	}
}

func readRSS(linkSender chan<- linkInfo, fp *gofeed.Parser, feedURL string, checkLink func(string) bool, after time.Time) time.Time {
	var lastPublished time.Time
	if feed, err := fp.ParseURL(feedURL); err == nil {
		for _, item := range feed.Items {
			published := item.PublishedParsed
			if published == nil || published.IsZero() {
				log.Println("RSS published parsing failed")
			} else {
				if published.After(after) {
					if checkLink(item.Link) {
						linkSender <- linkInfo{link: item.Link, description: item.Description}
					} else {
						log.Println("Rejected link : ", item.Link)
					}
				}
				if published.After(lastPublished) {
					lastPublished = *published
				}
			}
		}
	} else {
		log.Println("RSS parsing failed :", err)
	}
	if lastPublished.IsZero() {
		lastPublished = after
	}
	return lastPublished
}
