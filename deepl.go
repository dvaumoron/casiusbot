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
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

var errCast = errors.New("failed to cast the retrieved result")
var errNoResult = errors.New("no translation in retrieved result")
var errLimit = errors.New("exceded free translate credit limit")

type DeepLClient struct {
	usageUrl     string
	translateUrl string
	token        string
	sourceLang   string
	targetLang   string
	messageError string
	messageLimit string
}

func makeDeepLClient(baseUrl string, token string, sourceLang string, targetLang string, messageError string, messageLimit string) (DeepLClient, error) {
	usageUrl, err := url.JoinPath(baseUrl, "v2/usage")
	if err != nil {
		return DeepLClient{}, err
	}

	translateUrl, err := url.JoinPath(baseUrl, "v2/translate")
	if err != nil {
		return DeepLClient{}, err
	}

	res := DeepLClient{
		usageUrl: usageUrl, translateUrl: translateUrl, token: "DeepL-Auth-Key " + token,
		sourceLang: sourceLang, targetLang: targetLang, messageError: messageError, messageLimit: messageLimit,
	}
	return res, res.checkUsage(1)
}

func (c DeepLClient) Translate(msg string) string {
	msgSize := len(msg)
	if msgSize == 0 {
		log.Println("Empty message : No translation")
		return c.messageError
	}

	err := c.checkUsage(msgSize)
	if err != nil {
		if err == errLimit {
			return c.messageLimit
		} else {
			log.Println("Failed to check free translation credit :", err)
			return c.messageError
		}
	}

	res, err := c.innerTranslate(msg)
	if err != nil {
		log.Println("Failed to translate :", err)
		return c.messageError
	}
	return res
}

func (c DeepLClient) checkUsage(size int) error {
	req, err := http.NewRequest(http.MethodGet, c.usageUrl, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", c.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var parsed map[string]any
	err = json.Unmarshal(body, &parsed)
	if err != nil {
		return err
	}

	count, ok := parsed["character_count"].(float64)
	if !ok {
		return errCast
	}

	limit, ok := parsed["character_limit"].(float64)
	if !ok {
		return errCast
	}

	if int(limit-count) < size {
		return errLimit
	}
	return nil
}

func (c DeepLClient) innerTranslate(msg string) (string, error) {
	data := url.Values{"text": {msg}, "target_lang": {c.targetLang}}
	if c.sourceLang != "" {
		data.Add("source_lang", c.sourceLang)
	}

	req, err := http.NewRequest(http.MethodPost, c.translateUrl, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", c.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var parsed map[string]any
	err = json.Unmarshal(body, &parsed)
	if err != nil {
		return "", err
	}

	castedTranslations, ok := parsed["translations"].([]any)
	if !ok {
		return "", errCast
	}

	if len(castedTranslations) == 0 {
		return "", errNoResult
	}

	castedTranslation, ok := castedTranslations[0].(map[string]any)
	if !ok {
		return "", errCast
	}

	castedText, ok := castedTranslation["text"].(string)
	if !ok {
		return "", errCast
	}
	return castedText, nil
}
