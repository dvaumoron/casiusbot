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
	"errors"
	"log"
	"math/rand"
	"os"
	"regexp"
	"sort"
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

type pathSender struct {
	sender chan<- string
}

func MakePathSender(session *discordgo.Session, channelId string, errorMsg string) pathSender {
	pathChan := make(chan string)
	go sendFile(session, channelId, pathChan, errorMsg)
	return pathSender{sender: pathChan}
}

func (s pathSender) SendPath(path string) {
	s.sender <- path
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
	nickName := member.Nick
	if nickName == "" {
		nickName = member.User.Username
	}
	return nickName
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

func sendFile(session *discordgo.Session, channelId string, pathReceiver <-chan string, errorMsg string) {
	for path := range pathReceiver {
		if innerSendFile(session, channelId, path) {
			if _, err := session.ChannelMessageSend(channelId, errorMsg); err != nil {
				log.Println("Message sending failed (2) :", err)
			}
		}
	}
}

func innerSendFile(session *discordgo.Session, channelId string, path string) bool {
	file, err := os.Open(path)
	if err != nil {
		log.Println("File opening failed :", err)
		return true
	}
	defer file.Close()

	if _, err = session.ChannelFileSend(channelId, path, file); err != nil {
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
