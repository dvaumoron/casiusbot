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
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

type empty = struct{}

func initSetId(names []string, nameToId map[string]string) map[string]empty {
	setIds := map[string]empty{}
	for _, name := range names {
		setIds[nameToId[strings.TrimSpace(name)]] = empty{}
	}
	return setIds
}

func roleIdInSet(roleIds []string, roleIdSet map[string]empty) bool {
	for _, roleId := range roleIds {
		if _, ok := roleIdSet[roleId]; ok {
			return true
		}
	}
	return false
}

func getAndTrimSlice(valuesName string) []string {
	values := strings.Split(os.Getenv(valuesName), ",")
	for index, value := range values {
		values[index] = strings.TrimSpace(value)
	}
	return values
}

func getAndParseDurationSec(valueName string) time.Duration {
	valueSec, err := strconv.ParseInt(os.Getenv(valueName), 10, 64)
	if err != nil {
		log.Fatalln("Configuration", valueName, "parsing failed :", err)
	}
	return time.Duration(valueSec) * time.Second
}

func getAndParseDelayMins(valuesName string) []time.Duration {
	values := strings.Split(os.Getenv(valuesName), ",")
	delays := make([]time.Duration, 0, len(values))
	for _, value := range values {
		valueMin, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			log.Fatalln("Configuration", valuesName, "parsing failed :", err)
		}
		delay := time.Duration(valueMin) * time.Minute
		if delay > 0 {
			delay = -delay
		}
		delays = append(delays, delay)
	}
	return delays
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
	for range time.Tick(interval) {
		session.UpdateGameStatus(0, games[rand.Intn(gamesLen)])
	}
}
