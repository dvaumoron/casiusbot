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

	"github.com/bwmarrin/discordgo"
	"github.com/mmcdole/gofeed"
)

func sendMessage(session *discordgo.Session, channelId string, messageReceiver <-chan string) {
	for message := range messageReceiver {
		_, err := session.ChannelMessageSend(channelId, message)
		if err != nil {
			log.Println("Message sending failed :", err)
		}
	}
}

func bgReadMultipleRSS(messageSender chan<- string, feedURLs []string, startTime time.Time, tickers []chan time.Time) {
	for index, feedURL := range feedURLs {
		go startReadRSS(messageSender, feedURL, startTime, tickers[index])
	}
}

func startReadRSS(messageSender chan<- string, feedURL string, previous time.Time, ticker <-chan time.Time) {
	fp := gofeed.NewParser()
	for range ticker {
		previous = readRSS(messageSender, fp, feedURL, previous)
	}
}

func readRSS(messageSender chan<- string, fp *gofeed.Parser, feedURL string, after time.Time) time.Time {
	var mostRecent time.Time
	if feed, err := fp.ParseURL(feedURL); err == nil {
		for _, item := range feed.Items {
			published := item.PublishedParsed
			if published == nil || published.IsZero() {
				log.Println("RSS published parsing failed")
			} else {
				if published.After(after) {
					messageSender <- item.Link
				}
				if published.After(mostRecent) {
					mostRecent = *published
				}
			}
		}
	} else {
		log.Println("RSS parsing failed :", err)
	}
	return mostRecent
}
