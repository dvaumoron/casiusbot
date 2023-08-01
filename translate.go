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
	go addTranslationFiltering(messageSender, initExtracter(selector), translater, filteringChan)
	return filteringChan
}

func addTranslationFiltering(messageSender chan<- string, extracter func(linkInfo) string, translater Translater, filteringChan <-chan linkInfo) {
	for info := range filteringChan {
		var filteredMessageBuilder strings.Builder
		filteredMessageBuilder.WriteString(info.link)
		filteredMessageBuilder.WriteByte('\n')
		filteredMessageBuilder.WriteString(translater.Translate(extracter(info)))
		messageSender <- filteredMessageBuilder.String()
	}
}

func initExtracter(selector string) func(linkInfo) string {
	if len(selector) > 3 {
		if strings.ToLower(selector[:4]) == "css:" {
			return createExtracter(selector[4:])
		}
	}
	return extractDescription
}

func extractDescription(info linkInfo) string {
	return info.description
}

func createExtracter(selector string) func(linkInfo) string {
	toString := htmlToString
	if strings.Contains(selector, "noscript") {
		toString = noscriptToString
	}

	return func(info linkInfo) string {
		resp, err := http.Get(info.link)
		if err != nil {
			log.Println("Failed to retrieved content from link :", err)
			return ""
		}
		defer resp.Body.Close()

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			log.Println("Failed to parse content from link :", err)
			return ""
		}
		return toString(doc.Find(selector))
	}
}

func htmlToString(html *goquery.Selection) string {
	notBrLast := false
	var buffer strings.Builder
	walkselection(html, &buffer, &notBrLast)
	return buffer.String()
}

func noscriptToString(noscript *goquery.Selection) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(noscript.Text()))
	if err != nil {
		log.Println("Failed to parse content from selection :", err)
		return ""
	}

	notBrLast := false
	var buffer strings.Builder
	walkselection(doc.Find("body"), &buffer, &notBrLast)
	return buffer.String()
}

func walkselection(parent *goquery.Selection, buffer *strings.Builder, notBrLast *bool) {
	parent.Each(func(i int, s *goquery.Selection) {
		switch goquery.NodeName(s) {
		case "br":
			if *notBrLast {
				*notBrLast = false
				buffer.WriteByte('\n')
			}
		case "#text":
			*notBrLast = true
			buffer.WriteString(s.Text())
		default:
			walkselection(s.Contents(), buffer, notBrLast)
		}
	})
}
