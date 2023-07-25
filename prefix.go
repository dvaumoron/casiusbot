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
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
)

type boolAtom struct {
	value bool
	mutex sync.RWMutex
}

func (b *boolAtom) Get() bool {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return b.value
}

func (b *boolAtom) Set(newValue bool) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.value = newValue
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
		if len(line) != 0 && line[0] != '#' {
			splitted := strings.Split(line, ":")
			name := strings.TrimSpace(splitted[0])
			if splittedSize := len(splitted); splittedSize > 1 && name != "" {
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

	if err = scanner.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	return nameToPrefix, prefixes, cmdToName, specialRoles, nil
}

func membersCmd(s *discordgo.Session, guildId string, authorized bool, okCmdMsg string, errPartialCmdMsg string, errGlobalCmdMsg string, errUnauthorizedCmdMsg string, interaction *discordgo.Interaction, cmdEffect func([]*discordgo.Member) int) {
	returnMsg := okCmdMsg
	if authorized {
		if guildMembers, err := s.GuildMembers(guildId, "", 1000); err == nil {
			if counterError := cmdEffect(guildMembers); counterError != 0 {
				returnMsg = errPartialCmdMsg + strconv.Itoa(counterError)
			}
		} else {
			log.Println("Members retrieving failed :", err)
			returnMsg = errGlobalCmdMsg
		}
	} else {
		returnMsg = errUnauthorizedCmdMsg
	}

	s.InteractionRespond(interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}

func transformName(nickName string, roleIds []string, defaultRoleId string, specialRoleIds map[string]empty, forbiddenRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string) (string, bool, bool) {
	cleanedNickName := cleanPrefixInNickName(nickName, prefixes)
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

func cleanPrefixInNickName(nickName string, prefixes []string) string {
	for _, prefix := range prefixes {
		if strings.HasPrefix(nickName, prefix) {
			return nickName[len(prefix):]
		}
	}
	return nickName
}

func applyPrefixes(s *discordgo.Session, guildMembers []*discordgo.Member, guildId string, ownerId string, defaultRoleId string, ignoredRoleIds map[string]empty, specialRoleIds map[string]empty, forbiddenRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string, cmdworking *boolAtom) int {
	counterError := 0
	cmdworking.Set(true)
	for _, guildMember := range guildMembers {
		counterError += applyPrefix(s, guildMember, guildId, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes)
	}
	cmdworking.Set(false)
	return counterError
}

func applyPrefix(s *discordgo.Session, guildMember *discordgo.Member, guildId string, ownerId string, defaultRoleId string, ignoredRoleIds map[string]empty, specialRoleIds map[string]empty, forbiddenRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string) int {
	counterError := 0
	userId := guildMember.User.ID
	roleIds := guildMember.Roles
	if userId != ownerId && !roleIdInSet(roleIds, ignoredRoleIds) {
		nickName := guildMember.Nick
		if nickName == "" {
			nickName = guildMember.User.Username
		}

		newNickName, hasDefault, hasPrefix := transformName(nickName, roleIds, defaultRoleId, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes)
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
		if newNickName != nickName {
			if err := s.GuildMemberNickname(guildId, userId, newNickName); err != nil {
				log.Println("Nickname change failed (2) :", err)
				counterError++
			}
		}
	}
	return counterError
}

func cleanPrefixes(s *discordgo.Session, guildMembers []*discordgo.Member, guildId string, ownerId string, prefixes []string, cmdworking *boolAtom) int {
	counterError := 0
	cmdworking.Set(true)
	for _, guildMember := range guildMembers {
		counterError += cleanPrefix(s, guildMember, guildId, ownerId, prefixes)
	}
	cmdworking.Set(false)
	return counterError
}

func cleanPrefix(s *discordgo.Session, guildMember *discordgo.Member, guildId string, ownerId string, prefixes []string) int {
	if userId := guildMember.User.ID; userId != ownerId {
		nickName := guildMember.Nick
		if nickName == "" {
			nickName = guildMember.User.Username
		}

		newNickName := cleanPrefixInNickName(nickName, prefixes)
		if newNickName != nickName {
			if err := s.GuildMemberNickname(guildId, userId, newNickName); err != nil {
				log.Println("Nickname change failed :", err)
				return 1
			}
		}
	}
	return 0
}

func addRoleCmd(s *discordgo.Session, i *discordgo.InteractionCreate, ownerId string, addedRoleId string, specialRoleIds map[string]empty, forbiddenRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string, cmdworking *boolAtom, okCmdMsg string, errGlobalCmdMsg string, errUnauthorizedCmdMsg string) {
	guildId := i.GuildID
	returnMsg := okCmdMsg
	roleIds := i.Member.Roles
	if roleIdInSet(roleIds, forbiddenRoleIds) {
		returnMsg = errUnauthorizedCmdMsg
	} else if userId := i.Member.User.ID; userId != ownerId && !cmdworking.Get() {
		removed := true
		for _, roleId := range roleIds {
			if roleId == addedRoleId {
				removed = false
				continue
			}

			if _, ok := roleIdToPrefix[roleId]; ok {
				if _, ok := specialRoleIds[roleId]; !ok {
					if err := s.GuildMemberRoleRemove(guildId, userId, roleId); err != nil {
						log.Println("Role removing failed (2) :", err)
						returnMsg = errGlobalCmdMsg
					}
				}
			}
		}

		if removed {
			if err := s.GuildMemberRoleAdd(guildId, userId, addedRoleId); err != nil {
				log.Println("Role addition failed (2) :", err)
				returnMsg = errGlobalCmdMsg
			}
		}
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}

func countRoleCmd(s *discordgo.Session, i *discordgo.InteractionCreate, roleIdToDisplayName map[string]string, countMsg string, errGlobalCmdMsg string) {
	returnMsg := errGlobalCmdMsg
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
		returnMsg = buildMsgWithNameValueList(countMsg, roleNameToCountStr)
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}

type nameValueSortByName [][2]string

func (nps nameValueSortByName) Len() int {
	return len(nps)
}

func (nps nameValueSortByName) Less(i, j int) bool {
	return nps[i][0] < nps[j][0]
}

func (nps nameValueSortByName) Swap(i, j int) {
	tmp := nps[i]
	nps[i] = nps[j]
	nps[j] = tmp
}

func buildMsgWithNameValueList(baseMsg string, nameToValue map[string]string) string {
	nameValues := make([][2]string, 0, len(nameToValue))
	for name, prefix := range nameToValue {
		nameValues = append(nameValues, [2]string{name, prefix})
	}
	sort.Sort(nameValueSortByName(nameValues))

	var buffer strings.Builder
	buffer.WriteString(baseMsg)
	for _, nameValue := range nameValues {
		buffer.WriteByte('\n')
		buffer.WriteString(nameValue[0])
		buffer.WriteString(" = ")
		buffer.WriteString(nameValue[1])
	}
	return buffer.String()
}
