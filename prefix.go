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

func readPrefixConfig(filePathName string) (map[string]string, []string, error) {
	file, err := os.Open(os.Getenv(filePathName))
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	nameToPrefix := map[string]string{}
	prefixes := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) != 0 && line[0] != '#' {
			sepIndex := strings.IndexByte(line, '=')
			prefix := strings.TrimSpace(line[sepIndex+1:]) + " "
			nameToPrefix[strings.TrimSpace(line[:sepIndex])] = prefix
			prefixes = append(prefixes, prefix)
		}
	}

	if err = scanner.Err(); err != nil {
		return nil, nil, err
	}
	return nameToPrefix, prefixes, nil
}

func membersCmd(s *discordgo.Session, guildId string, authorized bool, okCmdMsg string, errPartialCmdMsg string, errGlobalCmdMsg string, errUnauthorizedCmdMsg string, interaction *discordgo.Interaction, cmdEffect func([]*discordgo.Member) int) {
	returnMsg := okCmdMsg
	if authorized {
		if guildMembers, err := s.GuildMembers(guildId, "", 1000); err == nil {
			if counterError := cmdEffect(guildMembers); counterError != 0 {
				returnMsg = buildPartialErrorString(errPartialCmdMsg, counterError)
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

func transformName(nickName string, roleIds []string, defaultRoleId string, specialRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string) (string, bool, bool) {
	cleanedNickName := cleanPrefix(nickName, prefixes)
	nickName = cleanedNickName
	hasDefault, hasPrefix, notDone := false, false, true
	for _, roleId := range roleIds {
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

func cleanPrefix(nickName string, prefixes []string) string {
	for _, prefix := range prefixes {
		if strings.HasPrefix(nickName, prefix) {
			return nickName[len(prefix):]
		}
	}
	return nickName
}

func applyPrefixes(s *discordgo.Session, guildMembers []*discordgo.Member, guildId string, ownerId string, defaultRoleId string, ignoredRoleIds map[string]empty, specialRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string, cmdworking *boolAtom) int {
	counterError := 0
	cmdworking.Set(true)
	for _, guildMember := range guildMembers {
		counterError += applyPrefix(s, guildMember, guildId, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, roleIdToPrefix, prefixes)
	}
	cmdworking.Set(false)
	return counterError
}

func applyPrefix(s *discordgo.Session, guildMember *discordgo.Member, guildId string, ownerId string, defaultRoleId string, ignoredRoleIds map[string]empty, specialRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string) int {
	counterError := 0
	userId := guildMember.User.ID
	roleIds := guildMember.Roles
	if userId != ownerId && !roleIdInSet(roleIds, ignoredRoleIds) {
		nickName := guildMember.Nick
		if nickName == "" {
			nickName = guildMember.User.Username
		}

		newNickName, hasDefault, hasPrefix := transformName(nickName, roleIds, defaultRoleId, specialRoleIds, roleIdToPrefix, prefixes)
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
		if userId := guildMember.User.ID; userId != ownerId {
			nickName := guildMember.Nick
			if nickName == "" {
				nickName = guildMember.User.Username
			}

			newNickName := cleanPrefix(nickName, prefixes)
			if newNickName != nickName {
				if err := s.GuildMemberNickname(guildId, userId, newNickName); err != nil {
					log.Println("Nickname change failed :", err)
					counterError++
				}
			}
		}
	}
	cmdworking.Set(false)
	return counterError
}

func buildPartialErrorString(s string, i int) string {
	var buffer strings.Builder
	buffer.WriteString(s)
	buffer.WriteByte(' ')
	buffer.WriteString(strconv.Itoa(i))
	return buffer.String()
}

type namePrefixSortByName [][2]string

func (nps namePrefixSortByName) Len() int {
	return len(nps)
}

func (nps namePrefixSortByName) Less(i, j int) bool {
	return nps[i][0] < nps[j][0]
}

func (nps namePrefixSortByName) Swap(i, j int) {
	tmp := nps[i]
	nps[i] = nps[j]
	nps[j] = tmp
}

func buildMsgWithPrefixList(baseMsgName string, roleNameToPrefix map[string]string) string {
	var buffer strings.Builder
	buffer.WriteString(os.Getenv(baseMsgName))
	namePrefixes := make([][2]string, 0, len(roleNameToPrefix))
	for name, prefix := range roleNameToPrefix {
		namePrefixes = append(namePrefixes, [2]string{name, prefix})
	}
	sort.Sort(namePrefixSortByName(namePrefixes))
	for _, namePrefix := range namePrefixes {
		buffer.WriteByte('\n')
		buffer.WriteString(namePrefix[0])
		buffer.WriteString(" = ")
		buffer.WriteString(namePrefix[1])
	}
	return buffer.String()
}
