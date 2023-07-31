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
	"sync"

	"github.com/bwmarrin/discordgo"
)

type Monitor struct {
	running bool
	mutex   sync.RWMutex
}

func (m *Monitor) Running() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.running
}

func (m *Monitor) Stop() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.running = false
}

func (m *Monitor) Start() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.running {
		return false
	}
	m.running = true
	return true
}

func readPrefixConfig(filePathName string) (map[string]string, []string, map[string]string, []string, error) {
	file, err := os.Open(os.Getenv(filePathName))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	nameToPrefix := map[string]string{}
	prefixes := make([]string, 0)
	cmdToName := map[string]string{}
	specialRoles := make([]string, 0)
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
						cmdToName[strings.TrimSpace(splitted[2])] = name
					} else {
						specialRoles = append(specialRoles, name)
					}
				}
			}
		}
	}

	if err = scanner.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	return nameToPrefix, prefixes, cmdToName, specialRoles, nil
}

func membersCmd(s *discordgo.Session, messageSender chan<- string, cmdName string, guildId string, authorized bool, msgs [9]string, interaction *discordgo.Interaction, cmdMonitor *Monitor, cmdEffect func([]*discordgo.Member) int) {
	returnMsg := msgs[0]
	if authorized {
		if cmdMonitor.Start() {
			go processMembers(s, messageSender, cmdName, guildId, msgs, cmdMonitor, cmdEffect)
		} else {
			returnMsg = msgs[8]
		}
	} else {
		returnMsg = msgs[1]
	}

	s.InteractionRespond(interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}

func processMembers(s *discordgo.Session, messageSender chan<- string, cmdName string, guildId string, msgs [9]string, cmdMonitor *Monitor, cmdEffect func([]*discordgo.Member) int) {
	defer cmdMonitor.Stop()
	msg := msgs[2]
	if guildMembers, err := s.GuildMembers(guildId, "", 1000); err == nil {
		if counterError := cmdEffect(guildMembers); counterError == 0 {
			msg = msgs[7]
		} else {
			msg = msgs[3] + strconv.Itoa(counterError)
		}
	} else {
		log.Println("Members retrieving failed :", err)
	}
	messageSender <- strings.ReplaceAll(msg, "{{cmd}}", cmdName)
}

func transformNick(nickName string, roleIds []string, defaultRoleId string, specialRoleIds map[string]empty, forbiddenRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string) (string, bool, bool) {
	cleanedNickName := cleanPrefixInNick(nickName, prefixes)
	nickName = cleanedNickName
	hasDefault, hasPrefix, notDone := false, false, true
	for _, roleId := range roleIds {
		if _, ok := forbiddenRoleIds[roleId]; ok {
			// not adding prefix nor default role for user with forbidden role
			return cleanedNickName, true, true
		}
		if roleId == defaultRoleId {
			hasDefault = true
		}
		if prefix, ok := roleIdToPrefix[roleId]; ok {
			hasPrefix = true
			_, special := specialRoleIds[roleId]
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

func applyPrefixes(s *discordgo.Session, guildMembers []*discordgo.Member, guildId string, ownerId string, defaultRoleId string, ignoredRoleIds map[string]empty, specialRoleIds map[string]empty, forbiddenRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string, msgs [9]string) int {
	counterError := 0
	for _, guildMember := range guildMembers {
		counterError += applyPrefix(s, nil, guildMember, guildId, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes, msgs)
	}
	return counterError
}

func applyPrefix(s *discordgo.Session, messageSender chan<- string, guildMember *discordgo.Member, guildId string, ownerId string, defaultRoleId string, ignoredRoleIds map[string]empty, specialRoleIds map[string]empty, forbiddenRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string, msgs [9]string) int {
	counterError := 0
	userId := guildMember.User.ID
	roleIds := guildMember.Roles
	if userId != ownerId && !idInSet(roleIds, ignoredRoleIds) {
		nick := extractNick(guildMember)
		newNick, hasDefault, hasPrefix := transformNick(nick, roleIds, defaultRoleId, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes)
		if hasDefault {
			if hasPrefix {
				if err := s.GuildMemberRoleRemove(guildId, userId, defaultRoleId); err != nil {
					log.Println("Role removing failed :", err)
					counterError++
				}
			}
		} else if !hasPrefix {
			if err := s.GuildMemberRoleAdd(guildId, userId, defaultRoleId); err != nil {
				log.Println("Role addition failed :", err)
				counterError++
			}
		}
		if newNick != nick {
			if err := s.GuildMemberNickname(guildId, userId, newNick); err == nil {
				if messageSender != nil {
					msg := strings.ReplaceAll(msgs[5], "{{old}}", nick)
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

func cleanPrefixes(s *discordgo.Session, guildMembers []*discordgo.Member, guildId string, ownerId string, prefixes []string) int {
	counterError := 0
	for _, guildMember := range guildMembers {
		counterError += cleanPrefix(s, guildMember, guildId, ownerId, prefixes)
	}
	return counterError
}

func cleanPrefix(s *discordgo.Session, guildMember *discordgo.Member, guildId string, ownerId string, prefixes []string) int {
	if userId := guildMember.User.ID; userId != ownerId {
		nick := extractNick(guildMember)
		newNick := cleanPrefixInNick(nick, prefixes)
		if newNick != nick {
			if err := s.GuildMemberNickname(guildId, userId, newNick); err != nil {
				log.Println("Nickname change failed :", err)
				return 1
			}
		}
	}
	return 0
}
