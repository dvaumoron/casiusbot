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
	"cmp"
	"errors"
	"log"
	"math/rand"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const cmdPlaceHolder = "{{cmd}}"
const numErrorPlaceHolder = "{{numError}}"

type empty = struct{}
type stringSet = map[string]empty

type GuildAndConfInfo struct {
	guildId                    string
	ownerId                    string
	defaultRoleId              string
	authorizedRoleIds          stringSet
	forbiddenRoleIds           stringSet
	ignoredRoleIds             stringSet
	forbiddenAndignoredRoleIds stringSet
	adminitrativeRoleIds       stringSet
	cmdRoleIds                 stringSet
	specialRoleIds             stringSet
	roleIdToPrefix             map[string]string
	prefixes                   []string
	roleIdToDisplayName        map[string]string
	msgs                       [10]string
}

type IdMonitor struct {
	processing stringSet
	mutex      sync.RWMutex
}

func MakeIdMonitor() IdMonitor {
	return IdMonitor{processing: stringSet{}}
}

func (m *IdMonitor) StopProcessing(id string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.processing, id)
}

func (m *IdMonitor) StartProcessing(id string) bool {
	m.mutex.RLock()
	_, processing := m.processing[id]
	m.mutex.RUnlock()
	if processing {
		return false
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	// verify there was no change between lock
	_, processing = m.processing[id]
	if processing {
		return false
	}
	m.processing[id] = empty{}
	return true
}

type ChannelSenderManager struct {
	channels map[string]chan<- string
	session  *discordgo.Session
}

func MakeChannelSenderManager(session *discordgo.Session) ChannelSenderManager {
	return ChannelSenderManager{channels: map[string]chan<- string{}, session: session}
}

func (m ChannelSenderManager) AddChannel(channelId string) {
	if channelId != "" {
		if _, ok := m.channels[channelId]; !ok {
			m.channels[channelId] = createMessageSender(m.session, channelId)
		}
	}
}

func (m ChannelSenderManager) Get(channelId string) chan<- string {
	return m.channels[channelId]
}

func createDataSender(session *discordgo.Session, channelId string, errorMsg string, cmdName string) chan<- [2]string {
	if channelId == "" {
		return nil
	}

	dataChan := make(chan [2]string)
	go sendFile(session, channelId, dataChan, strings.ReplaceAll(errorMsg, cmdPlaceHolder, cmdName))
	return dataChan
}

func initIdSet(trimmedNames []string, nameToId map[string]string) (stringSet, error) {
	idSet := stringSet{}
	for _, name := range trimmedNames {
		id := nameToId[name]
		if id == "" {
			return nil, errors.New("Unrecognized name (2) : " + name)
		}
		idSet[nameToId[name]] = empty{}
	}
	return idSet, nil
}

func idInSet(ids []string, idSet stringSet) bool {
	for _, id := range ids {
		if _, ok := idSet[id]; ok {
			return true
		}
	}
	return false
}

func appendCommand(cmds []*discordgo.ApplicationCommand, config Config, cmdConfName string, cmdDescConfName string) (string, []*discordgo.ApplicationCommand) {
	cmdName := config.getString(cmdConfName)
	if cmdName != "" {
		cmds = append(cmds, &discordgo.ApplicationCommand{
			Name: cmdName, Description: config.require(cmdDescConfName),
		})
	}
	return cmdName, cmds
}

func addNonEmpty[T any](m map[string]T, name string, value T) {
	if name != "" {
		m[name] = value
	}
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
			msg = strings.ReplaceAll(infos.msgs[3], numErrorPlaceHolder, strconv.Itoa(counterError))
		}
	} else {
		log.Println("Cannot retrieve guild members (3) :", err)
	}
	messageSender <- strings.ReplaceAll(msg, cmdPlaceHolder, cmdName)
}

func extractNick(member *discordgo.Member) string {
	nickname := member.Nick
	if nickname == "" {
		nickname = member.User.Username
	}
	return nickname
}

func createMessageSender(session *discordgo.Session, channelId string) chan<- string {
	messageChan := make(chan string)
	go sendMessage(session, channelId, messageChan)
	return messageChan
}

func sendMessage(session *discordgo.Session, channelId string, messageReceiver <-chan string) {
	for message := range messageReceiver {
		if _, err := session.ChannelMessageSend(channelId, message); err != nil {
			log.Println("Message sending failed :", err)
		}
	}
}

func sendFile(session *discordgo.Session, channelId string, dataReceiver <-chan [2]string, errorMsg string) {
	for pathAndData := range dataReceiver {
		if innerSendFile(session, channelId, pathAndData[0], pathAndData[1]) {
			if _, err := session.ChannelMessageSend(channelId, errorMsg); err != nil {
				log.Println("Message sending failed (2) :", err)
			}
		}
	}
}

func innerSendFile(session *discordgo.Session, channelId string, path string, data string) bool {
	dataReader := strings.NewReader(data)
	if _, err := session.ChannelFileSend(channelId, path, dataReader); err != nil {
		log.Println("File sending failed :", err)
		return true
	}
	return false
}

func launchTickers(number int, interval time.Duration) []chan time.Time {
	subTickers := make([]chan time.Time, number)
	for index := range subTickers {
		subTickers[index] = make(chan time.Time, 1)
	}
	go startDispatchTick(interval, subTickers)
	return subTickers
}

func startDispatchTick(interval time.Duration, subTickers []chan time.Time) {
	dispatchTick(time.Now(), subTickers)
	for newTime := range time.Tick(interval) {
		dispatchTick(newTime, subTickers)
	}
}

func dispatchTick(newTime time.Time, subTickers []chan time.Time) {
	for _, subTicker := range subTickers {
		subTicker <- newTime
	}
}

func updateGameStatus(session *discordgo.Session, games []string, interval time.Duration) {
	if gamesLen := len(games); gamesLen != 0 {
		for range time.Tick(interval) {
			session.UpdateGameStatus(0, games[rand.Intn(gamesLen)])
		}
	}
}

func initChecker(rule string) func(string) bool {
	if rule != "" {
		if colonIndex := strings.IndexByte(rule, ':'); colonIndex == -1 {
			log.Println("Check rule not recognized :", rule)
		} else {
			if re, err := regexp.Compile(rule[colonIndex+1:]); err == nil {
				if rule[:colonIndex] == "reject" {
					return func(link string) bool {
						return !re.MatchString(link)
					}
				}
				return re.MatchString
			} else {
				log.Println("Failed to compile regexp to check link :", err)
			}
		}
	}
	return acceptAll
}

func acceptAll(link string) bool {
	return true
}

func buildMsgWithNameValueList(baseMsg string, nameToValue map[string]string) string {
	nameValues := make([][2]string, 0, len(nameToValue))
	for name, prefix := range nameToValue {
		nameValues = append(nameValues, [2]string{name, prefix})
	}
	slices.SortFunc(nameValues, cmpNameValueAsc)

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

func cmpNameValueAsc(a [2]string, b [2]string) int {
	return cmp.Compare(a[0], b[0])
}

func sendTick(tickSender chan<- bool, interval time.Duration) {
	for range time.Tick(interval) {
		tickSender <- false
	}
}
