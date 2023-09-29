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
	"github.com/dvaumoron/casiusbot/common"
)

func addRoleCmd(s *discordgo.Session, i *discordgo.InteractionCreate, addedRoleId string, infos common.GuildAndConfInfo, userMonitor *common.IdMonitor) {
	returnMsg := infos.Msgs.Ok
	if common.IdInSet(i.Member.Roles, infos.ForbiddenRoleIds) {
		returnMsg = infos.Msgs.ErrUnauthorized
	} else if userId := i.Member.User.ID; userId == infos.OwnerId {
		returnMsg = infos.Msgs.Owner
	} else if userMonitor.StartProcessing(userId) {
		defer userMonitor.StopProcessing(userId)

		messageQueue := make(chan common.MultipartMessage, 1)
		if counterError := addRole(s, messageQueue, true, addedRoleId, infos, i.Member); counterError == 0 {
			returnMsg = (<-messageQueue).Message
		} else {
			returnMsg = strings.ReplaceAll(infos.Msgs.ErrPartial, common.NumErrorPlaceHolder, strconv.Itoa(counterError))
		}
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}

func addRole(s *discordgo.Session, messageSender chan<- common.MultipartMessage, forceSend bool, addedRoleId string, infos common.GuildAndConfInfo, member *discordgo.Member) int {
	toAdd := true
	counterError := 0
	userId := member.User.ID
	for _, roleId := range member.Roles {
		if roleId == addedRoleId {
			toAdd = false
			continue
		}

		if _, ok := infos.CmdRoleIds[roleId]; ok {
			if err := s.GuildMemberRoleRemove(infos.GuildId, userId, roleId); err != nil {
				log.Println("Prefix role removing failed :", err)
				counterError++
			}
		}
	}

	if toAdd {
		if err := s.GuildMemberRoleAdd(infos.GuildId, userId, addedRoleId); err != nil {
			log.Println("Prefix role addition failed :", err)
			counterError++
		}
	}

	if member, err := s.GuildMember(infos.GuildId, userId); err == nil {
		counterError += applyPrefix(s, messageSender, forceSend, infos, member)
	} else {
		log.Println("Cannot retrieve member :", err)
		counterError++
	}
	return counterError
}

func countRoleCmd(s *discordgo.Session, i *discordgo.InteractionCreate, roleCountExtracter func([]*discordgo.Member) map[string]int, infos common.GuildAndConfInfo) {
	returnMsg := infos.Msgs.ErrGlobalCmd
	if guildMembers, err := s.GuildMembers(i.GuildID, "", common.MemberCallLimit); err == nil {
		roleNameToCountStr := map[string]string{}
		for roleId, count := range roleCountExtracter(guildMembers) {
			roleNameToCountStr[infos.RoleIdToDisplayName[roleId]] = strconv.Itoa(count)
		}
		returnMsg = common.BuildMsgWithNameValueList(infos.Msgs.Count, roleNameToCountStr)
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

func extractRoleCountWithFilter(guildMembers []*discordgo.Member, filterRoleIds common.StringSet) map[string]int {
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

func resetRole(s *discordgo.Session, infos common.GuildAndConfInfo, guildMember *discordgo.Member) int {
	userId := guildMember.User.ID
	if userId != infos.OwnerId && !common.IdInSet(guildMember.Roles, infos.ForbiddenAndignoredRoleIds) {
		return addRole(s, nil, false, infos.DefaultRoleId, infos, guildMember)
	}
	return 0
}
