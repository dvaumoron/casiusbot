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
	"log"
	"os"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/dvaumoron/casiusbot/common"
)

const defaultKey = "default"

func manageChatResponse(s *discordgo.Session, u *discordgo.MessageCreate, botId string, channelManager common.ChannelSenderManager, keywordToResponse map[string]string, keywordToResponseMutex *sync.RWMutex) {
	for _, user := range u.Mentions {
		if user.ID == botId {
			if response, ok := chooseResponse(u.Content, keywordToResponse, keywordToResponseMutex); ok {
				channelId := u.ChannelID
				channelManager.AddChannel(channelId)
				channelManager.Get(channelId) <- common.MultipartMessage{Message: response}
			}
			return
		}
	}
}

func chooseResponse(content string, keywordToResponse map[string]string, keywordToResponseMutex *sync.RWMutex) (string, bool) {
	contentLower := strings.ToLower(content)

	keywordToResponseMutex.RLock()
	defer keywordToResponseMutex.RUnlock()
	for keyword, response := range keywordToResponse {
		if strings.Contains(contentLower, keyword) {
			return response, true
		}

	}
	response, ok := keywordToResponse[defaultKey]
	return response, ok
}

func registerChatResponseCmd(s *discordgo.Session, i *discordgo.InteractionCreate, chatReponsePath string, keywordToResponse map[string]string, keywordToResponseMutex *sync.RWMutex, infos common.GuildAndConfInfo) {
	common.AuthorizedCmd(s, i, infos, func() string {
		options := i.ApplicationCommandData().Options
		if optionsLen := len(options); optionsLen != 0 {
			keyword := strings.ToLower(options[0].StringValue())
			response := ""
			if optionsLen > 1 {
				response = strings.TrimSpace(options[1].StringValue())
			}

			keywordToResponseMutex.Lock()
			if response == "" {
				delete(keywordToResponse, keyword)
			} else {
				keywordToResponse[keyword] = response
			}
			keywordToResponseMutex.Unlock()

			data, err := json.Marshal(keywordToResponse)
			if err == nil {
				if err = os.WriteFile(chatReponsePath, data, 0644); err == nil {
					return infos.Msgs.Ok
				} else {
					log.Println("Fail to save chat responses data :", err)
				}
			} else {
				log.Println("Fail to marshal chat responses data :", err)
			}
		}
		return infos.Msgs.ErrGlobal
	})
}

func displayChatResponseCmd(s *discordgo.Session, i *discordgo.InteractionCreate, baseMsg string, keywordToResponse map[string]string, keywordToResponseMutex *sync.RWMutex, infos common.GuildAndConfInfo) {
	common.AuthorizedCmd(s, i, infos, func() string {
		keywordToResponseMutex.RLock()
		defer keywordToResponseMutex.RUnlock()
		return common.BuildMsgWithNameValueList(baseMsg, keywordToResponse)
	})
}
