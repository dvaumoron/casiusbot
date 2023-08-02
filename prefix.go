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
	"bufio"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func readPrefixConfig(filePathName string) (map[string]string, []string, [][2]string, []string) {
	file, err := os.Open(os.Getenv(filePathName))
	if err != nil {
		log.Fatalln("Cannot read the configuration file :", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	nameToPrefix := map[string]string{}
	prefixes := []string{}
	cmdAndNames := [][2]string{}
	specialRoles := []string{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && line[0] != '#' {
			splitted := strings.Split(line, ":")
			if splittedSize := len(splitted); splittedSize > 1 {
				name := strings.TrimSpace(splitted[0])
				if name != "" {
					prefix := strings.TrimSpace(splitted[1]) + " "

					nameToPrefix[name] = prefix
					prefixes = append(prefixes, prefix)

					if splittedSize > 2 {
						cmd := strings.TrimSpace(splitted[2])
						if cmd == "" {
							log.Fatalln("Malformed configuration file, empty command")
						}
						cmdAndNames = append(cmdAndNames, [2]string{cmd, name})
					} else {
						specialRoles = append(specialRoles, name)
					}
				}
			}
		}
	}

	if err = scanner.Err(); err != nil {
		log.Fatalln("Cannot parse the configuration file :", err)
	}
	return nameToPrefix, prefixes, cmdAndNames, specialRoles
}

func membersCmd(s *discordgo.Session, i *discordgo.InteractionCreate, messageSender chan<- string, cmdName string, infos GuildAndConfInfo, cmdEffect func([]*discordgo.Member) int) {
	returnMsg := infos.msgs[0]
	if idInSet(i.Member.Roles, infos.authorizedRoleIds) {
		go processMembers(s, messageSender, cmdName, infos, cmdEffect)
	} else {
		returnMsg = infos.msgs[1]
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}

func processMembers(s *discordgo.Session, messageSender chan<- string, cmdName string, infos GuildAndConfInfo, cmdEffect func([]*discordgo.Member) int) {
	msg := infos.msgs[2]
	if guildMembers, err := s.GuildMembers(infos.guildId, "", 1000); err == nil {
		if counterError := cmdEffect(guildMembers); counterError == 0 {
			msg = infos.msgs[7]
		} else {
			msg = infos.msgs[3] + strconv.Itoa(counterError)
		}
	} else {
		log.Println("Members retrieving failed :", err)
	}
	messageSender <- strings.ReplaceAll(msg, "{{cmd}}", cmdName)
}

func transformNick(nickName string, roleIds []string, info GuildAndConfInfo) (string, bool, bool) {
	cleanedNickName := cleanPrefixInNick(nickName, info.prefixes)
	nickName = cleanedNickName
	hasDefault, hasPrefix, notDone := false, false, true
	for _, roleId := range roleIds {
		if _, ok := info.forbiddenRoleIds[roleId]; ok {
			// not adding prefix nor default role for user with forbidden role
			return cleanedNickName, true, true
		}
		if roleId == info.defaultRoleId {
			hasDefault = true
		}
		if prefix, ok := info.roleIdToPrefix[roleId]; ok {
			hasPrefix = true
			_, special := info.specialRoleIds[roleId]
			if notDone || special {
				notDone = false
				// prefix already end with a space
				nickName = prefix + cleanedNickName
			}
		}
	}
	return nickName, hasDefault, hasPrefix
}

func cleanPrefixInNick(nick string, prefixes []string) string {
	for _, prefix := range prefixes {
		if strings.HasPrefix(nick, prefix) {
			return nick[len(prefix):]
		}
	}
	return nick
}

func applyPrefixes(s *discordgo.Session, guildMembers []*discordgo.Member, infos GuildAndConfInfo, userMonitor *IdMonitor) int {
	counterError := 0
	for _, guildMember := range guildMembers {
		userId := guildMember.User.ID
		if userMonitor.StartProcessing(userId) {
			counterError += applyPrefix(s, nil, guildMember, infos)
			userMonitor.StopProcessing(userId)
		}
	}
	return counterError
}

func applyPrefix(s *discordgo.Session, messageSender chan<- string, member *discordgo.Member, infos GuildAndConfInfo) int {
	counterError := 0
	userId := member.User.ID
	roleIds := member.Roles
	if userId != infos.ownerId && !idInSet(roleIds, infos.ignoredRoleIds) {
		nick := extractNick(member)
		newNick, hasDefault, hasPrefix := transformNick(nick, roleIds, infos)
		if hasDefault {
			if hasPrefix {
				if err := s.GuildMemberRoleRemove(infos.guildId, userId, infos.defaultRoleId); err != nil {
					log.Println("Role removing failed :", err)
					counterError++
				}
			}
		} else if !hasPrefix {
			if err := s.GuildMemberRoleAdd(infos.guildId, userId, infos.defaultRoleId); err != nil {
				log.Println("Role addition failed :", err)
				counterError++
			}
		}
		if newNick != nick {
			if err := s.GuildMemberNickname(infos.guildId, userId, newNick); err == nil {
				if messageSender != nil {
					msg := strings.ReplaceAll(infos.msgs[5], "{{old}}", nick)
					msg = strings.ReplaceAll(msg, "{{new}}", newNick)
					messageSender <- msg
				}
			} else {
				log.Println("Nickname change failed (2) :", err)
				counterError++
			}
		}
	}
	return counterError
}

func cleanPrefixes(s *discordgo.Session, guildMembers []*discordgo.Member, infos GuildAndConfInfo, userMonitor *IdMonitor) int {
	counterError := 0
	for _, guildMember := range guildMembers {
		if userId := guildMember.User.ID; userId != infos.ownerId && userMonitor.StartProcessing(userId) {
			nick := extractNick(guildMember)
			newNick := cleanPrefixInNick(nick, infos.prefixes)
			if newNick != nick {
				if err := s.GuildMemberNickname(infos.guildId, userId, newNick); err != nil {
					log.Println("Nickname change failed :", err)
					counterError++
				}
			}
			userMonitor.StopProcessing(userId)
		}
	}
	return counterError
}
