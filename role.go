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
	"strings"

	"github.com/bwmarrin/discordgo"
)

func addRoleCmd(s *discordgo.Session, i *discordgo.InteractionCreate, addedRoleId string, addedRoleDisplayName string, infos GuildAndConfInfo, userMonitor *IdMonitor) {
	returnMsg := infos.msgs[0]
	if idInSet(i.Member.Roles, infos.forbiddenRoleIds) {
		returnMsg = infos.msgs[1]
	} else if userId := i.Member.User.ID; userId != infos.ownerId && userMonitor.StartProcessing(userId) {
		defer userMonitor.StopProcessing(userId)
		toAdd := true
		counterError := 0
		for _, roleId := range i.Member.Roles {
			if roleId == addedRoleId {
				toAdd = false
				continue
			}

			if _, ok := infos.cmdRoleIds[roleId]; ok {
				if err := s.GuildMemberRoleRemove(infos.guildId, userId, roleId); err != nil {
					log.Println("Prefix role removing failed :", err)
					counterError++
				}
			}
		}

		if toAdd {
			if err := s.GuildMemberRoleAdd(infos.guildId, userId, addedRoleId); err == nil {
				messageQueue := make(chan string, 1)
				counterError += applyPrefix(s, messageQueue, i.Member, infos)
				if counterError == 0 {
					returnMsg = <-messageQueue
				}
			} else {
				log.Println("Prefix role addition failed :", err)
				counterError++
			}
		} else {
			msg := strings.ReplaceAll(infos.msgs[6], "{{user}}", i.Member.Nick)
			msg = strings.ReplaceAll(msg, "{{role}}", addedRoleDisplayName)
			returnMsg = msg
		}

		if counterError != 0 {
			returnMsg = infos.msgs[8] + strconv.Itoa(counterError)
		}
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}

func countRoleCmd(s *discordgo.Session, i *discordgo.InteractionCreate, filter bool, filterRoleIds map[string]empty, infos GuildAndConfInfo) {
	returnMsg := infos.msgs[2]
	if guildMembers, err := s.GuildMembers(i.GuildID, "", 1000); err == nil {
		roleIdToCount := map[string]int{}
		for _, guildMember := range guildMembers {
			for _, roleId := range guildMember.Roles {
				if filter {
					if _, ok := filterRoleIds[roleId]; !ok {
						continue
					}
				}
				roleIdToCount[roleId]++
			}
		}
		roleNameToCountStr := map[string]string{}
		for roleId, count := range roleIdToCount {
			roleNameToCountStr[infos.roleIdToDisplayName[roleId]] = strconv.Itoa(count)
		}
		returnMsg = buildMsgWithNameValueList(infos.msgs[4], roleNameToCountStr)
	} else {
		log.Println("Members retrieving failed (2) :", err)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}
