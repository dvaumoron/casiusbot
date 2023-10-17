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

func manageChatResponse(s *discordgo.Session, u *discordgo.MessageCreate, botId string, channelManager common.ChannelSenderManager, keywordToResponse map[string]string, keywordToResponseMutex *sync.RWMutex) {
	for _, user := range u.Mentions {
		if user.ID == botId {
			content := u.Content
			keywordToResponseMutex.RLock()
			defer keywordToResponseMutex.RUnlock()
			for keyword, response := range keywordToResponse {
				if strings.Contains(content, keyword) {
					channelId := u.ChannelID
					channelManager.AddChannel(channelId)
					channelManager.Get(channelId) <- common.MultipartMessage{Message: response}
					break
				}
			}
			break
		}
	}
}

func registerChatResponseCmd(s *discordgo.Session, i *discordgo.InteractionCreate, chatReponsePath string, keywordToResponse map[string]string, keywordToResponseMutex *sync.RWMutex, infos common.GuildAndConfInfo) {
	common.AuthorizedCmd(s, i, infos, func() string {
		if options := i.ApplicationCommandData().Options; len(options) > 1 {
			keyword := options[0].StringValue()
			response := options[1].StringValue()

			keywordToResponseMutex.Lock()
			keywordToResponse[keyword] = response
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
