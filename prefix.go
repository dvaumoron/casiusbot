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
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/dvaumoron/casiusbot/common"
)

const (
	NOTHING uint8 = iota
	ADD_DEFAULT
	REMOVE_DEFAULT
	REMOVE_ALL
)

func transformNick(nickName string, roleIds []string, info common.GuildAndConfInfo) (string, string, uint8) {
	cleanedNickName := cleanPrefixInNick(nickName, info.Prefixes)
	nickName = cleanedNickName
	// usedRoleId is meant for the message of role commands when there is no prefix change
	// in case of useless reset command we need to indicate to the user it has already the default role
	usedRoleId, hasDefault, hasPrefix, notDone := info.DefaultRoleId, false, false, true
	for _, roleId := range roleIds {
		if _, ok := info.ForbiddenRoleIds[roleId]; ok {
			// not adding prefix nor default role for user with forbidden role
			return cleanedNickName, roleId, REMOVE_ALL
		}
		if roleId == info.DefaultRoleId {
			hasDefault = true
		}
		if prefix, ok := info.RoleIdToPrefix[roleId]; ok {
			hasPrefix = true
			_, special := info.SpecialRoleIds[roleId]
			if notDone || special {
				notDone = false
				usedRoleId = roleId
				// here prefix already end with a space
				nickName = prefix + cleanedNickName
			}
		}
	}
	action := NOTHING
	if hasDefault {
		if hasPrefix {
			action = REMOVE_DEFAULT
		}
	} else if !hasPrefix {
		action = ADD_DEFAULT
	}
	return nickName, usedRoleId, action
}

func cleanPrefixInNick(nick string, prefixes []string) string {
	for _, prefix := range prefixes {
		if strings.HasPrefix(nick, prefix) {
			// here prefix have no space added at its end
			return strings.TrimSpace(nick[len(prefix):])
		}
	}
	return nick
}

func applyPrefix(s *discordgo.Session, messageSender chan<- common.MultipartMessage, forceSend bool, infos common.GuildAndConfInfo, member *discordgo.Member) int {
	counterError := 0
	userId := member.User.ID
	roleIds := member.Roles
	if userId != infos.OwnerId && !common.IdInSet(roleIds, infos.IgnoredRoleIds) {
		nick := common.ExtractNick(member)
		newNick, usedRoleId, actionOnRoles := transformNick(nick, roleIds, infos)
		switch actionOnRoles {
		case ADD_DEFAULT:
			if err := s.GuildMemberRoleAdd(infos.GuildId, userId, infos.DefaultRoleId); err != nil {
				log.Println("Role addition failed :", err)
				counterError++
			}
		case REMOVE_DEFAULT:
			if err := s.GuildMemberRoleRemove(infos.GuildId, userId, infos.DefaultRoleId); err != nil {
				log.Println("Role removing failed :", err)
				counterError++
			}
		case REMOVE_ALL:
			for _, roleId := range roleIds {
				if _, ok := infos.RoleIdToPrefix[roleId]; ok || roleId == infos.DefaultRoleId {
					if err := s.GuildMemberRoleRemove(infos.GuildId, userId, roleId); err != nil {
						log.Println("Role removing failed (2) :", err)
						counterError++
					}
				}
			}
		}
		if newNick == nick {
			if forceSend && messageSender != nil {
				msg := strings.ReplaceAll(infos.Msgs.NoChange, "{{user}}", nick)
				msg = strings.ReplaceAll(msg, common.RolePlaceHolder, infos.RoleIdToDisplayName[usedRoleId])
				messageSender <- common.MultipartMessage{Message: msg}
			}
		} else {
			if err := s.GuildMemberNickname(infos.GuildId, userId, newNick); err == nil {
				if messageSender != nil {
					msg := strings.ReplaceAll(infos.Msgs.Prefix, "{{old}}", nick)
					msg = strings.ReplaceAll(msg, "{{new}}", newNick)
					messageSender <- common.MultipartMessage{Message: msg}
				}
			} else {
				log.Println("Nickname change failed (2) :", err)
				counterError++
			}
		}
	}
	return counterError
}

func cleanPrefix(s *discordgo.Session, infos common.GuildAndConfInfo, guildMember *discordgo.Member) int {
	counterError := 0
	if userId := guildMember.User.ID; userId != infos.OwnerId {
		nick := common.ExtractNick(guildMember)
		newNick := cleanPrefixInNick(nick, infos.Prefixes)
		if newNick != nick {
			if err := s.GuildMemberNickname(infos.GuildId, userId, newNick); err != nil {
				log.Println("Nickname change failed :", err)
				counterError++
			}
		}
	}
	return counterError
}
