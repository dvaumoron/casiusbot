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
	"math/rand"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

type empty = struct{}

type ChannelSenderManager map[string]chan<- string

func (m ChannelSenderManager) AddChannel(session *discordgo.Session, channelId string) {
	if _, ok := m[channelId]; !ok {
		m[channelId] = createMessageSender(session, channelId)
	}
}

func initIdSet(names []string, nameToId map[string]string) map[string]empty {
	idSet := map[string]empty{}
	for _, name := range names {
		idSet[nameToId[strings.TrimSpace(name)]] = empty{}
	}
	return idSet
}

func idInSet(ids []string, idSet map[string]empty) bool {
	for _, id := range ids {
		if _, ok := idSet[id]; ok {
			return true
		}
	}
	return false
}

func getAndTrimSlice(valuesConfName string) []string {
	values := os.Getenv(valuesConfName)
	if values == "" {
		return nil
	}
	splitted := strings.Split(values, ",")
	for index, value := range splitted {
		splitted[index] = strings.TrimSpace(value)
	}
	return splitted
}

func getAndParseDurationSec(valueConfName string) time.Duration {
	valueSec, err := strconv.ParseInt(os.Getenv(valueConfName), 10, 64)
	if err != nil {
		log.Println("Configuration", valueConfName, "parsing failed :", err)
	}
	return time.Duration(valueSec) * time.Second
}

func getAndParseDelayMins(valuesConfName string) []time.Duration {
	values := strings.Split(os.Getenv(valuesConfName), ",")
	delays := make([]time.Duration, 0, len(values))
	for _, value := range values {
		valueMin, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			log.Fatalln("Configuration", valuesConfName, "parsing failed :", err)
		}
		delay := time.Duration(valueMin) * time.Minute
		if delay > 0 {
			delay = -delay
		}
		delays = append(delays, delay)
	}
	return delays
}

func newCommand(cmdConfName string, cmdDescConfName string) *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        strings.TrimSpace(os.Getenv(cmdConfName)),
		Description: strings.TrimSpace(os.Getenv(cmdDescConfName)),
	}
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
	gamesLen := len(games)
	if gamesLen != 0 {
		for range time.Tick(interval) {
			session.UpdateGameStatus(0, games[rand.Intn(gamesLen)])
		}
	}
}

func initChecker(checkRules []string, index int, checkRulesSize int) func(string) bool {
	if index < checkRulesSize {
		if rule := checkRules[index]; rule != "" {
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
