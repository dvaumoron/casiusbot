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

func addRoleCmd(s *discordgo.Session, i *discordgo.InteractionCreate, addedRoleId string, infos GuildAndConfInfo, userMonitor *IdMonitor) {
	returnMsg := infos.msgs[0]
	if idInSet(i.Member.Roles, infos.forbiddenRoleIds) {
		returnMsg = infos.msgs[1]
	} else if userId := i.Member.User.ID; userId == infos.ownerId {
		returnMsg = infos.msgs[9]
	} else if userMonitor.StartProcessing(userId) {
		defer userMonitor.StopProcessing(userId)

		messageQueue := make(chan string, 1)
		if counterError := addRole(s, messageQueue, i.Member, addedRoleId, infos, true); counterError == 0 {
			returnMsg = <-messageQueue
		} else {
			returnMsg = infos.msgs[8] + strconv.Itoa(counterError)
		}
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}

func addRole(s *discordgo.Session, messageSender chan<- string, member *discordgo.Member, addedRoleId string, infos GuildAndConfInfo, forceSend bool) int {
	toAdd := true
	counterError := 0
	userId := member.User.ID
	for _, roleId := range member.Roles {
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
		if err := s.GuildMemberRoleAdd(infos.guildId, userId, addedRoleId); err != nil {
			log.Println("Prefix role addition failed :", err)
			counterError++
		}
	}

	if member, err := s.GuildMember(infos.guildId, userId); err == nil {
		counterError += applyPrefix(s, messageSender, member, infos, forceSend)
	} else {
		log.Println("Cannot retrieve member :", err)
		counterError++
	}
	return counterError
}

func countRoleCmd(s *discordgo.Session, i *discordgo.InteractionCreate, roleCountExtracter func([]*discordgo.Member) map[string]int, infos GuildAndConfInfo) {
	returnMsg := infos.msgs[2]
	if guildMembers, err := s.GuildMembers(i.GuildID, "", 1000); err == nil {
		roleNameToCountStr := map[string]string{}
		for roleId, count := range roleCountExtracter(guildMembers) {
			roleNameToCountStr[infos.roleIdToDisplayName[roleId]] = strconv.Itoa(count)
		}
		returnMsg = buildMsgWithNameValueList(infos.msgs[4], roleNameToCountStr)
	} else {
		log.Println("Cannot retrieve guild members (2) :", err)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}

func extractRoleCount(guildMembers []*discordgo.Member) map[string]int {
	roleIdToCount := map[string]int{}
	for _, guildMember := range guildMembers {
		for _, roleId := range guildMember.Roles {
			roleIdToCount[roleId]++
		}
	}
	return roleIdToCount
}

func extractRoleCountWithFilter(guildMembers []*discordgo.Member, filterRoleIds map[string]empty) map[string]int {
	roleIdToCount := map[string]int{}
	for _, guildMember := range guildMembers {
		for _, roleId := range guildMember.Roles {
			if _, ok := filterRoleIds[roleId]; ok {
				roleIdToCount[roleId]++
			}
		}
	}
	return roleIdToCount
}

func resetRoleAll(s *discordgo.Session, guildMembers []*discordgo.Member, infos GuildAndConfInfo, userMonitor *IdMonitor) int {
	counterError := 0
	for _, guildMember := range guildMembers {
		userId := guildMember.User.ID
		if userId != infos.ownerId && !idInSet(guildMember.Roles, infos.forbiddenAndignoredRoleIds) {
			if userMonitor.StartProcessing(userId) {
				counterError += addRole(s, nil, guildMember, infos.defaultRoleId, infos, false)
				userMonitor.StopProcessing(userId)
			}
		}
	}
	return counterError
}
