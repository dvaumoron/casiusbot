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
	"strconv"

	"github.com/bwmarrin/discordgo"
)

func addRoleCmd(s *discordgo.Session, i *discordgo.InteractionCreate, ownerId string, addedRoleId string, specialRoleIds map[string]empty, forbiddenRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string, cmdworking *boolAtom, msgs [5]string) {
	guildId := i.GuildID
	returnMsg := msgs[0]
	roleIds := i.Member.Roles
	if idInSet(roleIds, forbiddenRoleIds) {
		returnMsg = msgs[1]
	} else if userId := i.Member.User.ID; userId != ownerId && !cmdworking.Get() {
		toAdd := true
		for _, roleId := range roleIds {
			if roleId == addedRoleId {
				toAdd = false
				continue
			}

			if _, ok := roleIdToPrefix[roleId]; ok {
				if _, ok := specialRoleIds[roleId]; !ok {
					if err := s.GuildMemberRoleRemove(guildId, userId, roleId); err != nil {
						log.Println("Prefix role removing failed :", err)
						returnMsg = msgs[2]
					}
				}
			}
		}

		if toAdd {
			if err := s.GuildMemberRoleAdd(guildId, userId, addedRoleId); err != nil {
				log.Println("Prefix role addition failed :", err)
				returnMsg = msgs[2]
			}
		}
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}

func countRoleCmd(s *discordgo.Session, i *discordgo.InteractionCreate, roleIdToDisplayName map[string]string, msgs [5]string) {
	returnMsg := msgs[2]
	if guildMembers, err := s.GuildMembers(i.GuildID, "", 1000); err == nil {
		roleIdToCount := map[string]int{}
		for _, guildMember := range guildMembers {
			for _, roleId := range guildMember.Roles {
				roleIdToCount[roleId]++
			}
		}
		roleNameToCountStr := map[string]string{}
		for roleId, count := range roleIdToCount {
			roleNameToCountStr[roleIdToDisplayName[roleId]] = strconv.Itoa(count)
		}
		returnMsg = buildMsgWithNameValueList(msgs[4], roleNameToCountStr)
	} else {
		log.Println("Members retrieving failed (2) :", err)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}
