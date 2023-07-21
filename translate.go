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
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type Translater interface {
	Translate(msg string) string
}

func bgAddTranslationFilter(messageSender chan<- string, selector string, translater Translater) chan<- linkInfo {
	filteringChan := make(chan linkInfo)
	go addTranslationFiltering(messageSender, selector, translater, filteringChan)
	return filteringChan
}

func addTranslationFiltering(messageSender chan<- string, selector string, translater Translater, filteringChan <-chan linkInfo) {
	for info := range filteringChan {
		// use a separate function to avoid waiting infinitely for the defering execution of body close
		addTranslationFilter(messageSender, selector, translater, info)
	}
}

func addTranslationFilter(messageSender chan<- string, selector string, translater Translater, info linkInfo) {
	extract := info.description
	if strings.ToLower(selector[:4]) == "css:" {
		resp, err := http.Get(info.link)
		if err != nil {
			log.Println("Failed to retrieved content from link :", err)
			return
		}
		defer resp.Body.Close()

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			log.Println("Failed to parse content from link :", err)
			return
		}
		extract = doc.Find(selector[4:]).Text()
	}
	translated := translater.Translate(extract)

	var filteredMessageBuilder strings.Builder
	filteredMessageBuilder.WriteString(info.link)
	filteredMessageBuilder.WriteByte('\n')
	filteredMessageBuilder.WriteString(translated)
	messageSender <- filteredMessageBuilder.String()
}
