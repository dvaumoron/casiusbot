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

	"github.com/bwmarrin/discordgo"
)

func initRoleChannelCleaning(session *discordgo.Session, guildMembers []*discordgo.Member, roleChannelId string, initSize int) {
	memberIdSet := map[string]empty{}
	for _, member := range guildMembers {
		memberIdSet[member.User.ID] = empty{}
	}

	messages, err := session.ChannelMessagesPinned(roleChannelId)
	if err != nil || len(messages) == 0 {
		log.Println("Cannot retrieve the pinned messages in roleChannel :", err)
		return
	}
	message := messages[0]
	// emptying data no longer useful for GC cleaning
	messages = nil

	roleMessageId := message.ID
	roleEmojiIds := make([]string, 0, initSize)
	for _, reaction := range message.Reactions {
		emojiId := reaction.Emoji.ID
		roleEmojiIds = append(roleEmojiIds, emojiId)

		users, err := session.MessageReactions(roleChannelId, roleMessageId, emojiId, 100, "", "")
		if err != nil {
			log.Println("Cannot retrieve the reaction on the roleMessage :", err)
			return
		}
		for _, user := range users {
			userId := user.ID
			if _, ok := memberIdSet[userId]; !ok {
				if err = session.MessageReactionRemove(roleChannelId, roleMessageId, emojiId, userId); err != nil {
					log.Println("Cannot remove user reaction :", err)
				}
			}
		}
	}

	session.AddHandler(func(s *discordgo.Session, r *discordgo.GuildMemberRemove) {
		userId := r.User.ID
		var err error
		for _, emojiId := range roleEmojiIds {
			if err = s.MessageReactionRemove(roleChannelId, roleMessageId, emojiId, userId); err != nil {
				log.Println("Cannot remove user reaction (2) :", err)
			}
		}
	})
}
